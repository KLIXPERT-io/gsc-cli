package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/KLIXPERT-io/gsc-cli/internal/cache"
	"github.com/KLIXPERT-io/gsc-cli/internal/client"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/spf13/cobra"
)

func newCwvCmd() *cobra.Command {
	var (
		formFactor     string
		originFallback bool
		failOn         string
		asJSON         bool
	)
	c := &cobra.Command{
		Use:   "cwv <target>",
		Short: "Core Web Vitals summary (LCP/INP/CLS/TTFB) for a URL or origin",
		Long: `Fetches Core Web Vitals from the Chrome UX Report and renders a compact
pass/fail summary with p75 values and ratings.

By default, queries the URL; on a 404 and --origin-fallback (default on),
derives scheme://host and retries at the origin level.

Examples:
  gsc cwv https://example.com/pricing
  gsc cwv https://example.com/pricing --form-factor desktop
  gsc cwv https://example.com/ --fail-on poor
  gsc cwv https://example.com/ --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			target := args[0]

			if asJSON {
				s.OutputFormat = "json"
			}

			ffLower := strings.ToLower(formFactor)
			if ffLower != "phone" && ffLower != "desktop" {
				return errs.New(errs.CodeInvalidArgs, "invalid --form-factor: "+formFactor).WithHint("accepted: phone, desktop")
			}
			ff, err := normalizeFormFactor(ffLower)
			if err != nil {
				return err
			}
			switch failOn {
			case "", "none", "needs-improvement", "ni", "poor":
			default:
				return errs.New(errs.CodeInvalidArgs, "invalid --fail-on: "+failOn).WithHint("accepted: none, needs-improvement, poor")
			}

			httpClient, identity, err := s.buildHTTPClient(ctx)
			if err != nil {
				return err
			}
			cc := client.NewCrUX(httpClient)

			tryFetch := func(source string, req client.CruxRequest) (json.RawMessage, output.Meta, error) {
				key := cache.Key("cwv.query", []string{target, source, ffLower}, "", identity)
				return cachedOrCall(ctx, s, key, s.Cfg.CruxTTL(), func(ctx context.Context) (json.RawMessage, error) {
					raw, callErr := cc.QueryRecord(ctx, req)
					_ = s.Quota.Bump("crux", 1)
					if callErr != nil {
						return nil, callErr
					}
					return raw, nil
				})
			}

			source := "url"
			if isBareOrigin(target) {
				source = "origin"
			}
			req := client.CruxRequest{FormFactor: ff}
			if source == "origin" {
				o, err := originOf(target)
				if err != nil {
					return err
				}
				req.Origin = o
			} else {
				req.URL = target
			}

			data, meta, err := tryFetch(source, req)
			if err != nil && source == "url" && originFallback {
				var e *errs.E
				if errors.As(err, &e) && e.Code == errs.CodeNotFound {
					o, oerr := originOf(target)
					if oerr == nil {
						req2 := client.CruxRequest{FormFactor: ff, Origin: o}
						data, meta, err = tryFetch("origin", req2)
						if err == nil {
							source = "origin"
						}
					}
				}
			}
			if err != nil {
				return err
			}

			var resp client.CruxQueryResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return errs.New(errs.CodeGeneric, err.Error())
			}

			type metricOut struct {
				P75    any    `json:"p75"`
				Rating string `json:"rating"`
			}
			out := struct {
				Target     string               `json:"target"`
				Source     string               `json:"source"`
				FormFactor string               `json:"formFactor"`
				Metrics    map[string]metricOut `json:"metrics"`
			}{
				Target:     target,
				Source:     source,
				FormFactor: ffLower,
				Metrics:    map[string]metricOut{},
			}

			cols := []string{"metric", "p75", "rating"}
			rows := []output.Row{}
			worstRank := 0
			rank := map[string]int{"good": 1, "needs-improvement": 2, "poor": 3}
			for _, m := range []string{"lcp", "inp", "cls", "ttfb"} {
				k, _ := cwvCruxKey(m)
				mm, ok := resp.Record.Metrics[k]
				if !ok {
					continue
				}
				v, ok := parseCruxP75(mm.Percentiles.P75)
				if !ok {
					continue
				}
				r := rateCWV(m, v)
				out.Metrics[m] = metricOut{P75: v, Rating: r}
				if rank[r] > worstRank {
					worstRank = rank[r]
				}
				rows = append(rows, output.Row{
					"metric": m,
					"p75":    formatCWVValue(m, v),
					"rating": r,
				})
			}
			rows = append(rows, output.Row{"metric": "source", "p75": source, "rating": ""})

			if err := emit(cmd, out, meta, cols, rows); err != nil {
				return err
			}

			switch failOn {
			case "poor":
				if worstRank >= 3 {
					return errs.New(errs.CodeGeneric, "one or more metrics are poor")
				}
			case "ni", "needs-improvement":
				if worstRank >= 2 {
					return errs.New(errs.CodeGeneric, "one or more metrics are needs-improvement or worse")
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&formFactor, "form-factor", "phone", "form factor: phone|desktop")
	c.Flags().BoolVar(&originFallback, "origin-fallback", true, "on 404, retry with the origin derived from the URL")
	c.Flags().StringVar(&failOn, "fail-on", "", "exit non-zero when any metric is at or worse than: needs-improvement|poor")
	c.Flags().BoolVar(&asJSON, "json", false, "shortcut for --output json")

	// silence unused-var
	_ = context.Background
	return c
}
