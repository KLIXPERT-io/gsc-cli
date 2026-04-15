# gsc — Google Search Console CLI

A fast, LLM-friendly Go CLI that wraps the Google Search Console API v1.

- Single static binary, <25MB, cold-start <50ms on cache hits.
- JSON default output, CSV/table supported, TTY auto-detected.
- Structured errors with machine-readable codes + exit codes.
- Local disk cache with TTLs, quota tracking, audit log for writes.
- OS keychain token storage with file fallback (`~/.config/gsc/token.json`, 0600).

## Install

```bash
go install github.com/KLIXPERT-io/gsc-cli/cmd/gsc@latest
```

Or download a release from the [Releases page](https://github.com/KLIXPERT-io/gsc-cli/releases).

## Setup

1. Create a Google Cloud project. Enable the **Search Console API**.
2. Configure an OAuth consent screen (internal or external).
3. Create an **OAuth 2.0 Client ID** of type **Desktop app**. Download the `client_secrets.json`.
4. Tell `gsc` where it is:

   ```bash
   gsc config set auth.credentials_path ~/secrets/gsc-client.json
   gsc auth login
   ```

   The login flow starts a local loopback server on `127.0.0.1:<random-port>`, opens your browser, and stores tokens in your OS keychain (with a file fallback).

## Quick tour

```bash
gsc sites list
gsc sites get sc-domain:example.com

# Top 50 queries, mobile only, last 28 days
gsc analytics query sc-domain:example.com \
  --dimensions query --filter device=MOBILE --limit 50

# Daily clicks time-series as CSV
gsc analytics query sc-domain:example.com \
  --group-by date --range last-3m --output csv

# Compare CTR vs previous period
gsc analytics query sc-domain:example.com \
  --dimensions query --range last-28d --compare previous-period

# Inspect URLs in bulk
cat urls.txt | gsc urls inspect sc-domain:example.com -

# Sitemaps
gsc sitemaps list sc-domain:example.com
gsc sitemaps submit sc-domain:example.com https://www.example.com/sitemap.xml

# Check quota
gsc quota
```

## Output

Every JSON response has the envelope:

```json
{
  "data": { ... },
  "meta": {
    "cached": true,
    "cached_at": "2026-04-15T14:30:00Z",
    "ttl_remaining_sec": 543,
    "api_calls": 0
  }
}
```

Errors are always JSON on stderr, even in CSV mode:

```json
{"error":{"code":"quota_exceeded","message":"...","hint":"...","retriable":true,"retry_after_sec":3600}}
```

Exit codes: `0` ok · `1` generic · `2` auth · `3` quota/rate · `4` not-found · `5` validation · `6` network.

## Config

`~/.config/gsc/config.toml`:

```toml
[auth]
credentials_path = "~/secrets/gsc-client.json"

[defaults]
property = "sc-domain:example.com"
output = "json"
range = "last-28d"

[cache]
dir = "./.gsc/cache"
default_ttl = "15m"

[logging]
verbose = false
format = "text"
```

Manage it with `gsc config get|set|path|list`.

## Local state

`gsc` writes to `./.gsc/` in your working directory:

- `./.gsc/cache/` — cached API responses
- `./.gsc/quota.json` — daily/rolling quota counters
- `./.gsc/audit.log` — one JSON line per mutation (rotated at 10MB)

**Add `.gsc/` to your `.gitignore`** if you run `gsc` inside a git repo.

## Flags shared across commands

- `--output json|csv|table` (default: `json`, or `table` on TTY)
- `--no-cache` — bypass cache reads and writes
- `--refresh` — bypass read but write fresh result
- `--cache-ttl <duration>` — override TTL for this call
- `--yes` — required for destructive ops (`sites remove`) when not on a TTY
- `-v, --verbose` / `-q, --quiet`
- `--log-format text|json`

## Quota

The URL Inspection API is limited to 2000 requests/day. `gsc` warns at 1000, 1500, and every +100 after that, and hard-stops at 2000 with a `quota_exceeded` error. Search Analytics is rate-limited at 1200 QPM (best-effort in-memory rolling window).

## Non-goals

- No web UI, no daemon mode.
- No non-GSC sources (GA4, Bing, Ahrefs).
- No visualization — charts are the caller's job (pipe CSV into your plotting tool of choice).

## License

MIT
