// Package cmd wires the cobra command tree and shared context.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	SAPath       string
	Subject      string
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
	httpClient, identity, err := s.buildHTTPClient(ctx)
	if err != nil {
		return nil, "", err
	}
	c, err := client.New(ctx, httpClient)
	if err != nil {
		return nil, "", client.Translate(err)
	}
	return c, identity, nil
}

// buildHTTPClient returns a raw authed *http.Client and the caller identity,
// for either OAuth or service account credentials.
func (s *State) buildHTTPClient(ctx context.Context) (*http.Client, string, error) {
	creds, err := s.credentials()
	if err != nil {
		return nil, "", err
	}
	httpClient, err := creds.HTTPClient(ctx)
	if err != nil {
		hint := "Run `gsc auth login`."
		if creds.Kind == auth.KindServiceAccount {
			hint = "Check the service account key with `gsc auth status`; it may have been disabled or revoked."
		}
		return nil, "", errs.New(errs.CodeAuthExpired, err.Error()).WithHint(hint)
	}
	return httpClient, creds.Identity(), nil
}

// credentials resolves the configured credentials file and loads it.
func (s *State) credentials() (*auth.Credentials, error) {
	path, subject := s.resolveCredentials()
	creds, err := auth.Resolve(path, subject)
	if err != nil {
		if errors.Is(err, auth.ErrSubjectNotSupported) {
			return nil, errs.New(errs.CodeInvalidArgs, err.Error()).
				WithHint("Drop --subject/auth.subject, or point at a service account key.")
		}
		hint := "Pass --credentials <client_secrets.json> or --service-account <key.json>, set GSC_CREDENTIALS / GSC_SERVICE_ACCOUNT / GOOGLE_APPLICATION_CREDENTIALS, or run `gsc config set auth.credentials_path <path>`."
		return nil, errs.New(errs.CodeAuthMissing, err.Error()).WithHint(hint)
	}
	return creds, nil
}

// resolveCredentials picks the credentials file and optional delegation
// subject, honoring flags > env > config. Within each layer a service account
// source wins over a generic one, so a CI job can export GSC_SERVICE_ACCOUNT
// without disturbing a user's stored OAuth setup.
func (s *State) resolveCredentials() (path, subject string) {
	cfg := s.Cfg
	if cfg == nil {
		cfg = config.Default()
	}
	path = firstNonEmpty(
		s.SAPath,
		s.CredsPath,
		os.Getenv("GSC_SERVICE_ACCOUNT"),
		os.Getenv("GSC_CREDENTIALS"),
		os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		cfg.Auth.ServiceAccountPath,
		cfg.Auth.CredentialsPath,
	)
	subject = firstNonEmpty(s.Subject, os.Getenv("GSC_SUBJECT"), cfg.Auth.Subject)
	return config.ExpandHome(path), subject
}

// rememberCredentials persists a freshly verified credentials path to config so
// later commands find it without flags or env vars.
func (s *State) rememberCredentials(creds *auth.Credentials) {
	key := "auth.credentials_path"
	current := s.Cfg.Auth.CredentialsPath
	if creds.Kind == auth.KindServiceAccount {
		key = "auth.service_account_path"
		current = s.Cfg.Auth.ServiceAccountPath
	}
	if current != "" {
		return
	}
	if err := s.Cfg.Set(key, creds.Path); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not save credentials path to config: "+err.Error())
	}
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
			// Data dir resolution (~/.config/gsc/).
			dataDir, err := config.DataDir()
			if err != nil {
				return err
			}
			// Cache dir resolution.
			cacheDir := cfg.Cache.Dir
			if cacheDir == "" {
				cacheDir = filepath.Join(dataDir, "cache")
			}
			st.Cache = cache.New(cacheDir, cfg.TTL())
			// Quota store.
			st.Quota = quota.New(filepath.Join(dataDir, "quota.json"))
			st.Quota.WarnFn = func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) }
			// Audit log.
			st.Audit = audit.New(filepath.Join(dataDir, "audit.log"))
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
	root.PersistentFlags().StringVar(&st.CredsPath, "credentials", "", "path to a Google client_secrets.json or service account key (overrides config + env)")
	root.PersistentFlags().StringVar(&st.SAPath, "service-account", "", "path to a service account key JSON (no browser login required)")
	root.PersistentFlags().StringVar(&st.Subject, "subject", "", "Workspace user to impersonate via domain-wide delegation (service account only)")
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
		newPagespeedCmd(),
		newCruxCmd(),
		newCwvCmd(),
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
