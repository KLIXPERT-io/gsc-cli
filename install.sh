#!/bin/sh
# install.sh — install the gsc CLI on macOS or Linux.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/KLIXPERT-io/gsc-cli/main/install.sh | sh
#
# Env vars:
#   GSC_VERSION   pin a release tag (e.g. v1.2.3); default: latest
#   INSTALL_DIR   target directory; default: /usr/local/bin if writable, else $HOME/.local/bin
set -eu

REPO="KLIXPERT-io/gsc-cli"
BIN="gsc"

usage() {
  cat <<EOF
$BIN installer

Usage: sh install.sh [--help]

Environment variables:
  GSC_VERSION   release tag to install (default: latest)
  INSTALL_DIR   install directory (default: /usr/local/bin or \$HOME/.local/bin)
EOF
}

case "${1:-}" in
  -h|--help) usage; exit 0 ;;
esac

err() { printf 'error: %s\n' "$*" >&2; exit 1; }

# --- detect platform ---
os_raw="$(uname -s)"
arch_raw="$(uname -m)"

case "$os_raw" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) err "unsupported OS: $os_raw (use install.ps1 on Windows)" ;;
esac

case "$arch_raw" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "unsupported architecture: $arch_raw" ;;
esac

# --- resolve version ---
have() { command -v "$1" >/dev/null 2>&1; }
have curl || err "curl is required"
have tar  || err "tar is required"

if have sha256sum; then sha_cmd="sha256sum"
elif have shasum; then sha_cmd="shasum -a 256"
else err "sha256sum or shasum is required"
fi

version="${GSC_VERSION:-}"
if [ -z "$version" ]; then
  # Parse "tag_name": "v1.2.3" without jq.
  version="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
    | head -n1)"
  [ -n "$version" ] || err "could not resolve latest release tag"
fi

# strip leading v for archive name (goreleaser strips it from .Version)
ver_noV="${version#v}"
archive="${BIN}_${ver_noV}_${os}_${arch}.tar.gz"
base_url="https://github.com/${REPO}/releases/download/${version}"

# --- choose install dir ---
target_dir="${INSTALL_DIR:-}"
if [ -z "$target_dir" ]; then
  if [ -w "/usr/local/bin" ] || { [ ! -e "/usr/local/bin" ] && mkdir -p /usr/local/bin 2>/dev/null; }; then
    target_dir="/usr/local/bin"
  else
    target_dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$target_dir" || err "cannot create $target_dir"

# --- download + verify + extract ---
tmp="$(mktemp -d 2>/dev/null || mktemp -d -t gsc-install)"
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

printf 'Downloading %s ...\n' "$archive"
curl -fsSL -o "$tmp/$archive"      "$base_url/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base_url/checksums.txt"

printf 'Verifying checksum ...\n'
expected="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
[ -n "$expected" ] || err "no checksum entry for $archive"
actual="$(cd "$tmp" && $sha_cmd "$archive" | awk '{print $1}')"
[ "$expected" = "$actual" ] || err "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$BIN" ] || err "binary $BIN not found inside archive"

install_path="$target_dir/$BIN"
mv "$tmp/$BIN" "$install_path"
chmod 0755 "$install_path"

printf '\nInstalled %s to %s\n' "$version" "$install_path"
"$install_path" --version 2>/dev/null || true

case ":$PATH:" in
  *":$target_dir:"*) ;;
  *) printf '\nNote: %s is not in your $PATH. Add it with:\n  export PATH="%s:$PATH"\n' "$target_dir" "$target_dir" ;;
esac
