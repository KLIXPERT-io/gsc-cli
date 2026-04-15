---
title: Search Console API Coverage Gaps
slug: api-coverage-gaps
status: draft
version: 0.1
kind: feature
owner: TBD
created: 2026-04-15
updated: 2026-04-15
---

## 1. Summary

Close the remaining gaps against `searchconsole/v1`: add `gsc sitemaps remove` (calls `Sitemaps.Delete`) and surface four analytics knobs the Go client exposes but the CLI doesn't (`--data-state`, `--aggregation`, `--all` auto-pagination, and repeatable `--filter-group` for OR-of-AND filter groups). Deprecated `urlTestingTools.mobileFriendlyTest` and non-searchconsole APIs (Indexing, CrUX) are out of scope. Success = every non-deprecated `searchconsole/v1` method or option is reachable from the CLI, with destructive sitemap deletion gated the same way as `sites remove`.

## 2. User story / trigger

- **Operator** managing many properties wants to remove stale sitemaps via `gsc sitemaps remove <property> <sitemap-url> --yes` without touching the GSC UI.
- **Analyst / agent** running `gsc analytics query` wants (a) fresh numbers including last ~2 days via `--data-state all`, (b) control over URL-prefix vs domain rollup via `--aggregation`, (c) to pull result sets larger than 25 000 rows with `--all`, and (d) to express OR-of-AND filter logic the API supports but today's single-group builder doesn't.

## 3. Functional requirements

### FR-001 [MUST] `gsc sitemaps remove` command

**Given** a property and a sitemap URL previously submitted to that property,
**when** the user runs `gsc sitemaps remove <property> <sitemap-url> --yes`,
**then** the CLI calls `Sitemaps.Delete(siteURL, feedpath)`, bumps the `other` quota bucket, writes an audit event `sitemaps.remove` with `OK=true`, clears any cached `sitemaps.list`/`sitemaps.get` entries for that property, and emits `{ok: true, property, sitemap}`.

**And** without `--yes`:
- on a TTY, the CLI prompts `Remove sitemap <url> from <property>? Type the sitemap URL to confirm:` and only proceeds on an exact match;
- in non-TTY contexts, the CLI exits with `CodeInvalidArgs` (exit 5) and the hint `Pass --yes to confirm in non-TTY contexts.` — matching `sites remove` behavior at `internal/cmd/sites.go:135`.

**And** on API error, the CLI logs an audit event with `OK=false` and the translated error, and returns a non-zero exit code via `client.Translate`.

### FR-002 [MUST] `--data-state` on `analytics query` and `analytics overview`

**Given** the user passes `--data-state all|final` (default `final`, preserving current behavior),
**when** the CLI builds the `SearchAnalyticsQueryRequest`,
**then** it sets `DataState` to the chosen value and includes it in the cache key so `all` and `final` do not collide.

**And** an unrecognized value returns `CodeInvalidArgs` with a message listing the accepted tokens.

### FR-003 [MUST] `--aggregation` on `analytics query` and `analytics overview`

**Given** the user passes `--aggregation auto|byPage|byProperty` (default `auto`),
**when** the CLI builds the request,
**then** it sets `AggregationType` accordingly and includes it in the cache key. Response field `responseAggregationType` is already surfaced by `analytics query`; `overview` gains it in JSON output under the same key.

### FR-004 [MUST] `--all` auto-pagination on `analytics query`

**Given** the user passes `--all`,
**when** the CLI runs the query,
**then** it repeatedly calls `Searchanalytics.Query` with `StartRow = 0, 25000, 50000, …` until a response returns fewer than `RowLimit` rows or zero rows, concatenates rows into a single result, and bumps the SA quota once per request. Each request honors the user-provided `--limit` (default 25000; values above 25000 clamp to 25000 when `--all` is set).

**And** `--all` is mutually exclusive with `--compare`; passing both returns `CodeInvalidArgs` (comparison over auto-paginated sets is out of scope for this feature).

**And** the cache entry for an `--all` invocation stores the merged result under a key that includes `all=true`; partial page results are not cached individually.

**And** if any page request fails, the command returns the translated error with no partial output on stdout; the audit/quota side effects from already-completed pages remain (no rollback).

### FR-005 [SHOULD] Repeatable `--filter-group` for OR-of-AND filters

