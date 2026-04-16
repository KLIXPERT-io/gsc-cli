# gsc — Google Search Console CLI

[![Latest release](https://img.shields.io/github/v/release/KLIXPERT-io/gsc-cli?sort=semver)](https://github.com/KLIXPERT-io/gsc-cli/releases/latest)

A fast, LLM-friendly Go CLI that wraps the Google Search Console API v1.

- Single static binary, <25MB, cold-start <50ms on cache hits.
- JSON default output, CSV/table supported, TTY auto-detected.
- Structured errors with machine-readable codes + exit codes.
- Local disk cache with TTLs, quota tracking, audit log for writes.
- OS keychain token storage with file fallback (`~/.config/gsc/token.json`, 0600).

## Install

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/refs/heads/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/refs/heads/main/install.ps1 | iex
```

Or `go install github.com/KLIXPERT-io/gsc-cli/cmd/gsc@latest`. See [INSTALL.md](./INSTALL.md) for manual downloads, version pinning, and checksum verification.

After install, `gsc` keeps itself up to date in the background — see [INSTALL.md](./INSTALL.md#auto-update) for details and how to opt out.

## Use with local LLM agents (Claude, Gemini, …)

`gsc` ships an agent skill that teaches LLM coding agents how to drive the CLI safely (commands, flags, JSON envelope, exit codes, quota awareness). Install it into any tool that supports the [`skills`](https://github.com/anthropics/skills) format:

```bash
npx skills add https://github.com/KLIXPERT-io/gsc-cli/skills --skill gsc-cli
```

This drops `gsc-cli/SKILL.md` into your agent's skill directory (Claude Code, Gemini CLI, etc.). Re-run any time to pull updates.

## Setup

### 1. Create a Google Cloud OAuth client (required)

You need an OAuth 2.0 **client secret** file so `gsc` can authenticate with the Search Console API on your behalf.

1. Go to [console.cloud.google.com](https://console.cloud.google.com/) and create a project (or pick an existing one).
2. **Enable the Search Console API:** navigate to [APIs & Services → Library](https://console.cloud.google.com/apis/library), search for "Google Search Console API", and click **Enable**.
3. **Configure the OAuth consent screen:** go to [APIs & Services → OAuth consent screen](https://console.cloud.google.com/apis/credentials/consent). Choose "External" (or "Internal" for Workspace orgs), fill in the required fields, and add the scope `https://www.googleapis.com/auth/webmasters.readonly` (or `webmasters` for write access).
4. **Create credentials:** go to [APIs & Services → Credentials](https://console.cloud.google.com/apis/credentials), click **Create Credentials → OAuth client ID**, select **Desktop app**, and download the resulting `client_secrets.json`.
5. Point `gsc` at the file and log in:

   ```bash
   gsc config set auth.credentials_path ~/secrets/client_secrets.json
   gsc auth login
   ```

   The login flow starts a local loopback server on `127.0.0.1:<random-port>`, opens your browser, and stores tokens in your OS keychain (with a file fallback).

### 2. CrUX API key (required for `gsc cwv` / `gsc crux`)

The Chrome UX Report API uses an **API key** (not OAuth). To set it up:

1. In the same GCP project, enable the **Chrome UX Report API:** [APIs & Services → Library](https://console.cloud.google.com/apis/library/chromeuxreport.googleapis.com) → **Enable**.
2. Go to [APIs & Services → Credentials](https://console.cloud.google.com/apis/credentials), click **Create Credentials → API key**. Optionally restrict it to the Chrome UX Report API.
3. Configure `gsc`:

   ```bash
   gsc config set crux.api_key YOUR_API_KEY
   # or use an env var:
   export GSC_CRUX_API_KEY=YOUR_API_KEY
   ```

### 3. PageSpeed Insights API (optional, for `gsc pagespeed`)

PSI works with the same OAuth credentials, but the API must be enabled:

1. Enable the **PageSpeed Insights API:** [APIs & Services → Library](https://console.cloud.google.com/apis/library/pagespeedonline.googleapis.com) → **Enable**.
2. Optionally set an API key for higher rate limits (same process as CrUX):

   ```bash
   gsc config set psi.api_key YOUR_API_KEY
   # or: export GSC_PSI_API_KEY=YOUR_API_KEY
   ```

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

# Include fresh (non-finalized) last-2-days data
gsc analytics query sc-domain:example.com --dimensions query --data-state all

# Force byPage aggregation on a domain property
gsc analytics query sc-domain:example.com --dimensions query --aggregation byPage

# Auto-paginate past the 25k row cap and stream CSV to stdout
gsc analytics query sc-domain:example.com --dimensions query,page --all --output csv > queries.csv

# OR-of-AND filter groups: (query~brand AND device=MOBILE) OR (country=usa)
gsc analytics query sc-domain:example.com \
  --filter-group "query~brand,device=MOBILE" --filter-group "country=usa"

# Overview with fresh data / domain-rollup aggregation
gsc analytics overview sc-domain:example.com --data-state all --aggregation byProperty

# Inspect URLs in bulk
cat urls.txt | gsc urls inspect sc-domain:example.com -

# Sitemaps
gsc sitemaps list sc-domain:example.com
gsc sitemaps submit sc-domain:example.com https://www.example.com/sitemap.xml
# Remove a sitemap (destructive — requires --yes in non-TTY)
gsc sitemaps remove sc-domain:example.com https://www.example.com/sitemap.xml --yes

# Check quota
gsc quota
```

## Performance & Core Web Vitals

`gsc` wraps the **PageSpeed Insights** (Lighthouse + field data) and **Chrome UX Report** (real-user CWV, current + historical) APIs, plus a convenience `gsc cwv` that picks the right source and prints a pass/fail summary.

Both APIs must be enabled in the GCP project backing your OAuth client. Turn them on at [console.cloud.google.com/apis/library](https://console.cloud.google.com/apis/library) — search for "PageSpeed Insights API" and "Chrome UX Report API". On a 403 `SERVICE_DISABLED` the CLI surfaces a direct link.

```bash
# One-shot CWV triage for a URL (mobile by default, falls back to origin on 404)
gsc cwv https://example.com/pricing

# Fail CI when any metric is poor
gsc cwv https://example.com/pricing --fail-on poor

# Desktop form-factor, JSON output
gsc cwv https://example.com/ --form-factor desktop --json

# Full Lighthouse + field-data audit (mobile strategy)
gsc pagespeed run https://example.com/pricing

# Desktop run, restrict to the categories you care about
gsc pagespeed run https://example.com/ --strategy desktop --category performance,seo

# Current CrUX record at origin level, phone form-factor only
gsc crux query https://example.com --origin --form-factor phone

# 12 weekly collection periods of LCP + INP (table form)
gsc crux history https://example.com/ --metric lcp,inp --weeks 12
```

Quota buckets `psi` (25,000/day) and `crux` (150 QPS best-effort) are tracked alongside the GSC buckets — inspect with `gsc quota`. Cache TTLs default to 24h and are tunable via `cache.ttl_psi` and `cache.ttl_crux` in `config.toml` (CrUX refreshes monthly; PSI is per-run but rarely needs sub-daily refresh).

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
# dir = "~/custom/cache"  # override cache location (default: ~/.config/gsc/cache)
default_ttl = "15m"

[logging]
verbose = false
format = "text"
```

Manage it with `gsc config get|set|path|list`.

## Data directory

`gsc` stores persistent data under `~/.config/gsc/`:

- `~/.config/gsc/cache/` — cached API responses
- `~/.config/gsc/quota.json` — daily/rolling quota counters
- `~/.config/gsc/audit.log` — one JSON line per mutation (rotated at 10MB)
- `~/.config/gsc/update-state.json` — auto-update state

## Flags shared across commands

- `--output json|csv|table` (default: `json`, or `table` on TTY)
- `--no-cache` — bypass cache reads and writes
- `--refresh` — bypass read but write fresh result
- `--cache-ttl <duration>` — override TTL for this call
- `--yes` — required for destructive ops (`sites remove`, `sitemaps remove`) when not on a TTY
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
