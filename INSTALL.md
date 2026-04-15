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

## Uninstalling

Delete the binary:

```sh
rm "$(command -v gsc)"
```

```powershell
Remove-Item "$env:LOCALAPPDATA\Programs\gsc\gsc.exe"
```
