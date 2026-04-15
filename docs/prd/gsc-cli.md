---
title: gsc — Google Search Console CLI
status: draft
version: 0.1
owner: flo@codeline.co
created: 2026-04-15
updated: 2026-04-15
categories: [cli-tool, integration, user-facing]
---

## 1. Summary & Success Metrics

`gsc` is a fast, LLM-friendly Go CLI that wraps the Google Search Console (GSC) API v1. It is designed primarily to be driven by an LLM (e.g., Claude) on behalf of an SEO operator, but also usable directly by humans. Output is structured (JSON default, CSV for analytics), piping-safe, caches repeat reads locally, and surfaces errors with structured codes so agents can recover without human intervention.

**Success = "I can manage all my GSC properties from the CLI and do everything the web UI offers, through an LLM, without opening the GSC web app."**

Operational success signals:
- All 9 in-scope commands (see §3) functional on v1 release.
- An LLM given only `gsc --help` + per-subcommand `--help` can invoke every command correctly zero-shot (no prior examples).
- p95 cached read latency < 50ms (local disk hit); p95 uncached < 1500ms (network dependent).
- Zero "quota exceeded without warning" incidents once quota tracking is live.

## 2. Problem & Motivation

**Why now.** Operating GSC through its web UI is slow, un-scriptable, and impossible to hand to an LLM agent. Existing wrappers are either Python/Node (slow startup, heavy install) or proprietary SaaS. For an LLM-driven SEO workflow, the operator needs a binary that starts in <50ms, emits structured data, and fails with machine-readable errors.

**Cost of inaction.** Every SEO analysis task (find slipping queries, audit indexing on a page set, submit sitemaps across N properties) requires manual clicks or bespoke scripts. LLMs currently cannot perform these tasks autonomously against GSC.

## 3. Scope — In / Out

### In scope (v1)

Single-binary Go CLI with the following command tree (Cobra-style, kebab/noun verbs):

| Command | Purpose |
|---|---|
| `gsc auth login` | OAuth flow using user-provided `client_secrets.json`; stores refresh token locally. |
| `gsc auth status` | Show current auth identity and token validity. |
| `gsc sites list` | List all GSC properties (replaces `list_properties`). |
| `gsc sites get <url>` | Property details + verification info (replaces `get_site_details`). |
| `gsc sites add <url>` | Add property to account. |
| `gsc sites remove <url>` | Remove property. Requires `--yes` in non-TTY. |
| `gsc analytics query <url>` | Search Analytics with dimensions, filters, date range, compare (replaces `get_search_analytics`). |
| `gsc analytics overview <url>` | Summary performance (replaces `get_performance_overview`). |
| `gsc urls inspect <url> [urls...]` | URL Inspection API, fan-out with concurrency cap (replaces `inspect_url_enhanced` + `check_indexing_issues`). |
| `gsc sitemaps list <url>` | List sitemaps. |
| `gsc sitemaps submit <url> <sitemap-url>` | Submit sitemap. |
| `gsc sitemaps get <url> <sitemap-url>` | Sitemap status + warnings/errors. |
| `gsc quota` | Show today's API usage against known limits. |
| `gsc config` | Read/write `~/.config/gsc/config.toml`. |

### Out of scope (v1)

- Web UI, TUI, or daemon mode.
- Sharing/publishing results to third parties.
- Non-GSC data sources (GA4, Bing, Ahrefs, etc.).
- Automated scheduled runs — users wire cron themselves.
- Multi-user/multi-tenant auth (single local user; multi-profile deferred to v2).

## 4. Users & JTBD

- **Primary: the LLM agent** (Claude or similar) invoked by the operator. Needs: predictable JSON, structured errors, discoverable `--help`, idempotent reads, explicit confirmation for writes.
- **Secondary: the SEO operator (flo)** who types commands directly or pipes them through shell. Needs: concise output on TTY, fast startup, obvious cache status.

## 5. Functional Requirements

### FR-1 Output format
- `--output json` (default). `--output csv` (allowed for `analytics query`, `analytics overview`, `sites list`, `urls inspect`, `sitemaps list`). `--output table` (pretty, TTY-only; auto-selected when stdout is a TTY).
- When stdout is not a TTY and no `--output` is passed, default is `json`.
- Every JSON response includes: `data`, `meta.cached` (bool), `meta.cached_at` (RFC3339 or null), `meta.ttl_remaining_sec` (int or null), `meta.api_calls` (int).

### FR-2 Caching
- On-disk flat files under `./.gsc/cache/` (working directory) by default; overridable via config `cache.dir`.
- Cache key: sha256 of (command path + normalized args + property + auth identity).
- Default TTLs: `sites list` 1h, `sites get` 1h, `analytics query` 15m, `analytics overview` 15m, `sitemaps list` 10m, `sitemaps get` 10m, `urls inspect` 24h. Writes (`sites add/remove`, `sitemaps submit`) never cached and invalidate related keys.
- Flags: `--no-cache` (bypass read & write), `--refresh` (bypass read, write fresh result), `--cache-ttl <duration>` (override).

