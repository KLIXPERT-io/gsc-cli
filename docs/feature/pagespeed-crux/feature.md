---
title: PageSpeed Insights & CrUX Commands
slug: pagespeed-crux
status: shipped
version: 0.1
kind: feature
owner: TBD
created: 2026-04-16
updated: 2026-04-16
---

## 1. Summary

The CLI surfaces Search Console data but nothing about how pages actually perform for real users, forcing operators to context-switch to web UIs for Core Web Vitals. Add two new command groups — `gsc pagespeed` (Lighthouse + field data via PageSpeed Insights API) and `gsc crux` (current + historical real-user metrics via the CrUX APIs) — plus a convenience `gsc cwv` that picks the right source and renders a pass/fail summary. Success = for any URL the user already manages in GSC, `gsc cwv <url>` returns LCP/INP/CLS/TTFB ratings in one call without leaving the terminal.

## 2. User story / trigger

**Who:** SEO/perf operator who already uses `gsc analytics` to triage traffic drops.
**When:** a URL loses impressions/position and they suspect a Core Web Vitals regression.
**Job-to-be-done:** When I see an analytics anomaly on a URL, I want one command that returns current + historical CWV and a fresh Lighthouse score, so I can decide whether the issue is perf-related without opening PSI/CrUX dashboards.

Happy path:
1. User runs `gsc cwv https://example.com/pricing`.
2. CLI calls CrUX `queryRecord` for the URL; if CrUX has no row for that URL, falls back to origin-level data.
3. Output prints LCP/INP/CLS/TTFB with ratings and the `p75` value.
4. User drills in with `gsc pagespeed run <url> --strategy mobile` for a Lighthouse audit.
5. For trend, user runs `gsc crux history <url>` and gets 25 weekly points per metric.

Error paths:
- CrUX returns 404 (URL not in dataset) → CLI exits with `CodeNotFound`, hint to try `--origin`.
- PSI returns 5xx / times out → translated error, non-zero exit, no partial JSON on stdout.

## 3. Functional requirements

### FR-001 [MUST] `gsc pagespeed run <url>` — PageSpeed Insights

**Given** a valid https URL,
**when** the user runs `gsc pagespeed run <url> [--strategy mobile|desktop] [--category performance,accessibility,best-practices,seo,pwa] [--locale <bcp47>]` (defaults: `--strategy mobile`, all categories),
**then** the CLI calls `pagespeedonline.googleapis.com/v5/runPagespeed` with the user's OAuth token, bumps the `psi` quota bucket once, caches the response under a key that includes url+strategy+categories+locale+date (TTL matches existing `analytics` cache TTL), and prints either a table (default) or raw JSON (`--json`).

**And** the default table shows: Lighthouse category scores (0–100), CWV field metrics (LCP/INP/CLS/TTFB with p75 + rating), and the `lighthouseResult.fetchTime`.

**And** unknown `--strategy` / `--category` values return `CodeInvalidArgs` (exit 5) with accepted tokens in the message.

**And** if PSI returns `429` the CLI translates to `CodeRateLimited` and suggests `gsc quota` to inspect `psi` bucket usage.

### FR-002 [MUST] `gsc crux query <target>` — current CrUX record

**Given** a positional `<target>` that is either a URL or an origin,
**when** the user runs `gsc crux query <target> [--form-factor phone|desktop|tablet|all] [--metric …]` (default: all form factors merged, all CWV metrics),
**then** the CLI auto-detects origin vs URL (bare `scheme://host[:port]` with no path → origin query; anything else → url query; an explicit `--origin` flag forces origin mode), calls `chromeuxreport.googleapis.com/v1/records:queryRecord`, bumps the `crux` quota bucket, caches under target+form-factor+metrics+date, and prints p75 per metric + histogram buckets (table) or raw JSON (`--json`).

