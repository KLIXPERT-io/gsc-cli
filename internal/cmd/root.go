// Package cmd wires the cobra command tree and shared context.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/KLIXPERT-io/gsc-cli/internal/audit"
	"github.com/KLIXPERT-io/gsc-cli/internal/auth"
	"github.com/KLIXPERT-io/gsc-cli/internal/cache"
	"github.com/KLIXPERT-io/gsc-cli/internal/client"
	"github.com/KLIXPERT-io/gsc-cli/internal/config"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/logging"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/KLIXPERT-io/gsc-cli/internal/quota"
	"github.com/KLIXPERT-io/gsc-cli/internal/update"
	"github.com/spf13/cobra"
)

// Shared flag/state carried via context.
type ctxKey string

const stateKey ctxKey = "gsc.state"

type State struct {
	Cfg          *config.Config
	Cache        *cache.Store
	Quota        *quota.Store
	Audit        *audit.Logger
	OutputFormat string
	NoCache      bool
	Refresh      bool
	CacheTTL     time.Duration
	Yes          bool
	CredsPath    string
	Verbose      bool
	Quiet        bool
	LogFormat    string
}

func getState(cmd *cobra.Command) *State {
	v := cmd.Context().Value(stateKey)
	s, _ := v.(*State)
	return s
}

// buildClient returns an authed API client, refreshing tokens on the fly.
func (s *State) buildClient(ctx context.Context) (*client.Client, string, error) {
	credsPath := s.CredsPath
	if credsPath == "" {
		credsPath = os.Getenv("GSC_CREDENTIALS")
	}
	if credsPath == "" {
		credsPath = config.ExpandHome(s.Cfg.Auth.CredentialsPath)
	}
	cfg, err := auth.LoadConfig(credsPath)
	if err != nil {
		hint := "Run `gsc config set auth.credentials_path <path-to-client_secrets.json>`, or pass --credentials, or set GSC_CREDENTIALS."
		return nil, "", errs.New(errs.CodeAuthMissing, err.Error()).WithHint(hint)
	}
	httpClient, err := auth.HTTPClient(ctx, cfg)
	if err != nil {
		return nil, "", errs.New(errs.CodeAuthExpired, err.Error()).WithHint("Run `gsc auth login`.")
	}
	tok, _ := auth.LoadToken()
	identity := auth.Identity(cfg, tok)
	c, err := client.New(ctx, httpClient)
	if err != nil {
		return nil, "", client.Translate(err)
	}
	return c, identity, nil
}

// Execute builds and runs the root command.
func Execute(version string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st := &State{}
	root := &cobra.Command{
		Use:   "gsc",
		Short: "Google Search Console CLI — LLM-friendly, fast, cached",
		Long: `gsc is a Go CLI that wraps the Google Search Console API v1.
It outputs structured JSON (default), CSV, or pretty tables, caches reads
locally, tracks quota, and emits machine-readable errors for LLM agents.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			maybePrintUpdateNotice(cmd, version)
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			st.Cfg = cfg
			// Resolve output format default from config if flag unset.
			if st.OutputFormat == "" && cfg.Defaults.Output != "" {
				st.OutputFormat = cfg.Defaults.Output
			}
			// Cache dir resolution.
			cacheDir := cfg.Cache.Dir
			if cacheDir == "" {
				cacheDir = "./.gsc/cache"
			}
			st.Cache = cache.New(cacheDir, cfg.TTL())
			// Quota store.
			st.Quota = quota.New("./.gsc/quota.json")
			st.Quota.WarnFn = func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) }
			// Audit log.
			st.Audit = audit.New("./.gsc/audit.log")
			// Logging.
			logging.Setup(logging.Options{Verbose: st.Verbose || cfg.Logging.Verbose, Quiet: st.Quiet, Format: firstNonEmpty(st.LogFormat, cfg.Logging.Format, "text")})
			// Cache hint writer
			st.Cache.HintWriter = func(msg string) { fmt.Fprintln(os.Stderr, "hint: "+msg) }
			cmd.SetContext(context.WithValue(cmd.Context(), stateKey, st))
			return nil
		},
	}

	root.Version = version
	root.PersistentFlags().StringVar(&st.OutputFormat, "output", "", "output format: json|csv|table (default: json, or table on TTY)")
	root.PersistentFlags().BoolVar(&st.NoCache, "no-cache", false, "bypass cache read and write")
	root.PersistentFlags().BoolVar(&st.Refresh, "refresh", false, "bypass cache read, write fresh result")
	root.PersistentFlags().DurationVar(&st.CacheTTL, "cache-ttl", 0, "override cache TTL for this call (e.g. 30m)")
	root.PersistentFlags().BoolVar(&st.Yes, "yes", false, "answer yes to confirmation prompts (required for destructive ops in non-TTY)")
	root.PersistentFlags().StringVar(&st.CredsPath, "credentials", "", "path to Google client_secrets.json (overrides config + env)")
	root.PersistentFlags().BoolVarP(&st.Verbose, "verbose", "v", false, "verbose API traces to stderr")
	root.PersistentFlags().BoolVarP(&st.Quiet, "quiet", "q", false, "suppress warnings on stderr")
	root.PersistentFlags().StringVar(&st.LogFormat, "log-format", "", "log format: text|json (default text)")

	root.AddCommand(
		newAuthCmd(),
		newSitesCmd(),
		newAnalyticsCmd(),
		newURLsCmd(),
		newSitemapsCmd(),
		newQuotaCmd(),
		newConfigCmd(),
		newUpdateCmd(version),
	)

	root.SetContext(ctx)
	if err := root.Execute(); err != nil {
		errs.Write(os.Stderr, err)
		return errs.ExitCode(err)
	}
	return 0
}

// maybePrintUpdateNotice emits the FR-007 post-update notice once per upgrade.
// It is suppressed for any command beneath the `update` subtree and via the
// GSC_NO_UPDATE_NOTICE env var.
func maybePrintUpdateNotice(cmd *cobra.Command, version string) {
	if v := os.Getenv("GSC_NO_UPDATE_NOTICE"); v != "" && v != "0" && v != "false" {
		return
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "update" && c.HasParent() {
			return
		}
	}
	st, err := update.LoadState()
	if err != nil {
		return
	}
	installed := st.LastInstalledVersion
	if installed == "" || installed == version || installed == "v"+version {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "gsc: updated to %s (was v%s)\n", installed, version)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// openBrowser attempts to open a URL in the default browser.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// emit renders the given data with the resolved format + meta envelope.
func emit(cmd *cobra.Command, data any, meta output.Meta, columns []string, rows []output.Row) error {
	s := getState(cmd)
	fmtStr := s.OutputFormat
	fd := os.Stdout.Fd()
	f := output.ResolveFormat(fmtStr, fd)
	switch f {
	case output.FormatJSON:
		return output.WriteJSON(os.Stdout, data, meta)
	case output.FormatCSV:
		if columns == nil {
			return errs.New(errs.CodeInvalidArgs, "CSV not supported for this command")
		}
		return output.WriteCSV(os.Stdout, columns, rows)
	case output.FormatTable:
		if columns == nil {
			return output.WriteJSON(os.Stdout, data, meta)
		}
		return output.WriteTable(os.Stdout, columns, rows)
	}
	return output.WriteJSON(os.Stdout, data, meta)
}
