package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/KLIXPERT-io/gsc-cli/internal/auth"
	"github.com/KLIXPERT-io/gsc-cli/internal/config"
	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAuthCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands",
	}
	c.AddCommand(newAuthLoginCmd(), newAuthStatusCmd(), newAuthLogoutCmd())
	return c
}

func newAuthLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Run the OAuth 2.0 loopback flow and store tokens",
		Long: `Starts a local loopback server on 127.0.0.1, opens the Google authorization
URL in your browser, captures the auth code, and exchanges it for tokens.
Tokens are stored in the OS keychain when available, with a secure fallback
to ~/.config/gsc/token.json (mode 0600).

Examples:
  gsc auth login --credentials ~/secrets/gsc-client.json
  GSC_CREDENTIALS=~/secrets/gsc-client.json gsc auth login`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := getState(cmd)
			credsPath := s.CredsPath
			if credsPath == "" {
				credsPath = os.Getenv("GSC_CREDENTIALS")
			}
			if credsPath == "" {
				credsPath = config.ExpandHome(s.Cfg.Auth.CredentialsPath)
			}
			cfg, err := auth.LoadConfig(credsPath)
			if err != nil {
				return errs.New(errs.CodeAuthMissing, err.Error()).WithHint("Pass --credentials <path> or set auth.credentials_path.")
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			tok, err := auth.Login(ctx, cfg, openBrowser)
			if err != nil {
				return errs.New(errs.CodeAuthDenied, err.Error())
			}
			// Persist the credentials path to config so subsequent commands can find it.
			if s.Cfg.Auth.CredentialsPath == "" {
				if err := s.Cfg.Set("auth.credentials_path", credsPath); err != nil {
					fmt.Fprintln(os.Stderr, "warning: could not save credentials path to config: "+err.Error())
				}
			}
			meta := output.Meta{APICalls: 1}
			return emit(cmd, map[string]any{
				"ok":                true,
				"has_refresh":       tok.RefreshToken != "",
				"expiry":            tok.Expiry,
				"credentials_path":  credsPath,
				"config_path":       func() string { p, _ := config.Path(); return p }(),
			}, meta, nil, nil)
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current auth identity and token validity",
		Long: `Reports whether a token is stored, whether it's currently valid, and
the OAuth client_id it's associated with.

Examples:
  gsc auth status
  gsc auth status --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := getState(cmd)
			credsPath := s.CredsPath
			if credsPath == "" {
				credsPath = os.Getenv("GSC_CREDENTIALS")
			}
			if credsPath == "" {
				credsPath = config.ExpandHome(s.Cfg.Auth.CredentialsPath)
			}
			tok, err := auth.LoadToken()
			if err != nil {
				return errs.New(errs.CodeAuthMissing, err.Error()).WithHint("Run `gsc auth login`.")
			}
			status := map[string]any{
				"has_token":   true,
				"has_refresh": tok.RefreshToken != "",
				"expiry":      tok.Expiry,
				"valid":       tok.Valid(),
			}
			if credsPath != "" {
				status["credentials_path"] = credsPath
				if cfg, err := auth.LoadConfig(credsPath); err == nil {
					status["client_id"] = cfg.ClientID
				}
			}
			return emit(cmd, status, output.Meta{APICalls: 0}, nil, nil)
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.DeleteToken(); err != nil {
				return errs.New(errs.CodeGeneric, err.Error())
			}
			fmt.Fprintln(os.Stderr, "logged out")
			return emit(cmd, map[string]any{"ok": true}, output.Meta{}, nil, nil)
		},
	}
}