**Given** the user passes one or more `--filter-group "<dim><op><value>[,<dim><op><value>…]"` flags,
**when** the CLI builds the request,
**then** each flag value parses into one `ApiDimensionFilterGroup{GroupType: "and", Filters: […]}` and all groups are appended to `DimensionFilterGroups` (the API ORs groups). Per-filter syntax inside a group reuses `parseFilter` (`=`, `!=`, `~`, `!~`) at `internal/cmd/analytics.go:337`.

**And** `--filter-group` and the existing `--filter` flag are mutually exclusive; passing both returns `CodeInvalidArgs` with a hint pointing to `--filter-group` as the superset.

**And** an empty group or a group with an unparseable filter returns `CodeInvalidArgs` identifying the offending group index.

### FR-006 [MUST] README & help text updated

**Given** FR-001..FR-005 are implemented,
**when** the user runs `gsc sitemaps remove --help`, `gsc analytics query --help`, or reads `README.md`,
**then** each new flag/command is documented with at least one example, and the `--all` example shows CSV output streaming to stdout.

## 4. Non-functional notes

- **Auditing:** `sitemaps.remove` writes the same `audit.Event` shape as existing destructive commands (`internal/audit`).
- **Cache invalidation:** on successful `sitemaps.remove`, clear `sitemaps.list` and `sitemaps.get` cache keys for the affected property; global `s.Cache.Clear()` is acceptable for v1 (matches `sites remove`).
- **Quota:** each API call bumps the existing buckets (`SA` for analytics, `other` for sitemaps). `--all` therefore increments `SA` N times.
- **Performance:** `--all` against a 100k-row property issues 4 sequential requests; no parallelism. If this proves slow in practice, a follow-up can parallelize — out of scope here.

## 5. Out of scope / non-goals

- `urlTestingTools.mobileFriendlyTest.run` shim (Google retired Dec 2023).
- Indexing API (`indexing.googleapis.com`) — separate service, separate feature.
- PageSpeed Insights / CrUX / Core Web Vitals — separate API.
- `--compare` combined with `--all` (explicit non-goal per FR-004).
- Parallel pagination for `--all`.
- Per-sitemap cache invalidation (coarse clear is acceptable v1).

## 6. Technical notes

**Affected paths:**
- `internal/cmd/sitemaps.go` — add `newSitemapsRemoveCmd`, register in `newSitemapsCmd`.
- `internal/cmd/sites.go:135` — reference implementation for the `--yes` gate; extract a shared helper (`confirmDestructive(cmd, s, subject string) error`) if clean, otherwise copy-adapt.
- `internal/cmd/analytics.go` — add `dataState`, `aggregation`, `all`, `filterGroups` flags to `newAnalyticsQueryCmd`; extend `buildAnalyticsRequest` signature or introduce a request-builder struct; add `dataState`/`aggregation` to `newAnalyticsOverviewCmd`.
- `internal/cmd/analytics.go:313` — `buildAnalyticsRequest` gains `dataState`, `aggregation`, `filterGroups [][]string` parameters; update both call sites.
- `internal/cmd/analytics.go:337` — reuse `parseFilter` unchanged.
- `README.md` — new examples for `sitemaps remove`, `--data-state`, `--aggregation`, `--all`, `--filter-group`.

**Dependencies:** no new Go modules; `google.golang.org/api/searchconsole/v1` already exposes `DataState`, `AggregationType`, `DimensionFilterGroups`, and `Sitemaps.Delete`.

**Data model deltas:** none — all new knobs are request-time flags with cache-key participation.

**CLI surface additions (summary):**
```
gsc sitemaps remove <property> <sitemap-url> [--yes]
gsc analytics query  <url>  [--data-state final|all] [--aggregation auto|byPage|byProperty] [--all] [--filter-group "<f>,<f>…" …]
gsc analytics overview <url> [--data-state final|all] [--aggregation auto|byPage|byProperty]
```

## 7. Open questions & assumptions

- **ASSUMPTION:** `--filter` and `--filter-group` being mutually exclusive is acceptable. Alternative: treat a bare `--filter` as sugar for a single `--filter-group`. Pick during review.
- **ASSUMPTION:** `--all` clamping `--limit` to 25000 (API max page size) is acceptable; no per-page override needed.
- **OPEN QUESTION:** Should `sitemaps remove` accept `--force` as an alias to `--yes` for symmetry with other CLIs, or stick to `--yes` only? Current repo uses `--yes` globally via `s.Yes`, so sticking is the default.
- **OPEN QUESTION:** On `--all` errors mid-pagination, should we emit partial rows to stderr as JSON for debugging, or stay silent? Default: silent.
