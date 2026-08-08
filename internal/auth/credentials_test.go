package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A throwaway RSA key so JWTConfigFromJSON can parse the fixture.
const testPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIBOwIBAAJBALPGWjRDBGqQrQpjMEQrjPAaBnQBVMzD7WMoLYK6JvBmwUmwrE5Y
0OwPP7Rr5m3PWyOWEo4LTAaVOaZaOEFN3RUCAwEAAQJAWzoZJgSHhBmVjxeS6PQ4
9uNw0v9SD8s7z6UhrfCGwSMH6BeAvv5g0uYJc0EQTIRZBEHVMgVjPIJCLQGWmvED
9QIhANMwpJKlkVYcYoO6M0PZ5eDDzB2rXA0Efc0DHnb0LmITAiEA2K0uYnjHtHVA
LKQqmmzHZSjRPjK3JLNq4iScvXSCqmcCIQCsAr7pJ2YlYYNGLzTiEfhMV3Ei0FMk
tOEuMOLbtqXHZQIgWLR6qRLwuIF0IsK7ykm1KMS3aP9WNqQpb1cGA5G0PWECIQC2
FTgQE3jvNAgQKe6nzYAlLNc4tPeVTFhkNhKENGm5Bw==
-----END RSA PRIVATE KEY-----`

func writeFixture(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func serviceAccountFixture(t *testing.T) string {
	t.Helper()
	body := `{
  "type": "service_account",
  "project_id": "data-265420",
  "private_key_id": "abc123",
  "private_key": "` + escapeNewlines(testPrivateKey) + `",
  "client_email": "search-console@data-265420.iam.gserviceaccount.com",
  "client_id": "999",
  "token_uri": "https://oauth2.googleapis.com/token"
}`
	return writeFixture(t, "sa.json", body)
}

func escapeNewlines(s string) string {
	out := make([]byte, 0, len(s)+32)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, '\\', 'n')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

const oauthClientJSON = `{"installed":{"client_id":"cid.apps.googleusercontent.com","client_secret":"secret","redirect_uris":["http://localhost"],"auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token"}}`

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Kind
	}{
		{"service account key", serviceAccountFixture(t), KindServiceAccount},
		{"installed app client", writeFixture(t, "oauth.json", oauthClientJSON), KindOAuth},
		{"web app client", writeFixture(t, "web.json", `{"web":{"client_id":"cid","client_secret":"s"}}`), KindOAuth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Detect(tc.path)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got != tc.want {
				t.Errorf("Detect = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectRejectsUnknown(t *testing.T) {
	if _, err := Detect(""); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("empty path: got %v, want ErrNoCredentials", err)
	}
	if _, err := Detect(writeFixture(t, "authorized_user.json", `{"type":"authorized_user","client_id":"x"}`)); err == nil {
		t.Error("expected an error for an unsupported credential type")
	}
	if _, err := Detect(writeFixture(t, "junk.json", `not json`)); err == nil {
		t.Error("expected an error for a non-JSON file")
	}
}

func TestResolveServiceAccount(t *testing.T) {
	creds, err := Resolve(serviceAccountFixture(t), "user@example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if creds.Kind != KindServiceAccount {
		t.Fatalf("Kind = %q, want %q", creds.Kind, KindServiceAccount)
	}
	if creds.NeedsLogin() {
		t.Error("service account credentials should not need a login")
	}
	if creds.JWT.Subject != "user@example.com" {
		t.Errorf("JWT.Subject = %q, want the delegation subject", creds.JWT.Subject)
	}
	if creds.JWT.Email != "search-console@data-265420.iam.gserviceaccount.com" {
		t.Errorf("JWT.Email = %q", creds.JWT.Email)
	}
	// Identity must be stable, and distinguish the impersonated subject so one
	// subject never reads another's cached rows.
	want := "sa:search-console@data-265420.iam.gserviceaccount.com#user@example.com"
	if got := creds.Identity(); got != want {
		t.Errorf("Identity = %q, want %q", got, want)
	}
}

func TestResolveOAuthRejectsSubject(t *testing.T) {
	p := writeFixture(t, "oauth.json", oauthClientJSON)
	if _, err := Resolve(p, "user@example.com"); !errors.Is(err, ErrSubjectNotSupported) {
		t.Errorf("got %v, want ErrSubjectNotSupported", err)
	}
	creds, err := Resolve(p, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if creds.Kind != KindOAuth || !creds.NeedsLogin() {
		t.Errorf("Kind = %q, NeedsLogin = %v", creds.Kind, creds.NeedsLogin())
	}
}
