---
title: CLI Background Auto-Update
slug: auto-update
status: done
version: 0.1
kind: feature
owner: TBD
created: 2026-04-15
updated: 2026-04-15
---

## 1. Summary

Have `gsc` keep itself up to date without user effort. On any command, a non-blocking background goroutine checks the GitHub Releases API at most once per 24 hours; if a newer stable tag is available and the binary's install location is writable, it downloads the matching archive, verifies the SHA-256 against `checksums.txt`, and atomically swaps the binary in place. The current invocation is unaffected; the next invocation runs the new version. Disabling is one flag (`GSC_NO_UPDATE=1`) or one config key (`auto_update: false`). Success = a user on `v0.1.0` runs any `gsc` command, comes back the next day, runs another command, and `gsc --version` reports the latest published tag — with zero prompts.

## 2. User story / trigger

- **Operator** runs `gsc` daily and wants new features and fixes without remembering to re-curl the installer.
- **Maintainer** ships a release via the existing tag-driven pipeline (`docs/feature/release-automation`) and wants every existing install to migrate within ~24h with no broadcast required.
- **Locked-down environment** (CI, system-managed install, Homebrew/Scoop in future) needs a guaranteed off-switch so the package manager remains the source of truth.

## 3. Functional requirements

### FR-001 [MUST] Background check, throttled to once per 24h

**Given** any `gsc` subcommand is invoked,
**when** the command starts,
**then** a goroutine is launched that:

1. Reads `~/.config/gsc/update-state.json` (or platform equivalent via `os.UserConfigDir()`); if `last_check_at` is < 24h ago, exits silently.
2. Otherwise, GETs `https://api.github.com/repos/KLIXPERT-io/gsc-cli/releases/latest` with a 5s timeout and `User-Agent: gsc-cli/<version>`.
3. Parses `tag_name`, compares against the running `version` using semver (ignore the leading `v`); if equal or older, writes `last_check_at = now` and exits.
4. If newer, proceeds to FR-003 (download + apply).
5. On any error (network, 4xx, 5xx, parse), logs at debug level only and writes `last_check_at = now` so a broken endpoint does not retry-storm.

**And** the goroutine MUST NOT block the foreground command's exit. The main process exits when the user's command finishes; the updater either completes its swap before then or is abandoned (download is resumable on next check, not transactionally critical).

**And** the check is skipped entirely when:
- `GSC_NO_UPDATE=1` is set (FR-005).
- `auto_update: false` in the config file (FR-005).
- `version == "dev"` (developer build, no tag to compare against).
- Stdin is non-TTY *and* the command is `gsc auth login` flow (avoid surprising side effects in scripted auth) — actually deferred; see Open Questions.
- The running binary's path is not writable by the current user (FR-004).

### FR-002 [MUST] Stable channel only, semver comparison

**Given** the GitHub `releases/latest` endpoint,
**when** the latest tag is fetched,
**then** only non-prerelease tags are considered (`prerelease == false`, which `releases/latest` already enforces). Tags like `v1.2.3-rc.1` are ignored.

**And** version comparison uses semver: `1.10.0 > 1.9.9`, `1.0.0 > 1.0.0-rc.1`. Use `golang.org/x/mod/semver` (already a low-risk std-adjacent dep) — if a different semver lib is already vendored, prefer that.

### FR-003 [MUST] Download, verify, atomic swap

**Given** a newer tag `vX.Y.Z` has been resolved,
**when** the updater proceeds,
**then** it:

1. Computes the archive name from `runtime.GOOS`/`runtime.GOARCH` matching the matrix in `release-automation` FR-001 (e.g. `gsc_X.Y.Z_darwin_arm64.tar.gz`, `gsc_X.Y.Z_windows_amd64.zip`).
2. Downloads the archive and `checksums.txt` from the release's asset URLs to a temp directory under `os.TempDir()`.
3. Verifies the archive's SHA-256 against `checksums.txt`. On mismatch: delete temp dir, log debug, write `last_check_at = now`, exit.
4. Extracts only the `gsc` (or `gsc.exe`) binary from the archive.
5. Resolves the running binary's absolute path via `os.Executable()` + `filepath.EvalSymlinks`.
6. Atomically replaces the binary:
   - **Unix:** `os.Rename(newBinary, runningPath)` — works while the running process holds the old inode.
   - **Windows:** rename current binary to `gsc.exe.old` (allowed even when in use), then `os.Rename(newBinary, runningPath)`. On next check, delete any stale `gsc.exe.old`.
