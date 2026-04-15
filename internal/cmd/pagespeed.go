package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/KLIXPERT-io/gsc-cli/internal/cache"
	"github.com/KLIXPERT-io/gsc-cli/internal/client"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/spf13/cobra"
	pagespeedonline "google.golang.org/api/pagespeedonline/v5"
)

var psiAllowedCategories = []string{"performance", "accessibility", "best-practices", "seo", "pwa"}

func newPagespeedCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pagespeed",
		Short: "PageSpeed Insights (Lighthouse + field data)",
		Long:  `Run Lighthouse audits and fetch Core Web Vitals via the PageSpeed Insights API.`,
	}
	c.AddCommand(newPagespeedRunCmd())
	return c
}

func newPagespeedRunCmd() *cobra.Command {
	var (
		strategy   string
		categories []string
		locale     string
		asJSON     bool
	)
	c := &cobra.Command{
		Use:   "run <url>",
		Short: "Run a PageSpeed Insights audit for a URL",
		Long: `Runs Lighthouse + CrUX field data via the PageSpeed Insights API.

Strategy defaults to "mobile". Categories default to all of: performance,
accessibility, best-practices, seo, pwa.

Examples:
  gsc pagespeed run https://example.com/
  gsc pagespeed run https://example.com/pricing --strategy desktop
  gsc pagespeed run https://example.com/ --category performance,seo --json
  gsc pagespeed run https://example.com/ --locale en-US --output table`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			target := args[0]

			strategy = strings.ToLower(strategy)
			if strategy != "mobile" && strategy != "desktop" {
				return errs.New(errs.CodeInvalidArgs, "invalid --strategy: "+strategy).WithHint("accepted: mobile, desktop")
			}
			cats, err := normalizeCategories(categories)
			if err != nil {
				return err
			}

			if asJSON {
				s.OutputFormat = "json"
			}

			httpClient, identity, err := s.buildHTTPClient(ctx)
			if err != nil {
				return err
			}
			p, err := client.NewPSI(ctx, httpClient)
			if err != nil {
				return client.Translate(err)
			}

			key := cache.Key("psi.run", []string{target, strategy, strings.Join(cats, ","), locale}, "", identity)
			data, meta, err := cachedOrCall(ctx, s, key, s.Cfg.PSITTL(), func(ctx context.Context) (json.RawMessage, error) {
				apiCats := make([]string, len(cats))
				for i, c := range cats {
					apiCats[i] = psiCategoryAPI(c)
				}
				call := p.Svc.Pagespeedapi.Runpagespeed(target).Context(ctx).Strategy(strings.ToUpper(strategy)).Category(apiCats...)
				if locale != "" {
					call = call.Locale(locale)
				}
				resp, callErr := call.Do()
				_ = s.Quota.Bump("psi", 1)
				if callErr != nil {
					return nil, client.TranslatePSI(callErr)
				}
				return json.Marshal(resp)
			})
			if err != nil {
				return err
			}

			var resp pagespeedonline.PagespeedApiPagespeedResponseV5
			if err := json.Unmarshal(data, &resp); err != nil {
				return errs.New(errs.CodeGeneric, err.Error())
			}

			cols := []string{"metric", "value", "rating"}
			rows := []output.Row{}
			if resp.LighthouseResult != nil && resp.LighthouseResult.Categories != nil {
				cc := resp.LighthouseResult.Categories
				addCat := func(name string, lc *pagespeedonline.LighthouseCategoryV5) {
					if lc == nil {
						return
					}
					var score any
					if lc.Score != nil {
						if f, ok := lc.Score.(float64); ok {
							score = int(f * 100)
						} else {
							score = lc.Score
						}
					}
					rows = append(rows, output.Row{"metric": "category:" + name, "value": score, "rating": ""})
				}
				addCat("performance", cc.Performance)
				addCat("accessibility", cc.Accessibility)
				addCat("best-practices", cc.BestPractices)
				addCat("seo", cc.Seo)
				addCat("pwa", cc.Pwa)
			}
			if resp.LoadingExperience != nil && resp.LoadingExperience.Metrics != nil {
				for _, m := range cwvMetricOrder() {
					apiKey, ok := cwvPSIKey(m)
					if !ok {
						continue
					}
					mm, ok := resp.LoadingExperience.Metrics[apiKey]
					if !ok {
						continue
					}
					val := float64(mm.Percentile)
					rows = append(rows, output.Row{
						"metric": m,
						"value":  formatCWVValue(m, val),
						"rating": rateCWV(m, val),
					})
				}
			}
			if resp.LighthouseResult != nil && resp.LighthouseResult.FetchTime != "" {
				rows = append(rows, output.Row{"metric": "fetch_time", "value": resp.LighthouseResult.FetchTime, "rating": ""})
			}

			return emit(cmd, resp, meta, cols, rows)
		},
	}
	c.Flags().StringVar(&strategy, "strategy", "mobile", "strategy: mobile|desktop")
	c.Flags().StringSliceVar(&categories, "category", nil, "categories (comma-separated): performance,accessibility,best-practices,seo,pwa")
	c.Flags().StringVar(&locale, "locale", "", "BCP-47 locale for the Lighthouse result (e.g. en-US)")
	c.Flags().BoolVar(&asJSON, "json", false, "shortcut for --output json")
	return c
}

func normalizeCategories(in []string) ([]string, error) {
	if len(in) == 0 {
		out := append([]string(nil), psiAllowedCategories...)
		sort.Strings(out)
		return out, nil
	}
	var out []string
	for _, s := range in {
		for _, p := range strings.Split(s, ",") {
			p = strings.ToLower(strings.TrimSpace(p))
			if p == "" {
				continue
			}
			ok := false
			for _, a := range psiAllowedCategories {
				if p == a {
					ok = true
					break
				}
			}
			if !ok {
				return nil, errs.New(errs.CodeInvalidArgs, fmt.Sprintf("invalid --category %q", p)).WithHint("accepted: " + strings.Join(psiAllowedCategories, ", "))
			}
			out = append(out, p)
		}
	}
	sort.Strings(out)
	// dedupe
	uniq := out[:0]
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			uniq = append(uniq, v)
		}
	}
	return uniq, nil
}

// psiCategoryAPI maps our normalized category to the PSI API enum form.
func psiCategoryAPI(c string) string {
	switch c {
	case "best-practices":
		return "BEST_PRACTICES"
	default:
		return strings.ToUpper(c)
	}
}