### FR-3 Authentication
- User provides `client_secrets.json` path via `--credentials <path>`, `GSC_CREDENTIALS` env var, or config `auth.credentials_path`.
- `gsc auth login` runs standard OAuth 2.0 loopback flow (local redirect on `127.0.0.1:<random-port>`), prints auth URL, captures code, exchanges for tokens.
- Refresh token + access token stored at `~/.config/gsc/token.json` with file mode `0600`.
- Auto-refresh on expiry. On refresh failure, return structured error with code `auth_expired` and hint to re-run `gsc auth login`.
- Single-account v1. Multi-profile (`--profile`) deferred.

### FR-4 Date range UX (analytics commands)
Support all three forms, mutually exclusive:
- `--range last-7d | last-28d | last-3m | last-6m | last-12m | last-16m`
- `--start YYYY-MM-DD --end YYYY-MM-DD`
- `--compare previous-period | previous-year` (pairs with either of the above; adds a `comparison` block to the response)

Help text for each analytics command MUST include ≥3 worked examples (e.g., "top 50 queries for mobile last 28d", "compare click-through rate vs previous period").

### FR-5 Search Analytics query surface
- `--dimensions query,page,country,device,searchAppearance,date` (comma list; default `query`).
- `--filter <dimension><op><value>` repeatable. Ops: `=`, `!=`, `~` (contains), `!~`. Example: `--filter country=usa --filter device=MOBILE`.
- `--search-type web | image | video | news | discover | googleNews` (default `web`).
- `--limit N` (default 20, max 25000 via pagination; API hard-cap 25000/request).
- `--order-by clicks | impressions | ctr | position` (default `clicks`). `--desc` (default) / `--asc`.

### FR-6 URL Inspection fan-out
- Accepts N URLs as positional args or newline-delimited on stdin when `-` is passed.
- Parallelism default 5, override via `--concurrency N` (max 10).
- Progress bar to stderr when TTY; silent when piped.
- On quota exhaustion, stop dispatch, return partial results with `meta.partial=true` and error block listing un-inspected URLs.

### FR-7 Quota tracking & warnings
- Track daily counters at `./.gsc/quota.json`, reset at midnight America/Los_Angeles (GSC quota window).
- Counters per-API: `url_inspection` (limit 2000/day), `search_analytics` (1200 QPM — rate, not daily; track rolling 60s window), `other`.
- Warning thresholds for URL Inspection: first warn at 1000 used, then 1500, then every +100 (1600, 1700, …). Warnings go to stderr as JSON when `--log-format json`, else plain text.
- Hard-stop at 100% with structured error code `quota_exceeded`.
- `gsc quota` prints current usage.

### FR-8 Errors & exit codes
All errors emitted as JSON to stderr (even when `--output csv`). Schema:
```json
{"error": {"code": "quota_exceeded", "message": "...", "hint": "...", "retriable": true, "retry_after_sec": 3600}}
```
Exit codes:
- `0` success
- `1` generic error
- `2` auth error (`auth_missing`, `auth_expired`, `auth_denied`)
- `3` quota error (`quota_exceeded`, `rate_limited`)
- `4` not found (`property_not_found`, `sitemap_not_found`, `url_not_indexed`)
- `5` validation error (`invalid_args`, `invalid_date_range`)
- `6` network error (`network_unreachable`, `api_5xx`)

### FR-9 Write-operation safety
- `sites remove`: requires `--yes`. Without it: if TTY, prompt; if non-TTY, exit code 5 with `hint` to pass `--yes`.
- `sitemaps submit`: no confirmation required.
- `sites add`: no confirmation required (non-destructive).
- Writes are logged to `./.gsc/audit.log` (append-only, one JSON line per mutation).

