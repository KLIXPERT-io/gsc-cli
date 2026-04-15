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
)

func newCruxCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "crux",
		Short: "Chrome UX Report (real-user field data)",
		Long:  `Query the Chrome UX Report API for current and historical Core Web Vitals data from real Chrome users.`,
	}
	c.AddCommand(newCruxQueryCmd(), newCruxHistoryCmd())
	return c
}

func newCruxQueryCmd() *cobra.Command {
	var (
		formFactor string
		metrics    []string
		origin     bool
		asJSON     bool
	)
	c := &cobra.Command{
		Use:   "query <target>",
		Short: "Query the current CrUX record for a URL or origin",
		Long: `Queries the Chrome UX Report for a URL or origin.

Auto-detects origin vs URL: a bare "scheme://host[:port]" with no path is
treated as an origin; anything else is a URL. Use --origin to force origin
mode (derives origin from a URL input).

Examples:
  gsc crux query https://example.com/pricing
  gsc crux query https://example.com --origin
  gsc crux query https://example.com/pricing --form-factor desktop
  gsc crux query https://example.com/ --metric lcp,inp --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			target := args[0]

			if asJSON {
				s.OutputFormat = "json"
			}

			ff, err := normalizeFormFactor(formFactor)
			if err != nil {
				return err
			}
			cruxMetrics, err := normalizeMetrics(metrics)
			if err != nil {
				return err
			}

			req := client.CruxRequest{FormFactor: ff, Metrics: cruxMetrics}
			useOrigin := origin || isBareOrigin(target)
			if useOrigin {
				o, err := originOf(target)
				if err != nil {
					return err
				}
				req.Origin = o
			} else {
				req.URL = target
			}

			httpClient, identity, err := s.buildHTTPClient(ctx)
			if err != nil {
				return err
			}
			cc := client.NewCrUX(httpClient)

			mode := "url"
			if useOrigin {
				mode = "origin"
			}
			key := cache.Key("crux.query", []string{target, mode, formFactor, strings.Join(cruxMetrics, ",")}, "", identity)
			data, meta, err := cachedOrCall(ctx, s, key, s.Cfg.CruxTTL(), func(ctx context.Context) (json.RawMessage, error) {
				raw, callErr := cc.QueryRecord(ctx, req)
				_ = s.Quota.Bump("crux", 1)
				if callErr != nil {
					return nil, callErr
				}
				return raw, nil
			})
			if err != nil {
				return err
			}

			var resp client.CruxQueryResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return errs.New(errs.CodeGeneric, err.Error())
			}

			cols := []string{"metric", "p75", "rating", "histogram"}
			rows := []output.Row{}
			// Stable order: known CWV first, then anything else alphabetically.
			known := map[string]bool{}
			for _, m := range cwvMetricOrder() {
				if k, ok := cwvCruxKey(m); ok {
					known[k] = true
					mm, ok := resp.Record.Metrics[k]
					if !ok {
						continue
					}
					rows = append(rows, cruxMetricRow(m, mm))
				}
			}
			extra := []string{}
			for k := range resp.Record.Metrics {
				if !known[k] {
					extra = append(extra, k)
				}
			}
			sort.Strings(extra)
			for _, k := range extra {
				rows = append(rows, cruxMetricRow(cruxShortForKey(k), resp.Record.Metrics[k]))
			}

			return emit(cmd, resp, meta, cols, rows)
		},
	}
	c.Flags().StringVar(&formFactor, "form-factor", "all", "form factor: phone|desktop|tablet|all")
	c.Flags().StringSliceVar(&metrics, "metric", nil, "metrics (comma-separated): lcp,inp,cls,ttfb,fcp (default all)")
	c.Flags().BoolVar(&origin, "origin", false, "force origin mode (derive scheme://host from a URL input)")
	c.Flags().BoolVar(&asJSON, "json", false, "shortcut for --output json")
	return c
}

func cruxMetricRow(short string, m client.CruxMetric) output.Row {
	row := output.Row{"metric": short}
	if v, ok := parseCruxP75(m.Percentiles.P75); ok {
		row["p75"] = formatCWVValue(short, v)
		row["rating"] = rateCWV(short, v)
	}
	if len(m.Histogram) > 0 {
		parts := make([]string, 0, len(m.Histogram))
		for _, b := range m.Histogram {
			parts = append(parts, fmt.Sprintf("[%s..%s]=%.3f", strings.Trim(string(b.Start), "\""), strings.Trim(string(b.End), "\""), b.Density))
		}
		row["histogram"] = strings.Join(parts, " ")
	}
	return row
}

func newCruxHistoryCmd() *cobra.Command {
	var (
		formFactor string
		metrics    []string
		origin     bool
		weeks      int
		asJSON     bool
	)
	c := &cobra.Command{
		Use:   "history <target>",
		Short: "Query historical CrUX records (up to 25 weeks)",
		Long: `Queries the Chrome UX Report History API for a URL or origin.

Returns up to 25 weekly collection periods per metric (API maximum).

Examples:
  gsc crux history https://example.com/
  gsc crux history https://example.com --origin --weeks 12
  gsc crux history https://example.com/pricing --metric lcp,inp --form-factor phone
  gsc crux history https://example.com/ --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			target := args[0]

			if asJSON {
				s.OutputFormat = "json"
			}
			if weeks < 1 {
				return errs.New(errs.CodeInvalidArgs, "--weeks must be >= 1")
			}
			if weeks > 25 {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: --weeks clamped to API maximum of 25")
				weeks = 25
			}
			ff, err := normalizeFormFactor(formFactor)
			if err != nil {
				return err
			}
			cruxMetrics, err := normalizeMetrics(metrics)
			if err != nil {
				return err
			}

			req := client.CruxRequest{FormFactor: ff, Metrics: cruxMetrics, CollectionPeriodCount: weeks}
			useOrigin := origin || isBareOrigin(target)
			if useOrigin {
				o, err := originOf(target)
				if err != nil {
					return err
				}
				req.Origin = o
			} else {
				req.URL = target
			}

			httpClient, identity, err := s.buildHTTPClient(ctx)
			if err != nil {
				return err
			}
			cc := client.NewCrUX(httpClient)

			mode := "url"
			if useOrigin {
				mode = "origin"
			}
			key := cache.Key("crux.history", []string{target, mode, formFactor, strings.Join(cruxMetrics, ","), fmt.Sprintf("w%d", weeks)}, "", identity)
			data, meta, err := cachedOrCall(ctx, s, key, s.Cfg.CruxTTL(), func(ctx context.Context) (json.RawMessage, error) {
				raw, callErr := cc.QueryHistoryRecord(ctx, req)
				_ = s.Quota.Bump("crux", 1)
				if callErr != nil {
					return nil, callErr
				}
				return raw, nil
			})
			if err != nil {
				return err
			}

			var resp client.CruxHistoryResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return errs.New(errs.CodeGeneric, err.Error())
			}

			// Build wide table: one row per collection period, one column per metric p75.
			metricsOrder := []string{}
			seen := map[string]bool{}
			for _, m := range cwvMetricOrder() {
				if k, ok := cwvCruxKey(m); ok {
					if _, present := resp.Record.Metrics[k]; present {
						metricsOrder = append(metricsOrder, m)
						seen[k] = true
					}
				}
			}
			extra := []string{}
			for k := range resp.Record.Metrics {
				if !seen[k] {
					extra = append(extra, k)
				}
			}
			sort.Strings(extra)
			for _, k := range extra {
				metricsOrder = append(metricsOrder, cruxShortForKey(k))
			}

			cols := append([]string{"period_end"}, metricsOrder...)
			rows := []output.Row{}
			for i, cp := range resp.Record.CollectionPeriods {
				row := output.Row{"period_end": fmt.Sprintf("%04d-%02d-%02d", cp.LastDate.Year, cp.LastDate.Month, cp.LastDate.Day)}
				for _, m := range metricsOrder {
					k, ok := cwvCruxKey(m)
					if !ok {
						continue
					}
					ts, ok := resp.Record.Metrics[k]
					if !ok {
						continue
					}
					if i < len(ts.Percentiles.P75s) {
						if v, ok := parseCruxP75(ts.Percentiles.P75s[i]); ok {
							row[m] = formatCWVValue(m, v)
						}
					}
				}
				rows = append(rows, row)
			}

			return emit(cmd, resp, meta, cols, rows)
		},
	}
	c.Flags().StringVar(&formFactor, "form-factor", "all", "form factor: phone|desktop|tablet|all")
	c.Flags().StringSliceVar(&metrics, "metric", nil, "metrics (comma-separated): lcp,inp,cls,ttfb,fcp (default all)")
	c.Flags().BoolVar(&origin, "origin", false, "force origin mode (derive scheme://host from a URL input)")
	c.Flags().IntVar(&weeks, "weeks", 25, "number of weekly collection periods (1..25)")
	c.Flags().BoolVar(&asJSON, "json", false, "shortcut for --output json")
	return c
}
