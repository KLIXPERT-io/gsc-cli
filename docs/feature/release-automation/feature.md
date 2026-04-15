---
title: Release Automation & Cross-Platform Install
slug: release-automation
status: draft
version: 0.1
kind: feature
owner: TBD
created: 2026-04-15
updated: 2026-04-15
---

## 1. Summary

Stand up a tag-driven GitHub Actions release pipeline that produces `gsc` binaries for Linux, macOS, and Windows on both amd64 and arm64, publishes them to a GitHub Release with checksums, and ships a one-liner `curl | sh` installer (`install.sh`), a PowerShell installer (`install.ps1`), and `INSTALL.md` covering every supported platform. GoReleaser drives the build; pushing a `v*` tag is the only trigger. Success = `curl -fsSL https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.sh | sh` installs the latest release on macOS/Linux and `gsc --version` prints the released tag.

## 2. User story / trigger

- **Operator** on macOS/Linux wants to install `gsc` in one command without cloning the repo or having a Go toolchain.
- **Maintainer** wants to cut a release by pushing `git tag vX.Y.Z && git push --tags` — no manual binary uploads.
- **Windows user** wants a first-class install path (PowerShell one-liner) equivalent to the Unix flow.

## 3. Functional requirements

### FR-001 [MUST] GoReleaser config for 5 targets

**Given** a `.goreleaser.yml` at the repo root,
**when** `goreleaser release --snapshot --clean` runs locally (already invoked by `make release-snapshot`),
**then** it produces five archives under `dist/`:

- `gsc_<version>_linux_amd64.tar.gz`
- `gsc_<version>_linux_arm64.tar.gz`
- `gsc_<version>_darwin_amd64.tar.gz`
- `gsc_<version>_darwin_arm64.tar.gz`
- `gsc_<version>_windows_amd64.zip`

Each archive contains the `gsc` binary (`gsc.exe` on Windows), `README.md`, and `LICENSE` (if present). A `checksums.txt` (SHA-256) is emitted alongside.

**And** binaries are built with `-ldflags "-s -w -X main.version={{.Version}}"` so `gsc --version` reports the release tag (matching `Makefile` and `cmd/gsc/main.go:10`).

**And** CGO is disabled for all targets (`CGO_ENABLED=0`).

### FR-002 [MUST] Tag-triggered GitHub Actions release workflow

**Given** `.github/workflows/release.yml`,
**when** a git tag matching `v*` is pushed,
**then** the workflow:

1. Checks out the repo with full history (`fetch-depth: 0`) so GoReleaser can compute the changelog.
2. Sets up Go using the version from `go.mod` (currently 1.26.2).
3. Runs `goreleaser release --clean` with `GITHUB_TOKEN` scoped to `contents: write`.
4. Publishes a GitHub Release containing the five archives, `checksums.txt`, and an auto-generated changelog (commits since the previous tag).

**And** the workflow runs on `ubuntu-latest` (GoReleaser cross-compiles; no matrix needed).

**And** non-tag pushes MUST NOT trigger a publish; a separate lightweight job (or the existing CI if present) can still run snapshot builds on PRs without publishing — that is a non-goal here.

### FR-003 [MUST] `install.sh` one-liner for macOS/Linux

**Given** `install.sh` at repo root,
**when** a user runs `curl -fsSL https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.sh | sh`,
**then** the script:

1. Detects OS (`linux`|`darwin`) and arch (`amd64`|`arm64`); exits 1 with a clear message on unsupported combos (incl. `windows`, `i386`, `armv7`).
2. Resolves the latest release tag by hitting `https://api.github.com/repos/KLIXPERT-io/gsc-cli/releases/latest` unless `GSC_VERSION` env var is set (pins version).
3. Downloads the matching archive and `checksums.txt` from the release.
4. Verifies the archive's SHA-256 against `checksums.txt` (fails on mismatch).
5. Extracts `gsc` to `$INSTALL_DIR` (default: `/usr/local/bin` if writable, else `$HOME/.local/bin`; overridable via `INSTALL_DIR` env var). Creates the directory if missing.
6. Prints the installed path, the version, and a hint if `$INSTALL_DIR` is not on `$PATH`.

**And** the script uses only POSIX sh + `curl` + `tar` + `shasum`/`sha256sum` (no bash-isms, no jq). It sets `set -eu` and cleans up the temp dir on exit.

**And** running `sh install.sh --help` prints usage: supported env vars and flags.

### FR-004 [MUST] `install.ps1` one-liner for Windows

**Given** `install.ps1` at repo root,
**when** a user runs `irm https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.ps1 | iex` in PowerShell 5.1+,
**then** the script mirrors `install.sh` behavior for `windows_amd64`:

