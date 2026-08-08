package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KLIXPERT-io/gsc-cli/internal/cache"
	"github.com/KLIXPERT-io/gsc-cli/internal/client"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/spf13/cobra"
	searchconsole "google.golang.org/api/searchconsole/v1"
)

func newAnalyticsCmd() *cobra.Command {
	c := &cobra.Command{Use: "analytics", Short: "Search Analytics commands"}
	c.AddCommand(newAnalyticsQueryCmd(), newAnalyticsOverviewCmd())
	return c
}

type rangeFlags struct {
	Range   string
	Start   string
	End     string
	Compare string
}

func addRangeFlags(cmd *cobra.Command, rf *rangeFlags) {
	cmd.Flags().StringVar(&rf.Range, "range", "", "preset range: last-7d|last-28d|last-3m|last-6m|last-12m|last-16m")
	cmd.Flags().StringVar(&rf.Start, "start", "", "start date YYYY-MM-DD (mutually exclusive with --range)")
	cmd.Flags().StringVar(&rf.End, "end", "", "end date YYYY-MM-DD (mutually exclusive with --range)")
	cmd.Flags().StringVar(&rf.Compare, "compare", "", "compare window: previous-period|previous-year")
}

// resolveRange returns (start, end) as YYYY-MM-DD strings.
// GSC data has a ~3-day lag; we use "today" as the upper bound, not today-3, to match UI behavior.
func (rf *rangeFlags) resolve(defaultRange string) (string, string, error) {
	today := time.Now().UTC()
	if rf.Start != "" || rf.End != "" {
		if rf.Range != "" {
			return "", "", errs.New(errs.CodeInvalidDateRange, "--range and --start/--end are mutually exclusive")
		}
		if rf.Start == "" || rf.End == "" {
			return "", "", errs.New(errs.CodeInvalidDateRange, "--start and --end must both be provided")
		}
		if _, err := time.Parse("2006-01-02", rf.Start); err != nil {
			return "", "", errs.New(errs.CodeInvalidDateRange, "invalid --start: "+err.Error())
		}
		if _, err := time.Parse("2006-01-02", rf.End); err != nil {
			return "", "", errs.New(errs.CodeInvalidDateRange, "invalid --end: "+err.Error())
		}
		return rf.Start, rf.End, nil
	}
	r := rf.Range
	if r == "" {
		r = defaultRange
	}
	if r == "" {
		r = "last-28d"
	}
	var days int
	switch r {
	case "last-7d":
		days = 7
	case "last-28d":
		days = 28
	case "last-3m":
		days = 90
	case "last-6m":
		days = 180
	case "last-12m":
		days = 365
	case "last-16m":
		days = 486
	default:
		return "", "", errs.New(errs.CodeInvalidDateRange, "unknown --range: "+r)
	}
	start := today.AddDate(0, 0, -days).Format("2006-01-02")
	end := today.Format("2006-01-02")
	return start, end, nil
}

// compareRange returns the start/end for the comparison window.
func compareRange(start, end, mode string) (string, string, error) {
	s, _ := time.Parse("2006-01-02", start)
	e, _ := time.Parse("2006-01-02", end)
	switch mode {
	case "previous-period":
		d := e.Sub(s)
		ps := s.Add(-d - 24*time.Hour)
		pe := s.Add(-24 * time.Hour)
		return ps.Format("2006-01-02"), pe.Format("2006-01-02"), nil
	case "previous-year":
		return s.AddDate(-1, 0, 0).Format("2006-01-02"), e.AddDate(-1, 0, 0).Format("2006-01-02"), nil
	case "":
		return "", "", nil
	}
	return "", "", errs.New(errs.CodeInvalidArgs, "unknown --compare: "+mode)
}

