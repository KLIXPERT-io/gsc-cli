package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/KLIXPERT-io/gsc-cli/internal/cache"
	"github.com/KLIXPERT-io/gsc-cli/internal/client"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/KLIXPERT-io/gsc-cli/internal/quota"
	"github.com/spf13/cobra"
	searchconsole "google.golang.org/api/searchconsole/v1"
)

func newURLsCmd() *cobra.Command {
	c := &cobra.Command{Use: "urls", Short: "URL inspection commands"}
	c.AddCommand(newURLsInspectCmd())
	return c
}

type inspectResult struct {
	URL    string                                    `json:"url"`
	Error  *errs.E                                   `json:"error,omitempty"`
	Result *searchconsole.UrlInspectionResult        `json:"result,omitempty"`
}

func newURLsInspectCmd() *cobra.Command {
	var (
		siteURL     string
		concurrency int
		langCode    string
	)
	c := &cobra.Command{
		Use:   "inspect <property> <url> [urls...]",
		Short: "Inspect one or more URLs (fans out with concurrency cap)",
		Long: `Inspect URLs against the URL Inspection API for the given property.

Accepts URLs as positional args or newline-delimited on stdin when "-" is passed.
Parallelism default 5 (override with --concurrency, max 10).
Progress goes to stderr when it's a TTY; silent when piped.
On quota exhaustion, dispatch stops and partial results are returned with
meta.partial=true.

Examples:
  gsc urls inspect sc-domain:example.com https://www.example.com/page1 https://www.example.com/page2
  cat urls.txt | gsc urls inspect sc-domain:example.com -
  gsc urls inspect sc-domain:example.com https://www.example.com/a --concurrency 10`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			siteURL = args[0]
			var urls []string
			for _, a := range args[1:] {
				if a == "-" {
					br := bufio.NewScanner(os.Stdin)
					for br.Scan() {
						line := br.Text()
						if line != "" {
							urls = append(urls, line)
						}
					}
					if err := br.Err(); err != nil && !errors.Is(err, io.EOF) {
						return err
					}
				} else {
					urls = append(urls, a)
				}
			}
			if len(urls) == 0 {
				return errs.New(errs.CodeInvalidArgs, "no URLs provided")
			}
			if concurrency < 1 {
				concurrency = 5
			}
			if concurrency > 10 {
				concurrency = 10
			}

			cc, identity, err := s.buildClient(ctx)
			if err != nil {
				return err
			}

			sem := make(chan struct{}, concurrency)
			var wg sync.WaitGroup
			results := make([]inspectResult, len(urls))
			var mu sync.Mutex
			stopped := false
			var unInspected []string

			for i, u := range urls {
				if stopped {
					unInspected = append(unInspected, u)
					continue
				}
				wg.Add(1)
				sem <- struct{}{}
				go func(i int, u string) {
					defer wg.Done()
					defer func() { <-sem }()
					ir := inspectResult{URL: u}
					// Cache check
					key := cache.Key("urls.inspect", []string{u, langCode}, siteURL, identity)
					if !s.NoCache && !s.Refresh {
						if entry, _ := s.Cache.Get(key); entry != nil {
							_ = json.Unmarshal(entry.Payload, &ir)
							mu.Lock()
							results[i] = ir
							mu.Unlock()
							return
						}
					}
					// Quota bump before dispatch
					if err := s.Quota.Bump("url_inspection", 1); err != nil {
						mu.Lock()
						if errors.Is(err, quota.ErrQuotaExceeded) {
							stopped = true
						}
						results[i] = inspectResult{URL: u, Error: errs.New(errs.CodeQuotaExceeded, err.Error()).WithRetry(3600)}
						mu.Unlock()
						return
					}
					req := &searchconsole.InspectUrlIndexRequest{
						SiteUrl:       siteURL,
						InspectionUrl: u,
						LanguageCode:  langCode,
					}
					resp, err := cc.Svc.UrlInspection.Index.Inspect(req).Context(ctx).Do()
					if err != nil {
						e := client.Translate(err)
						var se *errs.E
						if errors.As(e, &se) {
							ir.Error = se
						} else {
							ir.Error = errs.New(errs.CodeGeneric, e.Error())
						}
					} else {
						ir.Result = resp.InspectionResult
						b, _ := json.Marshal(ir)
						if !s.NoCache {
							_ = s.Cache.Put(key, b, 24*time.Hour)
						}
					}
					mu.Lock()
					results[i] = ir
					mu.Unlock()
				}(i, u)
			}
			wg.Wait()

			// Filter out empty slots (quota-stopped items) into unInspected.
			filtered := results[:0]
			for i, r := range results {
				if r.URL == "" {
					unInspected = append(unInspected, urls[i])
					continue
				}
				filtered = append(filtered, r)
			}
			results = filtered

			meta := output.Meta{APICalls: len(results), Partial: len(unInspected) > 0}
			out := map[string]any{
				"property":     siteURL,
				"results":      results,
			}
			if len(unInspected) > 0 {
				out["uninspected"] = unInspected
			}

			cols := []string{"url", "verdict", "coverage_state", "indexing_state"}
			rows := []output.Row{}
			for _, r := range results {
				row := output.Row{"url": r.URL}
				if r.Result != nil && r.Result.IndexStatusResult != nil {
					row["verdict"] = r.Result.IndexStatusResult.Verdict
					row["coverage_state"] = r.Result.IndexStatusResult.CoverageState
					row["indexing_state"] = r.Result.IndexStatusResult.IndexingState
				}
				rows = append(rows, row)
			}
			return emit(cmd, out, meta, cols, rows)
		},
	}
	c.Flags().IntVar(&concurrency, "concurrency", 5, "parallel inspections (1..10)")
	c.Flags().StringVar(&langCode, "lang", "en-US", "language code for inspection")
	_ = siteURL
	return c
}

// silence unused-import warning when debugging.
var _ = context.Background
