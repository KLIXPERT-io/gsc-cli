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
		Short: "Authorize the CLI (OAuth loopback flow, or verify a service account)",
		Long: `Prepares credentials for use. The flow depends on the credentials file:

OAuth client (client_secrets.json)
  Starts a local loopback server on 127.0.0.1, opens the Google authorization
  URL in your browser, captures the auth code, and exchanges it for tokens.
  Tokens are stored in the OS keychain when available, with a secure fallback
  to ~/.config/gsc/token.json (mode 0600).

Service account key
  No browser is involved. The key is validated by minting an access token, and
  its path is remembered for later commands. Grant it access by adding the
  service account's client_email as a user on the Search Console property.

Examples:
  gsc auth login --credentials ~/secrets/gsc-client.json
  gsc auth login --service-account ~/secrets/gsc-sa.json
  GSC_SERVICE_ACCOUNT=~/secrets/gsc-sa.json gsc auth login`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := getState(cmd)
			creds, err := s.credentials()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			if creds.Kind == auth.KindServiceAccount {
				fmt.Fprintln(os.Stderr, "service account credentials detected — no browser login required")
				tok, err := creds.Verify(ctx)
				if err != nil {
					return errs.New(errs.CodeAuthDenied, err.Error()).
						WithHint("Confirm the key is still active and that the Search Console API is enabled for its project.")
				}
				s.rememberCredentials(creds)
				return emit(cmd, map[string]any{
					"ok":                   true,
					"mode":                 string(creds.Kind),
					"client_email":         creds.Key.ClientEmail,
					"project_id":           creds.Key.ProjectID,
					"subject":              creds.Subject,
					"expiry":               tok.Expiry,
					"service_account_path": creds.Path,
					"config_path":          configPath(),
				}, output.Meta{APICalls: 1}, nil, nil)
			}

			tok, err := auth.Login(ctx, creds.OAuth, openBrowser)
			if err != nil {
				return errs.New(errs.CodeAuthDenied, err.Error())
			}
			s.rememberCredentials(creds)
			return emit(cmd, map[string]any{
				"ok":               true,
				"mode":             string(creds.Kind),
				"has_refresh":      tok.RefreshToken != "",
				"expiry":           tok.Expiry,
				"credentials_path": creds.Path,
				"config_path":      configPath(),
			}, output.Meta{APICalls: 1}, nil, nil)
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current auth mode, identity, and token validity",
		Long: `Reports which credentials are active, whether they currently yield a
valid access token, and the identity they authenticate as — the OAuth
client_id, or the service account's client_email.

Examples:
  gsc auth status
  gsc auth status --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := getState(cmd)
			creds, err := s.credentials()
			if err != nil {
				return err
			}
			status := map[string]any{
				"mode":             string(creds.Kind),
				"credentials_path": creds.Path,
			}

			if creds.Kind == auth.KindServiceAccount {
				status["client_email"] = creds.Key.ClientEmail
				status["project_id"] = creds.Key.ProjectID
				status["private_key_id"] = creds.Key.PrivateKeyID
				status["subject"] = creds.Subject
				status["needs_login"] = false
				tok, err := creds.Verify(cmd.Context())
				status["valid"] = err == nil
				if err != nil {
					status["error"] = err.Error()
				} else {
					status["expiry"] = tok.Expiry
				}
				return emit(cmd, status, output.Meta{APICalls: 1}, nil, nil)
			}

			status["client_id"] = creds.OAuth.ClientID
			status["needs_login"] = true
			tok, err := auth.LoadToken()
			if err != nil {
				return errs.New(errs.CodeAuthMissing, err.Error()).WithHint("Run `gsc auth login`.")
			}
			status["has_token"] = true
			status["has_refresh"] = tok.RefreshToken != ""
			status["expiry"] = tok.Expiry
			status["valid"] = tok.Valid()
			return emit(cmd, status, output.Meta{APICalls: 0}, nil, nil)
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete stored OAuth tokens",
		Long: `Removes the stored OAuth token from the keychain and the file fallback.

Service account keys are files you manage yourself, so logout does not touch
them; remove the key file or clear auth.service_account_path instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.DeleteToken(); err != nil {
				return errs.New(errs.CodeGeneric, err.Error())
			}
			fmt.Fprintln(os.Stderr, "logged out")
			return emit(cmd, map[string]any{"ok": true}, output.Meta{}, nil, nil)
		},
	}
}

func configPath() string {
	p, _ := config.Path()
	return p
}
