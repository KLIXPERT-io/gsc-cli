package cmd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KLIXPERT-io/gsc-cli/internal/audit"
	"github.com/KLIXPERT-io/gsc-cli/internal/cache"
	"github.com/KLIXPERT-io/gsc-cli/internal/client"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/spf13/cobra"
	searchconsole "google.golang.org/api/searchconsole/v1"
)

func newSitesCmd() *cobra.Command {
	c := &cobra.Command{Use: "sites", Short: "Manage Search Console properties"}
	c.AddCommand(newSitesListCmd(), newSitesGetCmd(), newSitesAddCmd(), newSitesRemoveCmd())
	return c
}

func newSitesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all GSC properties the authenticated user owns or manages",
		Long: `Lists every Search Console property visible to the authenticated user.

Examples:
  gsc sites list
  gsc sites list --output csv
  gsc sites list --no-cache`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			c, identity, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			key := cache.Key("sites.list", nil, "", identity)
			data, meta, err := cachedOrCall(ctx, s, key, s.Cfg.TTL(), func(ctx context.Context) (json.RawMessage, error) {
				resp, err := c.Svc.Sites.List().Context(ctx).Do()
				if err != nil {
					return nil, client.Translate(err)
				}
				_ = s.Quota.Bump("other", 1)
				return json.Marshal(resp)
			})
			if err != nil {
				return err
			}
			var resp searchconsole.SitesListResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return errs.New(errs.CodeGeneric, err.Error())
			}
			var columns []string
			var rows []output.Row
			columns = []string{"site_url", "permission_level"}
			for _, e := range resp.SiteEntry {
				rows = append(rows, output.Row{"site_url": e.SiteUrl, "permission_level": e.PermissionLevel})
			}
			return emit(cmd, resp.SiteEntry, meta, columns, rows)
		},
	}
}

func newSitesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <url>",
		Short: "Show property details and verification info",
		Long: `Returns the property record and permission level.

Examples:
  gsc sites get sc-domain:example.com
  gsc sites get https://www.example.com/`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			c, identity, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			siteURL := args[0]
			key := cache.Key("sites.get", []string{siteURL}, siteURL, identity)
			data, meta, err := cachedOrCall(ctx, s, key, s.Cfg.TTL(), func(ctx context.Context) (json.RawMessage, error) {
				resp, err := c.Svc.Sites.Get(siteURL).Context(ctx).Do()
				if err != nil {
					return nil, client.Translate(err)
				}
				_ = s.Quota.Bump("other", 1)
				return json.Marshal(resp)
			})
			if err != nil {
				return err
			}
			var e searchconsole.WmxSite
			_ = json.Unmarshal(data, &e)
			return emit(cmd, e, meta, nil, nil)
		},
	}
}

func newSitesAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <url>",
		Short: "Add a property to the authenticated account",
		Long: `Adds a property. Not cached. Logged to ./.gsc/audit.log.

Examples:
  gsc sites add sc-domain:example.com
  gsc sites add https://www.example.com/`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			c, _, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			siteURL := args[0]
			if err := c.Svc.Sites.Add(siteURL).Context(ctx).Do(); err != nil {
				_ = s.Audit.Append(audit.Event{Command: "sites.add", Property: siteURL, Action: "add", OK: false, Err: err.Error()})
				return client.Translate(err)
			}
			_ = s.Quota.Bump("other", 1)
			_ = s.Audit.Append(audit.Event{Command: "sites.add", Property: siteURL, Action: "add", OK: true})
			// Invalidate sites.list cache.
			_ = s.Cache.Clear()
			return emit(cmd, map[string]any{"ok": true, "site_url": siteURL}, output.Meta{APICalls: 1}, nil, nil)
		},
	}
}

func newSitesRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <url>",
		Short: "Remove a property (destructive — requires --yes in non-TTY)",
		Long: `Removes a property from the account. DESTRUCTIVE.

Without --yes:
- On a TTY: prompts for confirmation.
- In non-TTY (pipes, scripts, agents): exits with code 5.

Examples:
  gsc sites remove sc-domain:example.com --yes
  gsc sites remove https://www.example.com/`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s := getState(cmd)
			siteURL := args[0]
			if !s.Yes {
				if !output.IsTTY(cmd.InOrStdin().(interface{ Fd() uintptr }).Fd()) {
					return errs.New(errs.CodeInvalidArgs, "confirmation required").WithHint("Pass --yes to confirm in non-TTY contexts.")
				}
				cmd.PrintErr("Remove property " + siteURL + "? Type the URL to confirm: ")
				var answer string
				_, _ = cmd.InOrStdin().Read([]byte(answer))
				// Simple read — agents will use --yes; interactive humans can type.
				if strings.TrimSpace(answer) != siteURL {
					return errs.New(errs.CodeInvalidArgs, "confirmation mismatch")
				}
			}
			c, _, err := s.buildClient(ctx)
			if err != nil {
				return err
			}
			if err := c.Svc.Sites.Delete(siteURL).Context(ctx).Do(); err != nil {
				_ = s.Audit.Append(audit.Event{Command: "sites.remove", Property: siteURL, Action: "remove", OK: false, Err: err.Error()})
				return client.Translate(err)
			}
			_ = s.Quota.Bump("other", 1)
			_ = s.Audit.Append(audit.Event{Command: "sites.remove", Property: siteURL, Action: "remove", OK: true})
			_ = s.Cache.Clear()
			return emit(cmd, map[string]any{"ok": true, "site_url": siteURL}, output.Meta{APICalls: 1}, nil, nil)
		},
	}
}