**And** if the API returns `404 CHROME_UX_REPORT_NOT_FOUND`, the CLI exits with `CodeNotFound` and the hint `No CrUX data for this URL. Try the origin: gsc crux query <origin> --origin`.

### FR-003 [MUST] `gsc crux history <target>` — CrUX History API

**Given** a target as in FR-002,
**when** the user runs `gsc crux history <target> [--form-factor …] [--metric …] [--weeks N]` (default: all metrics, last 25 weeks which is the API maximum),
**then** the CLI calls `queryHistoryRecord`, bumps `crux` once per call, caches under target+form-factor+metrics+week-range, and prints either a wide table (one row per week, one column per metric p75) or raw JSON.

**And** `--weeks` values >25 clamp to 25 with a stderr warning; `<1` returns `CodeInvalidArgs`.

### FR-004 [MUST] `gsc cwv <target>` — unified Core Web Vitals summary

**Given** a target,
**when** the user runs `gsc cwv <target> [--form-factor phone|desktop] [--origin-fallback]` (default form-factor `phone`, fallback **on**),
**then** the CLI calls CrUX `queryRecord` for the target; on `404` and `--origin-fallback` (default), derives the origin from the URL and retries with origin mode; output is a compact table of LCP / INP / CLS / TTFB with p75 value + rating (`good` / `needs-improvement` / `poor`) using the published CWV thresholds, plus a source indicator (`source=url` or `source=origin`).

**And** the JSON form (`--json`) includes `{target, source, formFactor, metrics: {lcp, inp, cls, ttfb: {p75, rating}}}`.

**And** exit code is non-zero if any metric is `poor` and `--fail-on poor` is passed (default: exit 0 regardless of ratings). `--fail-on needs-improvement` also treats NI as failure.

### FR-005 [MUST] Quota buckets `psi` and `crux`

**Given** FR-001..FR-004,
**when** any of those commands complete an API call (success or failure after the request is issued),
**then** `internal/quota.Store` exposes two new counters `psi` and `crux` alongside `other`, and `gsc quota` renders them with the per-day thresholds (PSI 25 000/day, CrUX 150 QPS best-effort) as warning-at-80% like the existing buckets.

**And** the `Bump` category vocabulary is extended to accept `"psi"` and `"crux"`; unknown values remain errors.

### FR-006 [MUST] OAuth scope + API enablement preflight

**Given** the user's stored OAuth credentials from `gsc auth`,
**when** a PSI or CrUX command is invoked and the Google API responds with `403 SERVICE_DISABLED` or `PERMISSION_DENIED`,
**then** the CLI translates to `CodeAuthRequired` with a hint `Enable the PageSpeed Insights API / Chrome UX Report API for the GCP project backing your credentials: https://console.cloud.google.com/apis/library/<api-id>`.

**And** no new OAuth scope is requested beyond what `gsc auth` already grants (both APIs accept the default Cloud Platform scope). If a missing-scope error is detected, the CLI instructs the user to re-run `gsc auth login`.

### FR-007 [SHOULD] README & help updates

**Given** FR-001..FR-006 are implemented,
**when** the user reads `README.md` or `gsc <cmd> --help`,
**then** each new command has at least one worked example, and the README gains a "Performance & Core Web Vitals" section.

## 4. Non-functional notes

- **Auth:** reuses existing OAuth flow (`internal/auth`); no new credential storage.
- **Caching:** reuses `internal/cache`; TTL config key `cache.ttl.psi` / `cache.ttl.crux` default 24h / 24h respectively (CrUX data refreshes monthly; PSI is per-run but rarely needs sub-daily refresh).
- **Quota visibility:** `gsc quota` output adds two rows; format stays backwards-compatible (additive).
- **Performance:** one HTTP call per invocation (except `cwv` with fallback = up to 2). No concurrency introduced.
- **Output stability:** JSON shape documented in help text and treated as a stability contract; breaking changes require version bump.

## 5. Out of scope

