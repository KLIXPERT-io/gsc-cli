# Installing `gsc`

Pick the path that fits your platform. All install methods deliver the same statically-linked `gsc` binary.

## macOS / Linux — one-liner

```sh
curl -fsSL https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.sh | sh
```

The installer detects your OS (`linux` / `darwin`) and architecture (`amd64` / `arm64`), downloads the latest release archive plus `checksums.txt`, verifies the SHA-256, and installs `gsc` to `/usr/local/bin` (if writable) or `~/.local/bin`.

Pin a version or override the install location:

```sh
GSC_VERSION=v1.2.3 INSTALL_DIR="$HOME/bin" \
  curl -fsSL https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.sh | sh
```

Run `sh install.sh --help` for the full list of options.

## Windows — one-liner (PowerShell 5.1+)

```powershell
irm https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.ps1 | iex
```

Installs `gsc.exe` to `%LOCALAPPDATA%\Programs\gsc\` (no admin required). Override with environment variables:

```powershell
$env:GSC_VERSION = 'v1.2.3'
$env:INSTALL_DIR = "$env:USERPROFILE\bin"
irm https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.ps1 | iex
```

> The first run may show a SmartScreen warning because the binary is not (yet) Authenticode-signed. Choose "Run anyway" — or verify the SHA-256 manually (see below).

## Manual download

Grab the archive for your platform from the [Releases page](https://github.com/KLIXPERT-io/gsc-cli/releases/latest):

| Platform        | Archive                                |
| --------------- | -------------------------------------- |
| Linux amd64     | `gsc_<version>_linux_amd64.tar.gz`     |
| Linux arm64     | `gsc_<version>_linux_arm64.tar.gz`     |
| macOS amd64     | `gsc_<version>_darwin_amd64.tar.gz`    |
| macOS arm64     | `gsc_<version>_darwin_arm64.tar.gz`    |
| Windows amd64   | `gsc_<version>_windows_amd64.zip`      |

Extract, then move `gsc` (or `gsc.exe`) into a directory on your `$PATH`.

## With a Go toolchain

```sh
go install github.com/KLIXPERT-io/gsc-cli/cmd/gsc@latest
```

The binary lands in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`). Note: this build does not embed the release tag in `gsc --version`.

## From source

```sh
git clone https://github.com/KLIXPERT-io/gsc-cli.git
cd gsc-cli
make build      # produces ./gsc
make install    # installs via `go install`
```

## Verifying checksums manually

Every release ships a `checksums.txt` file alongside the archives. To verify before installing:

```sh
curl -fsSLO https://github.com/KLIXPERT-io/gsc-cli/releases/download/v1.2.3/gsc_1.2.3_linux_amd64.tar.gz
curl -fsSLO https://github.com/KLIXPERT-io/gsc-cli/releases/download/v1.2.3/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
```

## Pinning a version

```sh
GSC_VERSION=v1.2.3 curl -fsSL https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.sh | sh
```

```powershell
$env:GSC_VERSION = 'v1.2.3'
irm https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.ps1 | iex
```

## Auto-Update

Once installed, `gsc` keeps itself current. On every invocation a background goroutine checks the GitHub Releases API at most once per 24 hours; if a newer stable tag is published and the running binary is writable, it downloads the matching archive, verifies the SHA-256 against `checksums.txt`, and atomically swaps the binary in place. The current command is unaffected — the next `gsc` invocation runs the new version.

### Disabling auto-update

Two equivalent opt-outs:

```bash
export GSC_NO_UPDATE=1
```

Or in `~/.config/gsc/config.toml`:

```toml
auto_update = false
```

When either is set, no network requests are made and `update-state.json` is not touched.

### Managed installs are skipped automatically

`gsc` detects package-managed binaries by install-path prefix and never auto-updates them — updates come through the package manager instead. The detected prefixes are:

- `/opt/homebrew`, `/usr/local/Cellar` (Homebrew)
- `/home/linuxbrew` (Linuxbrew)
- `/snap` (Snap)
- `/var/lib/flatpak` (Flatpak)
- `C:\ProgramData\chocolatey` (Chocolatey)
- `C:\Users\*\scoop` (Scoop)
- `C:\Program Files`

A binary that is not writable by the current user (or owned by a different uid on unix) is also skipped.

### Inspecting / forcing updates

```bash
gsc update status   # current + latest version, last check time, enabled state (with reason if disabled)
gsc update check    # force a check now, bypassing the 24h throttle
gsc update apply    # force download + atomic swap to the latest version
```

`update status` also prints the resolved install path and the last-installed version recorded in state. `update check` and `update apply` still respect the opt-out and managed-install guards.

### Post-update notice

After a successful background update, the next `gsc` command prints a one-line notice to stderr before its normal output:

```
gsc: updated to vX.Y.Z (was vA.B.C)
```

Suppress it with `GSC_NO_UPDATE_NOTICE=1`.

## Cutting a release (maintainers)

The release version lives in the [`VERSION`](./VERSION) file at the repo root. To ship a new release:

1. Bump `VERSION` (e.g. `0.1.0` → `0.2.0`) and merge to `main`.
2. The `Auto Tag & Release` workflow reads the file, creates a matching `vX.Y.Z` git tag, and triggers the release pipeline (`release.yml`).
3. GoReleaser builds the 5 archives and publishes a GitHub Release with `checksums.txt`.

Manual fallback: `git tag v0.2.0 && git push --tags` runs the same release pipeline directly.

## Uninstalling

Delete the binary:

```sh
rm "$(command -v gsc)"
```

```powershell
Remove-Item "$env:LOCALAPPDATA\Programs\gsc\gsc.exe"
```