func newAnalyticsQueryCmd() *cobra.Command {
	var rf rangeFlags
	var (
		dimensions   string
		filters      []string
		filterGroups []string
		searchType   string
		limit        int64
		orderBy      string
		asc          bool
		groupBy      string
		aggregation  string
		dataState    string
		all          bool
	)
	c := &cobra.Command{
		Use:   "query <url>",
		Short: "Run a Search Analytics query with dimensions, filters, date range, and compare",
		Long: `Query the Search Analytics API for a property.

Examples:
  # top 50 queries for mobile last 28 days
  gsc analytics query sc-domain:example.com --dimensions query --filter device=MOBILE --limit 50

  # daily clicks/impressions time series (shortcut adds 'date' dimension + CSV)
  gsc analytics query sc-domain:example.com --group-by date --range last-3m --output csv

  # compare CTR vs previous period
  gsc analytics query sc-domain:example.com --dimensions query --range last-28d --compare previous-period

  # page+country drilldown for US traffic
  gsc analytics query sc-domain:example.com --dimensions page,country --filter country=usa --limit 100

  # include fresh (non-finalized) last-2-days data
  gsc analytics query sc-domain:example.com --dimensions query --data-state all

  # hourly breakdown (up to 10 days back; rows are timestamped in PT)
  gsc analytics query sc-domain:example.com --group-by hour --range last-7d --output csv

  # force byPage aggregation on a domain property
  gsc analytics query sc-domain:example.com --dimensions query --aggregation byPage

  # auto-paginate past 25k rows, stream CSV
  gsc analytics query sc-domain:example.com --dimensions query,page --all --output csv

  # OR-of-AND filter: (query~brand AND device=MOBILE) OR (country=usa)
  gsc analytics query sc-domain:example.com --filter-group "query~brand,device=MOBILE" --filter-group "country=usa"

  # RE2 regex filter: pages under /blog/ or /guides/
  gsc analytics query sc-domain:example.com --dimensions page --filter "page~~/(blog|guides)/"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			siteURL := args[0]
			start, end, err := rf.resolve(s.Cfg.Defaults.Range)
			if err != nil {
				return err
			}
			dims := parseCSV(dimensions)
			if groupBy != "" && !contains(dims, groupBy) {
				dims = append(dims, groupBy)
			}
			if len(dims) == 0 {
				dims = []string{"query"}
			}
			switch aggregation {
			case "auto", "byPage", "byProperty":
			default:
				return errs.New(errs.CodeInvalidArgs, "--aggregation must be one of: auto, byPage, byProperty")
			}
			if err := validateDataState(dataState); err != nil {
				return err
			}
			switch orderBy {
			case "clicks", "impressions", "ctr", "position":
			default:
				return errs.New(errs.CodeInvalidArgs, "--order-by must be one of: clicks, impressions, ctr, position")
			}
			// Only reorder when asked. Left alone, the API's own ordering is
			// clicks-desc for most groupings but chronological for date/hour,
			// which is what a time series wants.
			sortRequested := cmd.Flags().Changed("order-by") || cmd.Flags().Changed("asc")
			// The API requires dataState=hourly_all whenever HOUR is a grouping
			// dimension. Upgrade silently when the user left --data-state at its
			// default; refuse when they asked for something incompatible.
			if contains(dims, "hour") {
				if !cmd.Flags().Changed("data-state") {
					dataState = "hourly_all"
				} else if dataState != "hourly_all" {
					return errs.New(errs.CodeInvalidArgs, "--dimensions hour requires --data-state hourly_all").
						WithHint("Hourly rows are always partial; the API rejects final/all when grouping by hour.")
				}
			} else if dataState == "hourly_all" {
				return errs.New(errs.CodeInvalidArgs, "--data-state hourly_all requires 'hour' in --dimensions").
					WithHint("Add --dimensions hour (or --group-by hour) to get an hourly breakdown.")
			}
			if all && rf.Compare != "" {
				return errs.New(errs.CodeInvalidArgs, "--all and --compare are mutually exclusive")
			}
			if all && limit > 25000 {
				limit = 25000
			}
			if len(filters) > 0 && len(filterGroups) > 0 {
				return errs.New(errs.CodeInvalidArgs, "--filter and --filter-group are mutually exclusive").WithHint("--filter-group is a superset; express single AND groups there.")
			}
			parsedGroups := make([][]*searchconsole.ApiDimensionFilter, 0, len(filterGroups))
			for i, g := range filterGroups {
				parts := strings.Split(g, ",")
				group := make([]*searchconsole.ApiDimensionFilter, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p == "" {
						return errs.New(errs.CodeInvalidArgs, fmt.Sprintf("invalid --filter-group[%d]: empty filter expression", i))
					}
					df, err := parseFilter(p)
					if err != nil {
						return errs.New(errs.CodeInvalidArgs, fmt.Sprintf("invalid --filter-group[%d]: %s", i, err.Error()))
					}
					group = append(group, df)
				}
				if len(group) == 0 {
					return errs.New(errs.CodeInvalidArgs, fmt.Sprintf("invalid --filter-group[%d]: empty group", i))
				}
				parsedGroups = append(parsedGroups, group)
			}
			cc, identity, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			keyArgs := []string{start, end, strings.Join(dims, ","), strings.Join(filters, "|"), strings.Join(filterGroups, "|"), searchType, orderBy, fmt.Sprintf("%d", limit), fmt.Sprintf("%v", asc), rf.Compare, aggregation, dataState, fmt.Sprintf("all=%v", all), fmt.Sprintf("sorted=%v", sortRequested)}
			key := cache.Key("analytics.query", keyArgs, siteURL, identity)
			data, meta, err := cachedOrCall(ctx, s, key, 15*time.Minute, func(ctx context.Context) (json.RawMessage, error) {
				if all {
					var merged []*searchconsole.ApiDataRow
					var aggType string
					var meta *searchconsole.Metadata
					var startRow int64 = 0
					for {
						req, err := buildAnalyticsRequest(start, end, dims, filters, searchType, limit, orderBy, asc, dataState, aggregation, startRow, parsedGroups)
						if err != nil {
							return nil, err
						}
						resp, err := runQuery(ctx, s, cc, siteURL, req)
						if err != nil {
							return nil, err
						}
						if aggType == "" {
							aggType = resp.ResponseAggregationType
						}
						if meta == nil {
							meta = resp.Metadata
						}
						merged = append(merged, resp.Rows...)
						n := int64(len(resp.Rows))
						if n == 0 || n < limit {
							break
						}
						startRow += limit
					}
					if sortRequested {
						sortRows(merged, orderBy, asc)
					}
					result := map[string]any{"rows": merged, "responseAggregationType": aggType}
					if meta != nil {
						result["metadata"] = meta
					}
					return json.Marshal(result)
				}
				req, err := buildAnalyticsRequest(start, end, dims, filters, searchType, limit, orderBy, asc, dataState, aggregation, 0, parsedGroups)
				if err != nil {
					return nil, err
				}
				resp, err := runQuery(ctx, s, cc, siteURL, req)
				if err != nil {
					return nil, err
				}
				if sortRequested {
					sortRows(resp.Rows, orderBy, asc)
				}
				result := map[string]any{"rows": resp.Rows, "responseAggregationType": resp.ResponseAggregationType}
				if resp.Metadata != nil {
					result["metadata"] = resp.Metadata
				}
				if rf.Compare != "" {
					cs, ce, err := compareRange(start, end, rf.Compare)
					if err != nil {
						return nil, err
					}
					creq, _ := buildAnalyticsRequest(cs, ce, dims, filters, searchType, limit, orderBy, asc, dataState, aggregation, 0, parsedGroups)
					cresp, err := runQuery(ctx, s, cc, siteURL, creq)
					if err != nil {
						return nil, err
					}
					if sortRequested {
						sortRows(cresp.Rows, orderBy, asc)
					}
					result["comparison"] = map[string]any{
						"start": cs, "end": ce, "mode": rf.Compare,
						"rows": cresp.Rows,
					}
				}
				return json.Marshal(result)
			})
			if err != nil {
				return err
			}
			var result map[string]any
			_ = json.Unmarshal(data, &result)
			// A metadata block means the window runs into data Google is still
			// collecting, so the tail rows may still move.
			if _, ok := result["metadata"]; ok {
				meta.Partial = true
			}
			// CSV rendering: one row per result row with dimension columns + metric columns.
			cols := append(append([]string{}, dims...), "clicks", "impressions", "ctr", "position")
			rows := []output.Row{}
			if r, ok := result["rows"].([]any); ok {
				for _, raw := range r {
					m, _ := raw.(map[string]any)
					row := output.Row{}
					keys, _ := m["keys"].([]any)
					for i, d := range dims {
						if i < len(keys) {
							row[d] = keys[i]
						}
					}
					row["clicks"] = m["clicks"]
					row["impressions"] = m["impressions"]
					row["ctr"] = m["ctr"]
					row["position"] = m["position"]
					rows = append(rows, row)
				}
			}
			return emit(cmd, result, meta, cols, rows)
		},
	}
	addRangeFlags(c, &rf)
	c.Flags().StringVar(&dimensions, "dimensions", "", "comma list: query,page,country,device,searchAppearance,date,hour (default query)")
	c.Flags().StringArrayVar(&filters, "filter", nil, "filter: <dim><op><value> where op is = != ~ !~ ~~ !~~ (repeatable)")
	c.Flags().StringArrayVar(&filterGroups, "filter-group", nil, "OR-of-AND filter group: comma-separated filters form one AND group; repeat flag for OR (mutually exclusive with --filter)")
	c.Flags().StringVar(&searchType, "search-type", "web", "web|image|video|news|discover|googleNews")
	c.Flags().Int64Var(&limit, "limit", 20, "row limit (max 25000)")
	c.Flags().StringVar(&orderBy, "order-by", "clicks", "sort key: clicks|impressions|ctr|position (client-side; the API always selects the top --limit rows by clicks, so pair with --all to sort the full set)")
	c.Flags().BoolVar(&asc, "asc", false, "sort ascending (default descending)")
	c.Flags().StringVar(&groupBy, "group-by", "", "shortcut to add a dimension (e.g. --group-by date for time-series)")
	c.Flags().StringVar(&aggregation, "aggregation", "auto", "aggregation type: auto|byPage|byProperty")
	c.Flags().StringVar(&dataState, "data-state", "final", "data freshness: final|all|hourly_all (all includes last ~2 days; hourly_all is required with, and implied by, --dimensions hour)")
	c.Flags().BoolVar(&all, "all", false, "auto-paginate until all rows fetched (uses --limit as page size, clamps to 25000; incompatible with --compare)")
	return c
}

func newAnalyticsOverviewCmd() *cobra.Command {
	var rf rangeFlags
	var searchType string
	var aggregation string
	var dataState string
	c := &cobra.Command{
		Use:   "overview <url>",
		Short: "Summary performance: totals for clicks/impressions/ctr/position",
		Long: `Returns aggregate metrics (no dimensions) for the property over the window.

Examples:
  gsc analytics overview sc-domain:example.com
  gsc analytics overview sc-domain:example.com --range last-7d
  gsc analytics overview sc-domain:example.com --compare previous-year --output json
  gsc analytics overview sc-domain:example.com --data-state all
  gsc analytics overview sc-domain:example.com --aggregation byProperty`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			siteURL := args[0]
			start, end, err := rf.resolve(s.Cfg.Defaults.Range)
			if err != nil {
				return err
			}
			cc, identity, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			switch aggregation {
			case "auto", "byPage", "byProperty":
			default:
				return errs.New(errs.CodeInvalidArgs, "--aggregation must be one of: auto, byPage, byProperty")
			}
			switch dataState {
			case "final", "all":
			default:
				return errs.New(errs.CodeInvalidArgs, "--data-state must be one of: final, all")
			}
			key := cache.Key("analytics.overview", []string{start, end, searchType, rf.Compare, aggregation, dataState}, siteURL, identity)
			data, meta, err := cachedOrCall(ctx, s, key, 15*time.Minute, func(ctx context.Context) (json.RawMessage, error) {
				req, _ := buildAnalyticsRequest(start, end, nil, nil, searchType, 1, "clicks", false, dataState, aggregation, 0, nil)
				if err := s.Quota.BumpSA(); err != nil {
					return nil, errs.New(errs.CodeRateLimited, err.Error())
				}
				resp, err := cc.Svc.Searchanalytics.Query(siteURL, req).Context(ctx).Do()
				if err != nil {
					return nil, client.Translate(err)
				}
				out := map[string]any{"start": start, "end": end, "responseAggregationType": resp.ResponseAggregationType}
				if len(resp.Rows) > 0 {
					r := resp.Rows[0]
					out["clicks"] = r.Clicks
					out["impressions"] = r.Impressions
					out["ctr"] = r.Ctr
					out["position"] = r.Position
				}
				if rf.Compare != "" {
					cs, ce, err := compareRange(start, end, rf.Compare)
					if err != nil {
						return nil, err
					}
					creq, _ := buildAnalyticsRequest(cs, ce, nil, nil, searchType, 1, "clicks", false, dataState, aggregation, 0, nil)
					if err := s.Quota.BumpSA(); err != nil {
						return nil, errs.New(errs.CodeRateLimited, err.Error())
					}
					cresp, err := cc.Svc.Searchanalytics.Query(siteURL, creq).Context(ctx).Do()
					if err != nil {
						return nil, client.Translate(err)
					}
					cmp := map[string]any{"start": cs, "end": ce, "mode": rf.Compare}
					if len(cresp.Rows) > 0 {
						r := cresp.Rows[0]
						cmp["clicks"] = r.Clicks
						cmp["impressions"] = r.Impressions
						cmp["ctr"] = r.Ctr
						cmp["position"] = r.Position
					}
					out["comparison"] = cmp
				}
				return json.Marshal(out)
			})
			if err != nil {
				return err
			}
			var out map[string]any
			_ = json.Unmarshal(data, &out)
			cols := []string{"start", "end", "clicks", "impressions", "ctr", "position"}
			rows := []output.Row{{
				"start": out["start"], "end": out["end"],
				"clicks": out["clicks"], "impressions": out["impressions"],
				"ctr": out["ctr"], "position": out["position"],
			}}
			return emit(cmd, out, meta, cols, rows)
		},
	}
	addRangeFlags(c, &rf)
	c.Flags().StringVar(&searchType, "search-type", "web", "web|image|video|news|discover|googleNews")
	c.Flags().StringVar(&aggregation, "aggregation", "auto", "aggregation type: auto|byPage|byProperty")
	c.Flags().StringVar(&dataState, "data-state", "final", "data freshness: final|all (all includes last ~2 days, not finalized)")
	return c
}

func runQuery(ctx context.Context, s *State, cc *client.Client, siteURL string, req *searchconsole.SearchAnalyticsQueryRequest) (*searchconsole.SearchAnalyticsQueryResponse, error) {
	if err := s.Quota.BumpSA(); err != nil {
		return nil, errs.New(errs.CodeRateLimited, err.Error())
	}
	resp, err := cc.Svc.Searchanalytics.Query(siteURL, req).Context(ctx).Do()
	if err != nil {
		return nil, client.Translate(err)
	}
	return resp, nil
}

func buildAnalyticsRequest(start, end string, dims []string, filters []string, searchType string, limit int64, orderBy string, asc bool, dataState string, aggregation string, startRow int64, filterGroups [][]*searchconsole.ApiDimensionFilter) (*searchconsole.SearchAnalyticsQueryRequest, error) {
	req := &searchconsole.SearchAnalyticsQueryRequest{
		StartDate:  start,
		EndDate:    end,
		Dimensions: dims,
		SearchType: searchType,
		RowLimit:   limit,
		StartRow:   startRow,
	}
	if len(filterGroups) > 0 {
		groups := make([]*searchconsole.ApiDimensionFilterGroup, 0, len(filterGroups))
		for _, g := range filterGroups {
			groups = append(groups, &searchconsole.ApiDimensionFilterGroup{GroupType: "and", Filters: g})
		}
		req.DimensionFilterGroups = groups
	} else if len(filters) > 0 {
		fg := &searchconsole.ApiDimensionFilterGroup{GroupType: "and"}
		for _, f := range filters {
			df, err := parseFilter(f)
			if err != nil {
				return nil, err
			}
			fg.Filters = append(fg.Filters, df)
		}
		req.DimensionFilterGroups = []*searchconsole.ApiDimensionFilterGroup{fg}
	}
	if aggregation != "" {
		req.AggregationType = aggregation
	}
	if dataState != "" {
		if err := validateDataState(dataState); err != nil {
			return nil, err
		}
		req.DataState = dataState
	}
	// orderBy/asc are applied client-side by sortRows: the Search Analytics API
	// has no sort parameter and always returns rows ordered by clicks desc.
	_ = orderBy
	_ = asc
	return req, nil
}

func validateDataState(dataState string) error {
	switch dataState {
	case "final", "all", "hourly_all":
		return nil
	}
	return errs.New(errs.CodeInvalidArgs, "--data-state must be one of: final, all, hourly_all")
}

// sortRows orders rows by the requested metric. The API has no sort parameter
// and always returns the top rows by clicks, so without --all this only
// reorders the page that was fetched — it does not re-select rows.
func sortRows(rows []*searchconsole.ApiDataRow, orderBy string, asc bool) {
	if len(rows) < 2 {
		return
	}
	metric := func(r *searchconsole.ApiDataRow) float64 {
		switch orderBy {
		case "impressions":
			return r.Impressions
		case "ctr":
			return r.Ctr
		case "position":
			return r.Position
		default:
			return r.Clicks
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := metric(rows[i]), metric(rows[j])
		if a == b {
			return false
		}
		if asc {
			return a < b
		}
		return a > b
	})
}

func parseFilter(f string) (*searchconsole.ApiDimensionFilter, error) {
	// Longest tokens first: "!~~" must win over "!~", and "~~" over "~".
	ops := []struct {
		token string
		op    string
	}{
		{"!~~", "excludingRegex"},
		{"~~", "includingRegex"},
		{"!=", "notEquals"},
		{"!~", "notContains"},
		{"~", "contains"},
		{"=", "equals"},
	}
	// Split at the earliest operator so that operator characters inside the
	// value (a regex, say) don't get mistaken for the separator; on a tie the
	// longest token wins.
	best := -1
	var bestOp string
	for _, op := range ops {
		i := strings.Index(f, op.token)
		if i <= 0 {
			continue
		}
		if best == -1 || i < best || (i == best && len(op.token) > len(bestOp)) {
			best, bestOp = i, op.token
		}
	}
	if best > 0 {
		for _, op := range ops {
			if op.token == bestOp {
				return &searchconsole.ApiDimensionFilter{
					Dimension:  f[:best],
					Operator:   op.op,
					Expression: f[best+len(bestOp):],
				}, nil
			}
		}
	}
	return nil, errs.New(errs.CodeInvalidArgs, "filter must be <dim><op><value> with op in =,!=,~,!~,~~,!~~: "+f)
}

func parseCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}
