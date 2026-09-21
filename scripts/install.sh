#!/bin/sh
# Installs the latest wms release for this machine, after verifying its SHA-256.
#   curl -fsSL https://raw.githubusercontent.com/KC-OU/KC-LEGO-CLI-NEW/main/scripts/install.sh | sh
# Options (environment): INSTALL_DIR (default /usr/local/bin), VERSION (default: latest, e.g. v1.2.0).
set -eu

REPO="KC-OU/KC-LEGO-CLI-NEW"
API="${WMS_INSTALL_API:-https://api.github.com}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'install failed: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

need curl; need tar; need uname
if command -v sha256sum >/dev/null 2>&1; then sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "sha256sum or shasum is required to verify the download"; fi

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) die "unsupported OS $(uname -s): only Linux and macOS are built" ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) die "unsupported CPU $(uname -m)" ;; esac

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

if [ -n "${VERSION:-}" ]; then tag="$VERSION"; else
  say "Looking up the latest release..."
  curl -fsSL -H 'Accept: application/vnd.github+json' "$API/repos/$REPO/releases/latest" -o "$tmp/release.json" \
    || die "could not reach GitHub, or no release has been published yet"
  tag="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/release.json" | head -n1)"
  [ -n "$tag" ] || die "no release found"
fi
case "$tag" in v[0-9]*) ;; *) die "unexpected version tag '$tag'" ;; esac

ver="${tag#v}"
name="wms-go_${ver}_${os}_${arch}.tar.gz"
base="${WMS_INSTALL_DOWNLOAD:-https://github.com/$REPO/releases/download/$tag}"
say "Downloading $name ($tag)..."
curl -fsSL "$base/$name" -o "$tmp/$name" || die "download failed: $base/$name"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || die "could not download checksums.txt"

want="$(awk -v n="$name" '$2==n || $2=="*"n {print $1}' "$tmp/checksums.txt" | head -n1)"
[ -n "$want" ] || die "checksums.txt does not list $name"
got="$(sha "$tmp/$name")"
[ "$got" = "$want" ] || die "checksum mismatch (got $got, expected $want): NOT installed"
say "Checksum verified."

tar -xzf "$tmp/$name" -C "$tmp" wms || die "the archive does not contain the wms binary"
[ -x "$tmp/wms" ] || chmod 755 "$tmp/wms"
"$tmp/wms" version >/dev/null 2>&1 || die "the downloaded binary does not run on this machine"

mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then cp "$tmp/wms" "$INSTALL_DIR/wms-go.new" && mv "$INSTALL_DIR/wms-go.new" "$INSTALL_DIR/wms-go"
else need sudo; sudo cp "$tmp/wms" "$INSTALL_DIR/wms-go.new" && sudo mv "$INSTALL_DIR/wms-go.new" "$INSTALL_DIR/wms-go"; fi
say "Installed $INSTALL_DIR/wms-go ($tag)."
say "Next: wms-go doctor     Docs: https://kc-ou.github.io/KC-LEGO-CLI-NEW/"
