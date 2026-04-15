---
name: gsc-cli
description: Query Google Search Console from the command line with the `gsc` CLI — sites, search analytics, URL inspection, sitemaps, and quota. Use when the user asks about GSC data, search performance, impressions/clicks/CTR/position, indexing status, or sitemap management.
---

# gsc - Google Search Console CLI

A Go CLI that wraps the Google Search Console API v1. Designed to be LLM-friendly: JSON-by-default output with a stable envelope, machine-readable error codes, deterministic exit codes, and a local cache so repeated queries are cheap.

## Install

Installation instructions live in [INSTALL.md](https://github.com/KLIXPERT-io/gsc-cli/blob/main/INSTALL.md) — follow that document to install `gsc` before using any commands below.

## First-run setup

The user must do this once — you cannot do it for them:

1. In Google Cloud, enable the **Search Console API** and create an **OAuth 2.0 Client ID** of type **Desktop app**. Download `client_secrets.json`.
2. Run:
   ```bash
   gsc config set auth.credentials_path ~/secrets/gsc-client.json
   gsc auth login
   ```
3. Tokens are stored in the OS keychain (file fallback: `~/.config/gsc/token.json`, 0600).

Verify auth with `gsc sites list`.

## Output envelope (always JSON unless `--output` says otherwise)

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

Errors go to **stderr** as JSON (even in CSV/table modes):

```json
{"error":{"code":"quota_exceeded","message":"...","hint":"...","retriable":true,"retry_after_sec":3600}}
```

Exit codes:

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | generic error |
| 2 | auth |
| 3 | quota / rate |
| 4 | not found |
| 5 | validation |
| 6 | network |

When scripting: check exit code first, then parse `stderr` for `error.code`. Do **not** parse the human message.

## Commands — at a glance

| Group | Commands |
| --- | --- |
| `auth` | `login`, `logout`, `status` |
| `sites` | `list`, `get <prop>`, `add <prop>`, `remove <prop>` |
| `analytics` | `query <prop>`, `overview <prop>` |
| `urls` | `inspect <prop> <url\|->` |
| `sitemaps` | `list <prop>`, `get <prop> <url>`, `submit <prop> <url>`, `remove <prop> <url>` |
| `config` | `get`, `set`, `path`, `list` |
| `quota` | show current daily/rolling counters |

Property identifiers use GSC's conventions: `sc-domain:example.com` (domain property) or `https://www.example.com/` (URL-prefix property — trailing slash required).

## Flags shared across commands

- `--output json|csv|table` — default `json` (or `table` on a TTY)
- `--no-cache` — bypass cache reads and writes
- `--refresh` — bypass cache read, but write fresh result
- `--cache-ttl <duration>` — override TTL (e.g. `1h`, `15m`)
- `--yes` — required for destructive ops when stdin is not a TTY
- `-v, --verbose` / `-q, --quiet`
- `--log-format text|json`

## Examples

List verified properties:

```bash
gsc sites list
```

Top 50 queries, mobile only, last 28 days (the default range):

```bash
gsc analytics query sc-domain:example.com \
  --dimensions query --filter device=MOBILE --limit 50
```

Daily clicks time-series as CSV for plotting:

```bash
gsc analytics query sc-domain:example.com \
  --group-by date --range last-3m --output csv
```

Compare to previous period:

```bash
gsc analytics query sc-domain:example.com \
  --dimensions query --range last-28d --compare previous-period
```

Include fresh (non-finalized) last-2-days data:

```bash
gsc analytics query sc-domain:example.com --dimensions query --data-state all
```

Auto-paginate past the 25k-row API cap and stream CSV:

```bash
gsc analytics query sc-domain:example.com \
  --dimensions query,page --all --output csv > queries.csv
```

Filter groups — `(query~brand AND device=MOBILE) OR (country=usa)`:

```bash
gsc analytics query sc-domain:example.com \
  --filter-group "query~brand,device=MOBILE" \
  --filter-group "country=usa"
```

High-level overview for a property, fresh data, domain-rollup aggregation:

```bash
gsc analytics overview sc-domain:example.com --data-state all --aggregation byProperty
```

Bulk URL inspection (reads URLs from stdin, one per line):

```bash
cat urls.txt | gsc urls inspect sc-domain:example.com -
```

Sitemaps:

```bash
gsc sitemaps list sc-domain:example.com
gsc sitemaps submit sc-domain:example.com https://www.example.com/sitemap.xml
gsc sitemaps remove sc-domain:example.com https://www.example.com/sitemap.xml --yes
```

Current quota usage:

```bash
gsc quota
```

## Date ranges

Accepted `--range` values: `last-7d`, `last-28d` (default), `last-3m`, `last-6m`, `last-12m`, `last-16m`, or an explicit `YYYY-MM-DD:YYYY-MM-DD`. GSC data is typically finalized at 2-3 days lag; use `--data-state all` to include fresh/partial rows.

## Dimensions and filters

- `--dimensions` (comma-separated): `query`, `page`, `country`, `device`, `date`, `searchAppearance`.
- `--filter key OP value` — operators: `=`, `!=`, `~` (contains), `!~` (notContains), `^=` (startsWith), `$=` (endsWith).
- `--filter-group "a=b,c~d"` — AND inside a group, OR across repeated `--filter-group` flags.

## Tips for LLMs driving this CLI

- Prefer `--output json` (the default) and parse the `data` field. `meta.cached` tells you if a call hit the cache; `meta.api_calls` is the real network cost.
- For large result sets, use `--all` with `--output csv` and pipe to a file instead of loading everything into memory.
- Before a destructive command (`sites remove`, `sitemaps remove`), confirm with the user, then pass `--yes`.
- Errors are stable: branch on `error.code`, not on `error.message`.
- If `gsc auth status` shows the user is not logged in, stop and ask them to run `gsc auth login` — you cannot complete the OAuth flow on their behalf.