7. `chmod 0755` the new binary on unix.
8. Writes `update-state.json` with `last_check_at`, `last_installed_version`, `last_installed_at`.

**And** the swap MUST be atomic from the perspective of any concurrent `gsc` process — partial writes are not allowed. Use write-to-temp-then-rename within the same filesystem as the target.

**And** on permission errors during step 6 (binary path read-only), the updater MUST cleanly abandon the update, log debug, and mark the install path as non-writable in `update-state.json` to avoid re-attempting downloads next run.

### FR-004 [MUST] Refuse to update system-managed installs

**Given** the running binary's path,
**when** the updater starts,
**then** it skips the entire flow if any of these are true:

- The binary path begins with a known package-manager prefix: `/opt/homebrew`, `/usr/local/Cellar`, `/home/linuxbrew`, `/snap`, `/var/lib/flatpak`, `C:\Program Files`, `C:\ProgramData\chocolatey`, `C:\Users\*\scoop`.
- The binary file is not writable by the current uid (`unix.Access(path, unix.W_OK)` or equivalent).
- On unix, the binary is owned by a different uid than the current process (suggests root-installed).

**And** the skip MUST be silent (debug log only) and write `install_managed: true` to state so future checks short-circuit early.

### FR-005 [MUST] Opt-out via env var and config

**Given** the user wants to disable auto-update,
**when** either of these is true:
- Env var `GSC_NO_UPDATE` is set to a non-empty value other than `0`/`false`,
- Config file (`~/.config/gsc/config.yaml` or current config path used by `internal/config`) contains `auto_update: false`,

**then** the updater MUST NOT make any network requests and MUST NOT touch `update-state.json`.

**And** `gsc config set auto_update false` (existing config command pattern, see `internal/cmd/config.go`) sets the config key. If no such pattern exists yet, document the manual edit in the config file.

### FR-006 [MUST] User-visible status surface

**Given** the user wants to know what auto-update is doing,
**when** they run `gsc update status`,
**then** the command prints:

- Current version, latest known version, channel (`stable`).
- `last_check_at`, `last_installed_version`, `last_installed_at` from state.
- Whether auto-update is enabled, and if disabled, the reason (env var / config / managed install / non-writable).
- The install path resolved via `os.Executable()`.

**And** `gsc update check` forces a check now (bypasses the 24h throttle, still respects opt-out and managed-install guards).

**And** `gsc update apply` forces a download + swap of the latest version (useful for "I just published, update now"). Returns non-zero on any failure with a clear message — unlike the background updater which is silent.

### FR-007 [SHOULD] Post-update notice on next run

**Given** an auto-update completed since the last invocation,
**when** the user runs the next `gsc` command,
**then** a one-line stderr notice prints before normal output: `gsc: updated to vX.Y.Z (was vA.B.C)`. Suppressible via `GSC_NO_UPDATE_NOTICE=1`. Detection: compare `last_installed_version` in state vs. running `version`.

### FR-008 [SHOULD] Respect HTTP proxies and corp CA bundles

**Given** the user's environment defines `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` / `SSL_CERT_FILE`,
**when** the updater makes requests,
**then** it MUST honor them (use Go's default `http.Transport` / `http.ProxyFromEnvironment` — free if we don't override).

## 4. Non-functional notes

- **Latency:** the background goroutine MUST add 0ms to user-perceived command latency. Foreground command exits on its own schedule; updater completes opportunistically.
- **Network:** at most one `releases/latest` GET per 24h per user. Archive downloads only when an update exists. 5s timeout on the API call, 60s on the archive download.
- **Security:** all downloads HTTPS-only; SHA-256 verification mandatory before swap. No code signing yet (inherited limitation from `release-automation`).
- **Privacy:** the only outbound request is to `api.github.com` and `objects.githubusercontent.com`. No telemetry, no user identifiers in headers beyond `User-Agent: gsc-cli/<version>`.
- **Rate limits:** unauthenticated GitHub API allows 60 req/h per IP — 1/day/user is well within.
- **Crash safety:** if the process is killed mid-download, temp files leak in `os.TempDir()`. Acceptable; OS cleans them. Mid-rename is atomic on POSIX; on Windows the `.old` file may persist and is cleaned on next check.

