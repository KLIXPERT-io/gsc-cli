// Package auth handles Google OAuth loopback flow and token storage per FR-3.
//
// Tokens are stored in the OS keychain (via zalando/go-keyring) when available,
// with a fallback to ~/.config/gsc/token.json (mode 0600).
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	searchconsole "google.golang.org/api/searchconsole/v1"
)

const (
	keyringService = "gsc-cli"
	keyringAccount = "default"
)

// Scopes required by the CLI.
var Scopes = []string{searchconsole.WebmastersScope}

// ClientSecrets represents the installed-app OAuth client json.
type ClientSecrets struct {
	Installed struct {
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthURI      string   `json:"auth_uri"`
		TokenURI     string   `json:"token_uri"`
	} `json:"installed"`
}

// LoadConfig reads a client_secrets.json file and returns an OAuth2 config.
func LoadConfig(credentialsPath string) (*oauth2.Config, error) {
	if credentialsPath == "" {
		return nil, errors.New("no credentials path configured: set auth.credentials_path or pass --credentials")
	}
	b, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	cfg, err := google.ConfigFromJSON(b, Scopes...)
	if err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return cfg, nil
}

// Login runs the OAuth loopback flow and stores the resulting token.
func Login(ctx context.Context, cfg *oauth2.Config, openBrowser func(url string) error) (*oauth2.Token, error) {
	// Bind a random local port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer lis.Close()
	redirect := fmt.Sprintf("http://%s/callback", lis.Addr().String())
	cfg.RedirectURL = redirect

	state, err := randomString(24)
	if err != nil {
		return nil, err
	}
	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintln(os.Stderr, "Open this URL in your browser to authorize:")
	fmt.Fprintln(os.Stderr, authURL)
	if openBrowser != nil {
		_ = openBrowser(authURL)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q, _ := url.ParseQuery(r.URL.RawQuery)
			if q.Get("state") != state {
				http.Error(w, "state mismatch", http.StatusBadRequest)
				errCh <- errors.New("oauth state mismatch")
				return
			}
			if e := q.Get("error"); e != "" {
				http.Error(w, e, http.StatusBadRequest)
				errCh <- fmt.Errorf("oauth denied: %s", e)
				return
			}
			fmt.Fprintln(w, "Authorization complete. You can close this tab.")
			codeCh <- q.Get("code")
		}),
	}
	go srv.Serve(lis)
	defer srv.Shutdown(context.Background())

	var code string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case code = <-codeCh:
	case <-time.After(5 * time.Minute):
		return nil, errors.New("timeout waiting for authorization")
	}
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	if err := SaveToken(tok); err != nil {
		return nil, err
	}
	return tok, nil
}

// TokenSource returns an auto-refreshing token source wired to the loaded token.
func TokenSource(ctx context.Context, cfg *oauth2.Config) (oauth2.TokenSource, *oauth2.Token, error) {
	tok, err := LoadToken()
	if err != nil {
		return nil, nil, err
	}
	ts := cfg.TokenSource(ctx, tok)
	// Try refreshing once to detect expiry early.
	refreshed, err := ts.Token()
	if err != nil {
		return nil, nil, fmt.Errorf("refresh token: %w", err)
	}
	if refreshed.AccessToken != tok.AccessToken {
		_ = SaveToken(refreshed)
	}
	return ts, refreshed, nil
}

// HTTPClient returns an authenticated http.Client.
func HTTPClient(ctx context.Context, cfg *oauth2.Config) (*http.Client, error) {
	ts, _, err := TokenSource(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, ts), nil
}

// ===== Token storage =====

func tokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gsc", "token.json"), nil
}

// SaveToken stores the token in the OS keychain (with file fallback).
func SaveToken(tok *oauth2.Token) error {
	b, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	if err := keyring.Set(keyringService, keyringAccount, string(b)); err == nil {
		return nil
	}
	// Fallback: file at ~/.config/gsc/token.json (0600)
	p, err := tokenFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// LoadToken reads the token, preferring the keychain, falling back to the file.
func LoadToken() (*oauth2.Token, error) {
	if s, err := keyring.Get(keyringService, keyringAccount); err == nil && s != "" {
		var tok oauth2.Token
		if err := json.Unmarshal([]byte(s), &tok); err == nil {
			return &tok, nil
		}
	}
	p, err := tokenFilePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoToken
		}
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// DeleteToken removes any stored tokens.
func DeleteToken() error {
	_ = keyring.Delete(keyringService, keyringAccount)
	p, err := tokenFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ErrNoToken indicates no stored token was found.
var ErrNoToken = errors.New("no token stored; run `gsc auth login`")

// Identity returns a stable identifier for the current token (for cache keying).
// We use the client_id + a short hash of the refresh token (if present).
func Identity(cfg *oauth2.Config, tok *oauth2.Token) string {
	id := cfg.ClientID
	if tok != nil && tok.RefreshToken != "" {
		id += ":" + tok.RefreshToken[:min(8, len(tok.RefreshToken))]
	}
	return id
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
