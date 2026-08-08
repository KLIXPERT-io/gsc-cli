// Service account (two-legged JWT) authentication.
//
// A service account key file needs no interactive login: the CLI signs a JWT
// with the key and exchanges it for a short-lived access token on demand.
// Grant the service account access by adding its client_email as a user on the
// Search Console property (Settings → Users and permissions), or use
// domain-wide delegation with a subject.
package auth

import (
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
)

// ServiceAccountKey holds the non-secret fields of a service account key file
// that are useful for `gsc auth status` and cache keying.
type ServiceAccountKey struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	ClientEmail  string `json:"client_email"`
	ClientID     string `json:"client_id"`
	PrivateKeyID string `json:"private_key_id"`
}

// LoadServiceAccount parses a service account key file into a JWT config.
//
// subject, when non-empty, turns on domain-wide delegation: the service
// account impersonates that Workspace user. This requires the delegation to be
// granted for the SA's client_id in the Google Workspace Admin console.
func LoadServiceAccount(path, subject string) (*jwt.Config, *ServiceAccountKey, error) {
	if path == "" {
		return nil, nil, ErrNoCredentials
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read service account key: %w", err)
	}
	var key ServiceAccountKey
	if err := json.Unmarshal(b, &key); err != nil {
		return nil, nil, fmt.Errorf("parse service account key %s: %w", path, err)
	}
	if key.Type != "service_account" {
		return nil, nil, fmt.Errorf("%s is not a service account key (type=%q)", path, key.Type)
	}
	cfg, err := google.JWTConfigFromJSON(b, Scopes...)
	if err != nil {
		return nil, nil, fmt.Errorf("parse service account key %s: %w", path, err)
	}
	cfg.Subject = subject
	return cfg, &key, nil
}