## 5. Out of scope / non-goals

- Rollback / downgrade flow.
- Delta/binary patching (download full archive every time).
- Notifying the user *during* an in-flight update (it's silent by design).
- Updating a binary installed via Homebrew/Scoop/apt (FR-004 explicitly skips).
- Self-update of the install scripts (`install.sh`/`install.ps1`).
- Background daemon / scheduled task — checks happen on user-initiated invocations only.
- Signed updates / cosign / TUF — deferred until code signing exists in `release-automation`.
- Channel selection beyond `stable`. Pre-release opt-in deferred.
- Telemetry on update success/failure rates.

## 6. Technical notes

**New files:**
- `internal/update/update.go` — public `CheckAndApply(ctx, currentVersion string)` plus internals for fetch/verify/swap.
- `internal/update/state.go` — read/write `update-state.json` under `os.UserConfigDir()/gsc/`.
- `internal/update/platform_unix.go` / `internal/update/platform_windows.go` — atomic-swap and writability checks.
- `internal/cmd/update.go` — `gsc update status|check|apply` subcommands.

**Edited files:**
- `cmd/gsc/main.go` — pass `version` to a new `update.Background(version)` call before `cmd.Execute`. Updater goroutine launched from there (or from `internal/cmd/root.go` PersistentPreRun — TBD which is cleaner).
- `internal/cmd/root.go` — register the `update` subcommand; add the post-update notice (FR-007) hook.
- `internal/config/config.go` — add `AutoUpdate bool` field (default true).
- `INSTALL.md` — document opt-out (`GSC_NO_UPDATE=1`, config key) and managed-install behavior.
- `README.md` — one-line mention under Install.

**Dependencies:**
- `golang.org/x/mod/semver` for version compare (small, std-adjacent).
- No new HTTP client; use `net/http` defaults.
- `archive/tar` + `compress/gzip` (unix) and `archive/zip` (windows) — all stdlib.

**State file shape:**
```json
{
  "last_check_at": "2026-04-15T08:12:00Z",
  "last_installed_version": "v0.2.0",
  "last_installed_at": "2026-04-15T08:12:01Z",
  "install_managed": false,
  "install_path": "/usr/local/bin/gsc"
}
```

**Asset URL pattern (matches `release-automation` FR-001):**
`https://github.com/KLIXPERT-io/gsc-cli/releases/download/<tag>/gsc_<version-no-v>_<os>_<arch>.<tar.gz|zip>`
plus `checksums.txt` from the same release.

**Background lifecycle:** the goroutine is launched and detached. The main goroutine does NOT `wg.Wait()` on it — if the user's command finishes first, the updater is abandoned. This is intentional: user latency must be unaffected. State writes are best-effort. Concurrent invocations are protected by an advisory lockfile (`update-state.lock` via `flock`/`LockFileEx`) so two parallel `gsc` runs don't both download.

## 7. Open questions & assumptions

- **ASSUMPTION:** `release-automation` ships first; this feature consumes its archive naming and `checksums.txt`. If naming changes, FR-003 step 1 must update.
- **ASSUMPTION:** existing config loader at `internal/config/config.go` supports adding fields without migration. If it's a strict schema, we add a migration shim.
- **ASSUMPTION:** the CLI does not currently launch any other long-lived goroutines that would conflict with detached updater.
- **OPEN QUESTION:** Should the background updater run during interactive auth flows (`gsc auth login`)? Risk: the binary swap mid-OAuth is harmless (current process keeps inode), but a user who Ctrl-C's the auth flow may be surprised by a partial download in temp. Recommend: no special-casing for v1; revisit if reports come in.
- **OPEN QUESTION:** Initial check on a brand-new install — should it run immediately, or wait the full 24h? Recommend: run on first invocation (no `last_check_at` in state) so users on stale tarballs catch up fast.
- **OPEN QUESTION:** Should `gsc update apply` support `--version vX.Y.Z` to pin to a specific release (downgrade for debugging)? Deferred; out of scope unless asked.
- **OPEN QUESTION:** Windows `.old` cleanup on next check — what if the user has revoked write perms between check and cleanup? Just log debug and move on; not worth a retry loop.
