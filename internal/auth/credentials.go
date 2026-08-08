// Credential resolution: one entry point that works for both an installed-app
// OAuth client (interactive `gsc auth login`) and a service account key file.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

// Kind identifies which authentication flavor a credentials file provides.
type Kind string

const (
	// KindOAuth is an installed-app OAuth client (client_secrets.json); it
	// requires `gsc auth login` and a stored user token.
	KindOAuth Kind = "oauth"
	// KindServiceAccount is a service account key file; it mints its own
	// tokens and never needs a browser.
	KindServiceAccount Kind = "service_account"
)

// ErrNoCredentials indicates nothing pointed the CLI at a credentials file.
var ErrNoCredentials = errors.New("no credentials configured: pass --credentials or --service-account, set GSC_CREDENTIALS / GSC_SERVICE_ACCOUNT / GOOGLE_APPLICATION_CREDENTIALS, or run `gsc config set auth.credentials_path <path>`")

// ErrSubjectNotSupported indicates a delegation subject was set for
// credentials that cannot impersonate.
var ErrSubjectNotSupported = errors.New("subject impersonation needs service account credentials")

// Detect reports which kind of credentials the file at path holds.
func Detect(path string) (Kind, error) {
	if path == "" {
		return "", ErrNoCredentials
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read credentials: %w", err)
	}
	var probe struct {
		Installed json.RawMessage `json:"installed"`
		Web       json.RawMessage `json:"web"`
		Type      string          `json:"type"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return "", fmt.Errorf("parse credentials %s: %w", path, err)
	}
	switch {
	case probe.Type == "service_account":
		return KindServiceAccount, nil
	case len(probe.Installed) > 0 || len(probe.Web) > 0:
		return KindOAuth, nil
	}
	return "", fmt.Errorf("unrecognized credentials file %s: expected an installed-app OAuth client or a service account key", path)
}

// Credentials is a resolved credential of either flavor, ready to produce an
// authenticated HTTP client.
type Credentials struct {
	Kind    Kind
	Path    string
	Subject string // domain-wide delegation subject (service account only)

	OAuth *oauth2.Config     // KindOAuth
	JWT   *jwt.Config        // KindServiceAccount
	Key   *ServiceAccountKey // KindServiceAccount
}

// Resolve loads the credentials file at path and prepares whichever flow it
// implies. The kind is detected from the file's contents, so a single
// --credentials/GSC_CREDENTIALS setting accepts either flavor.
func Resolve(path, subject string) (*Credentials, error) {
	kind, err := Detect(path)
	if err != nil {
		return nil, err
	}
	c := &Credentials{Kind: kind, Path: path, Subject: subject}
	switch kind {
	case KindServiceAccount:
		jwtCfg, key, err := LoadServiceAccount(path, subject)
		if err != nil {
			return nil, err
		}
		c.JWT, c.Key = jwtCfg, key
	default:
		if subject != "" {
			return nil, fmt.Errorf("%w, but %s is an OAuth client", ErrSubjectNotSupported, path)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			return nil, err
		}
		c.OAuth = cfg
	}
	return c, nil
}

// NeedsLogin reports whether the credentials require an interactive
// `gsc auth login` before they can be used.
func (c *Credentials) NeedsLogin() bool { return c.Kind == KindOAuth }

// TokenSource returns an auto-refreshing token source. Service account sources
// are lazy — no network happens until a token is actually needed, so fully
// cached commands stay offline. Use Verify to force the check.
func (c *Credentials) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if c.Kind == KindServiceAccount {
		return c.JWT.TokenSource(ctx), nil
	}
	ts, _, err := TokenSource(ctx, c.OAuth)
	return ts, err
}

// HTTPClient returns an authenticated *http.Client.
func (c *Credentials) HTTPClient(ctx context.Context) (*http.Client, error) {
	ts, err := c.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, ts), nil
}

// Verify acquires an access token, proving the credentials work end to end.
func (c *Credentials) Verify(ctx context.Context) (*oauth2.Token, error) {
	ts, err := c.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	tok, err := ts.Token()
	if err != nil {
		if c.Kind == KindServiceAccount {
			return nil, fmt.Errorf("service account token: %w", err)
		}
		return nil, err
	}
	return tok, nil
}

// Identity returns a stable per-credential string used for cache keying, so
// switching accounts never serves another identity's cached rows.
func (c *Credentials) Identity() string {
	if c.Kind == KindServiceAccount {
		id := "sa:" + c.Key.ClientEmail
		if c.Subject != "" {
			id += "#" + c.Subject
		}
		return id
	}
	tok, _ := LoadToken()
	return Identity(c.OAuth, tok)
}