### FR-10 Config file
`~/.config/gsc/config.toml`, loaded on every invocation. Fields:
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
format = "text"  # or "json"
```
CLI flags override config; config overrides built-in defaults.
`gsc config get <key>` / `gsc config set <key> <value>` / `gsc config path`.

### FR-11 Help & discoverability
- Every command and subcommand has `--help` with: 1-line description, usage, flags, ≥2 worked examples, related commands.
- `gsc --help` prints the full command tree and points at `gsc <cmd> --help`.
- All flag descriptions must be human- and LLM-readable (no cryptic abbreviations).

### FR-12 Logging
- `--verbose` / `-v`: API request traces to stderr.
- `--quiet` / `-q`: suppress warnings (errors still shown).
- `--log-format json|text` (default text). JSON format emits structured log lines to stderr.

## 6. Non-Functional Requirements

- **Startup latency**: cold-start to first output < 50ms for cache-hit reads, < 150ms for no-op commands (`--help`, `config get`).
- **Binary size**: < 25MB stripped.
- **Platforms**: darwin-arm64, darwin-amd64, linux-amd64, linux-arm64, windows-amd64.
- **Go version**: 1.22+ (for latest stdlib + `log/slog`).
- **Concurrency safety**: cache writes via atomic rename; quota writes via file lock (`flock`).
- **No telemetry.** Zero outbound traffic beyond Google APIs and the OAuth loopback.

## 7. Technical Design (high-level)

- **Language**: Go.
- **Key deps**: `google.golang.org/api/searchconsole/v1`, `golang.org/x/oauth2/google`, `github.com/spf13/cobra`, `github.com/spf13/viper` (or equivalent for TOML), `github.com/BurntSushi/toml`.
- **Layout** (proposed):
  ```
  cmd/gsc/main.go
  internal/auth/       # oauth flow + token storage
  internal/cache/      # flat-file cache w/ TTL
  internal/quota/      # daily counters + warnings
  internal/client/     # thin GSC API wrapper
  internal/output/     # json/csv/table renderers
  internal/cmd/        # cobra command tree
  internal/config/     # TOML config
  ```
- **Cache file format**: JSON blob `{ "cached_at": "...", "ttl": "15m", "payload": <raw API response> }`.

## 8. Dependencies & Integrations

- Google Search Console API v1 (`searchconsole.googleapis.com`).
- Google OAuth 2.0 (installed-app / loopback flow).
- User-supplied Google Cloud project with GSC API enabled and OAuth consent screen configured (documented in README).

## 9. Rollout

Solo build; single cut. No staged rollout.

1. **Pre-release**: tag `v0.1.0-alpha`, publish GitHub Release with prebuilt binaries for 5 platforms via GoReleaser.
2. **v1.0**: all 9 INPUT commands working + auth/config/quota. Announce in README.
3. **Rollback**: not applicable (local CLI); users pin to prior release.

## 10. Success Metrics Revisited

| Metric | Target | How measured |
|---|---|---|
| Coverage of GSC web UI capabilities | 100% of the 9 INPUT features | Manual checklist at v1 cut |
| LLM zero-shot usability | Claude completes 8/10 sample SEO tasks with only `--help` | Eval suite of 10 prompts run manually |
| p95 cached read latency | < 50ms | Timed benchmarks |
| p95 uncached read latency | < 1500ms | Timed benchmarks |
| Auth setup time (new user) | < 5 minutes from `go install` to first `gsc sites list` | Dogfooding |

## 11. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| GSC API quota changes break quota warnings | low | medium | Quota thresholds in config, not hard-coded |
| OAuth consent-screen verification for public client | medium | medium | Document user-provided `client_secrets.json` approach; don't ship a shared client |
| `url-inspection` daily quota (2000) too tight for bulk audits | high | medium | Warn aggressively; document limit in help; cache results 24h |
| Cache directory `.gsc/` in working directory gets committed accidentally | medium | low | Document in README to add `.gsc/` to `.gitignore`; print one-time hint on first cache write |
| API response schema drift | low | low | Use official Google Go client — handled upstream |
| LLM hallucinates `sites remove` invocation | medium | high | `--yes` required in non-TTY; audit log |

## 12. Assumptions & Open Questions

**Assumptions** (confirm before v1 cut):
- Cache directory in CWD (`./.gsc/`) is preferred over XDG path (`~/.cache/gsc/`). `ASSUMPTION`: user confirmed "local folder of the cli" — interpreting as CWD, not install dir.
- Token storage at `~/.config/gsc/token.json` is acceptable (not keychain). `ASSUMPTION`: not explicitly discussed.
- Multi-profile is deferred to v2. `ASSUMPTION`: single-user confirmed in discovery.
- Search Analytics API rate limit (1200 QPM) tracking is best-effort rolling window, not persisted across process restarts. `ASSUMPTION`: acceptable for single-user CLI.

**Open questions**:
1. Should `gsc analytics query` support a `--group-by date` shortcut that auto-adds `date` to dimensions and formats time-series CSV?
2. Audit log retention — unbounded append, or rotate at N MB?
3. On first run with no config and no credentials, should `gsc` interactively walk the user through `auth login`, or hard-fail with a hint?
4. Keychain-backed token storage (macOS Keychain / linux secret-service) — v1 or defer?

## 13. Non-Goals (explicit)

- Not a replacement for GSC web UI for report *authoring*/sharing.
- Not a general Google API CLI (GSC only).
- No visualization rendering inside the CLI — charts are the LLM's job using exported CSV.
- No built-in diff/comparison beyond what `--compare` provides.

## 14. Appendix

**Revision log**

| Date | Version | Author | Change |
|---|---|---|---|
| 2026-04-15 | 0.1 | flo | Initial draft from INPUT.md + 3-batch discovery |

**References**
- About the API: https://developers.google.com/webmaster-tools/about
- Usage Limits: https://developers.google.com/webmaster-tools/limits
- Auth: https://developers.google.com/webmaster-tools/v1/how-tos/authorizing
- Batch Requests: https://developers.google.com/webmaster-tools/v1/how-tos/batch
- Search Analytics: https://developers.google.com/webmaster-tools/v1/how-tos/search_analytics
- Go client: https://pkg.go.dev/google.golang.org/api/searchconsole/v1