1. Resolves latest release tag (or `$env:GSC_VERSION`).
2. Downloads the `_windows_amd64.zip` and `checksums.txt`.
3. Verifies SHA-256.
4. Extracts `gsc.exe` to `$env:LOCALAPPDATA\Programs\gsc\` (overridable via `$env:INSTALL_DIR`).
5. Prints the installed path, version, and a hint to add the directory to the user `PATH` if not already present.

**And** the script sets `$ErrorActionPreference = 'Stop'` and does not require Admin.

### FR-005 [MUST] `INSTALL.md`

**Given** `INSTALL.md` at the repo root,
**when** a user reads it,
**then** it documents every supported install path with copy-pasteable commands:

- macOS/Linux one-liner (FR-003)
- Windows one-liner (FR-004)
- Manual download from the GitHub Releases page (all 5 archives)
- `go install github.com/KLIXPERT-io/gsc-cli/cmd/gsc@latest` for users with a Go toolchain
- `make build` from source
- How to pin a version (`GSC_VERSION=v1.2.3 sh install.sh`)
- How to verify checksums manually
- How to uninstall (delete the binary)

**And** `README.md` gains a short "Install" section linking to `INSTALL.md`.

### FR-006 [SHOULD] README release badge and version output

**Given** a release is published,
**when** `gsc --version` is run,
**then** it prints the released tag (e.g. `gsc version v1.2.3`). This is inherited behavior from existing `-X main.version=<tag>` wiring at `cmd/gsc/main.go:10` — verify it still reports correctly with a GoReleaser-built binary.

**And** `README.md` carries a "latest release" badge linking to the Releases page.

## 4. Non-functional notes

- **Security:** installers verify SHA-256 before extracting; no `curl | sh` without integrity check. No code signing yet (open question).
- **Determinism:** GoReleaser `builds.mod_timestamp: '{{ .CommitTimestamp }}'` for reproducible archives.
- **Size:** stripped binaries via `-s -w`; acceptable to ship uncompressed `gsc` inside tar/zip (goreleaser default).
- **Changelog:** auto-generated from commits; since this repo uses Conventional Commits, leverage goreleaser's `changelog.use: github` or `git` with group templates.

## 5. Out of scope / non-goals

- Homebrew tap, Scoop bucket, APT/DNF repos (follow-up).
- Code signing (macOS notarization, Windows Authenticode).
- Docker image publishing.
- `linux_armv7`, `linux_386`, `freebsd`, `openbsd` targets.
- Auto-update mechanism inside the CLI.
- Signed/reproducible SBOMs.
- PR-time snapshot publishing (local `make release-snapshot` already covers it).

## 6. Technical notes

**New files:**
- `.goreleaser.yml`
- `.github/workflows/release.yml`
- `install.sh`
- `install.ps1`
- `INSTALL.md`

**Edited files:**
- `README.md` — add Install section + release badge (FR-005/006).
- `Makefile` — optional: add `release` target that wraps `goreleaser release`; keep existing `release-snapshot`.

**Dependencies:**
- GoReleaser (via `goreleaser/goreleaser-action@v6` in CI; locally via `brew install goreleaser` or `go install`).
- `gh` CLI is *not* required in the workflow — `GITHUB_TOKEN` is passed directly to goreleaser.

**Version propagation:** `cmd/gsc/main.go:10` declares `var version = "dev"`; `-X main.version={{.Version}}` in ldflags overrides it. Confirmed consumer: `internal/cmd/root.go:122` sets `root.Version = version`.

**Archive layout (unix):**
```
gsc_v1.2.3_linux_amd64/
  gsc
  README.md
  LICENSE     # if exists
```

**install.sh detection matrix:**
| uname -s | uname -m | archive suffix |
|---|---|---|
| Linux | x86_64 | linux_amd64.tar.gz |
| Linux | aarch64 / arm64 | linux_arm64.tar.gz |
| Darwin | x86_64 | darwin_amd64.tar.gz |
| Darwin | arm64 | darwin_arm64.tar.gz |
| * | * | error: unsupported |

## 7. Open questions & assumptions

- **ASSUMPTION:** repo owner/name `KLIXPERT-io/gsc-cli` is stable (visible in `go.mod`). Installers hardcode this URL.
- **ASSUMPTION:** no `LICENSE` file review needed; if absent, goreleaser `files:` list tolerates missing optional files or needs explicit guard.
- **ASSUMPTION:** `main.version` ldflag path stays `main.version`. If the binary is ever split out, update `.goreleaser.yml`.
- **OPEN QUESTION:** Windows `arm64` — skip for v1 (only `windows_amd64` in FR-001)? Decision: skip for now. Revisit if a user asks.
- **OPEN QUESTION:** Code signing / notarization. Deferred; users will see Gatekeeper/SmartScreen warnings on first run. Document in INSTALL.md.
- **OPEN QUESTION:** Should the CI workflow also run `go test ./...` as a release gate, or trust that tests ran on the pre-tag commit? Recommend: run `go vet` + `go build` smoke only; full tests are CI's job (not yet set up — separate feature).
