package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/KLIXPERT-io/gsc-cli/internal/audit"
	"github.com/KLIXPERT-io/gsc-cli/internal/cache"
	"github.com/KLIXPERT-io/gsc-cli/internal/client"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/spf13/cobra"
	searchconsole "google.golang.org/api/searchconsole/v1"
)

func newSitemapsCmd() *cobra.Command {
	c := &cobra.Command{Use: "sitemaps", Short: "Manage sitemaps for a property"}
	c.AddCommand(newSitemapsListCmd(), newSitemapsSubmitCmd(), newSitemapsGetCmd())
	return c
}

func newSitemapsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <property>",
		Short: "List all submitted sitemaps for a property",
		Long: `Examples:
  gsc sitemaps list sc-domain:example.com
  gsc sitemaps list sc-domain:example.com --output csv`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			siteURL := args[0]
			cc, identity, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			key := cache.Key("sitemaps.list", nil, siteURL, identity)
			data, meta, err := cachedOrCall(ctx, s, key, 10*time.Minute, func(ctx context.Context) (json.RawMessage, error) {
				resp, err := cc.Svc.Sitemaps.List(siteURL).Context(ctx).Do()
				if err != nil {
					return nil, client.Translate(err)
				}
				_ = s.Quota.Bump("other", 1)
				return json.Marshal(resp)
			})
			if err != nil {
				return err
			}
			var resp searchconsole.SitemapsListResponse
			_ = json.Unmarshal(data, &resp)
			cols := []string{"path", "type", "is_pending", "is_sitemaps_index", "last_submitted", "last_downloaded"}
			rows := []output.Row{}
			for _, sm := range resp.Sitemap {
				rows = append(rows, output.Row{
					"path":              sm.Path,
					"type":              sm.Type,
					"is_pending":        sm.IsPending,
					"is_sitemaps_index": sm.IsSitemapsIndex,
					"last_submitted":    sm.LastSubmitted,
					"last_downloaded":   sm.LastDownloaded,
				})
			}
			return emit(cmd, resp.Sitemap, meta, cols, rows)
		},
	}
}

func newSitemapsSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit <property> <sitemap-url>",
		Short: "Submit a sitemap URL to a property",
		Long: `Submits a sitemap. Not cached. Logged to ./.gsc/audit.log.

Examples:
  gsc sitemaps submit sc-domain:example.com https://www.example.com/sitemap.xml`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			siteURL, sitemapURL := args[0], args[1]
			cc, _, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			if err := cc.Svc.Sitemaps.Submit(siteURL, sitemapURL).Context(ctx).Do(); err != nil {
				_ = s.Audit.Append(audit.Event{Command: "sitemaps.submit", Property: siteURL, Target: sitemapURL, Action: "submit", OK: false, Err: err.Error()})
				return client.Translate(err)
			}
			_ = s.Quota.Bump("other", 1)
			_ = s.Audit.Append(audit.Event{Command: "sitemaps.submit", Property: siteURL, Target: sitemapURL, Action: "submit", OK: true})
			return emit(cmd, map[string]any{"ok": true, "property": siteURL, "sitemap": sitemapURL}, output.Meta{APICalls: 1}, nil, nil)
		},
	}
}

func newSitemapsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <property> <sitemap-url>",
		Short: "Show status, warnings, and errors for a specific sitemap",
		Long: `Examples:
  gsc sitemaps get sc-domain:example.com https://www.example.com/sitemap.xml`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			siteURL, sitemapURL := args[0], args[1]
			cc, identity, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			key := cache.Key("sitemaps.get", []string{sitemapURL}, siteURL, identity)
			data, meta, err := cachedOrCall(ctx, s, key, 10*time.Minute, func(ctx context.Context) (json.RawMessage, error) {
				resp, err := cc.Svc.Sitemaps.Get(siteURL, sitemapURL).Context(ctx).Do()
				if err != nil {
					return nil, client.Translate(err)
				}
				_ = s.Quota.Bump("other", 1)
				return json.Marshal(resp)
			})
			if err != nil {
				return err
			}
			var sm searchconsole.WmxSitemap
			_ = json.Unmarshal(data, &sm)
			return emit(cmd, sm, meta, nil, nil)
		},
	}
}