- Batch mode (`gsc pagespeed run <url1> <url2> …`) — follow-up.
- CSV output — follow-up (JSON + table cover current needs per discovery).
- `--all-strategies` combined mobile+desktop run — explicit non-goal for v1.
- CrUX BigQuery public dataset access — different API, different auth.
- CrUX origin-level aggregated leaderboards — out of scope.
- API-key-based auth (only OAuth supported for v1).
- Automated regression alerts / thresholds stored over time — the CLI reports, it does not remember.
- Lighthouse audit tree extraction beyond category scores + CWV — JSON output exposes it, but no table rendering for individual audits.

## 6. Technical notes

**New paths:**
- `internal/cmd/pagespeed.go` — `newPagespeedCmd`, `newPagespeedRunCmd`.
- `internal/cmd/crux.go` — `newCruxCmd`, `newCruxQueryCmd`, `newCruxHistoryCmd`.
- `internal/cmd/cwv.go` — `newCwvCmd` (calls into crux package).
- `internal/client/psi.go` — thin wrapper over `pagespeedonline/v5` (use `google.golang.org/api/pagespeedonline/v5`).
- `internal/client/crux.go` — CrUX + History clients. No official Go SDK; use `net/http` + JSON against `https://chromeuxreport.googleapis.com/v1/records:queryRecord` and `:queryHistoryRecord`. Reuse the OAuth-authenticated `*http.Client` from `internal/auth`.

**Modified paths:**
- `internal/cmd/root.go` — register `pagespeed`, `crux`, `cwv`.
- `internal/quota/quota.go:25` — add `PSI int`, `CRUX int` fields; extend `Bump` switch.
- `internal/cmd/quota.go:9` — render new rows.
- `internal/cache/*` — no code change; new cache key prefixes `psi:` and `crux:`.
- `README.md` — new section + examples.

**Dependencies:**
- Add `google.golang.org/api/pagespeedonline/v5` to `go.mod`.
- No new dep for CrUX (hand-rolled client — small surface, avoids pulling chromeuxreport discovery).

**Data model deltas:** none on-disk beyond the additive quota fields (backwards-compatible JSON read).

**CLI surface summary:**
```
gsc pagespeed run <url> [--strategy mobile|desktop] [--category …] [--locale …] [--json]
gsc crux query <target> [--form-factor phone|desktop|tablet|all] [--metric …] [--origin] [--json]
gsc crux history <target> [--form-factor …] [--metric …] [--weeks N] [--json]
gsc cwv <target> [--form-factor phone|desktop] [--origin-fallback] [--fail-on ni|poor] [--json]
```

**Rating thresholds (CWV, p75):**
- LCP: good ≤2.5s, NI ≤4.0s, else poor
- INP: good ≤200ms, NI ≤500ms, else poor
- CLS: good ≤0.1, NI ≤0.25, else poor
- TTFB: good ≤800ms, NI ≤1800ms, else poor

## 7. Open questions & assumptions

- **ASSUMPTION:** The existing OAuth scope covers PSI + CrUX. Both accept the default Cloud Platform scope, but if the user authenticated with the narrower `webmasters.readonly` scope alone, they'll need to re-auth. FR-006 catches this at runtime; no preflight at install time.
- **ASSUMPTION:** CrUX History max window of 25 weeks is acceptable; users wanting longer history should use BigQuery (out of scope).
- **ASSUMPTION:** `cwv` defaulting to `--form-factor phone` matches user intent (mobile-first). Flip to `desktop` only on user request.
- **OPEN QUESTION:** Should `cwv` exit non-zero by default when any metric is `poor` (CI-friendly) vs. current design of opt-in via `--fail-on`? Default picked: opt-in (less surprising for interactive use).
- **OPEN QUESTION:** Should `pagespeed run` support a `--categories none` equivalent to skip Lighthouse and return only CrUX field data (cheaper)? Not included in v1; PSI always runs Lighthouse.
