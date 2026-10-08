#!/bin/sh
# brewkeg CLI installer — no npm, no go, no runtime deps.
#   curl -fsSL https://brewkeg.dev/install.sh | sh
set -eu

REPO="${BREWKEG_REPO:-brewkeghq/brewkeg-gateway}"
INSTALL_DIR="${BREWKEG_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin) os=darwin ;;
  linux)  os=linux ;;
  *) die "unsupported OS: $os (this installer covers macOS and Linux)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

say "→ fetching latest brewkeg release for ${os}/${arch}"

# The tag and the version are different strings: the URL needs the FULL tag
# while the asset name needs only the version. Assuming they are the same is how
# this installer ends up curling a URL that does not exist.
tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)
[ -n "$tag" ] || die "could not read the latest release from GitHub"

version=$(printf '%s' "$tag" | sed 's/^cli-//; s/^v//')
[ -n "$version" ] || die "could not read a version from the tag $tag"

# GoReleaser names assets with the capitalised GOOS: Darwin, Linux.
# `tr` rather than `sed 's/^./\u&/'`: the \u escape is a GNU extension, and
# BSD sed — the one on every Mac — passes it through literally, producing an
# asset called "brewkeg_0.2.0_udarwin_arm64".
gos=$(printf '%s' "${os:0:1}" | tr '[:lower:]' '[:upper:]')${os:1}
name="brewkeg_${version}_${gos}_${arch}"
base="https://github.com/${REPO}/releases/download/${tag}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "→ downloading brewkeg ${version}"
curl -fsSL "$base/${name}" -o "$tmp/brewkeg" || die "download failed: $base/${name}"
chmod +x "$tmp/brewkeg"

say "→ verifying checksum"
curl -fsSL "$base/brewkeg_${version}_checksums.txt" -o "$tmp/sums" 2>/dev/null || true
if [ -s "$tmp/sums" ]; then
  expected=$(awk -v f="$name" '$2 == f || $2 == "*"f {print $1}' "$tmp/sums" | head -n1)
  actual=$(sha256sum "$tmp/brewkeg" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$tmp/brewkeg" | awk '{print $1}')
  [ -n "$expected" ] || die "no checksum for $name in the release"
  [ "$expected" = "$actual" ] || die "checksum mismatch — refusing to install"
  say "  checksum ok"
else
  say "  (no checksum file published for this release, skipping verification)"
fi

mkdir -p "$INSTALL_DIR"
say "→ installing to $INSTALL_DIR/brewkeg"
mv "$tmp/brewkeg" "$INSTALL_DIR/brewkeg"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "  $INSTALL_DIR is not on your PATH. Add this to your shell rc:"
    say "    export PATH=\"\$HOME/.local/bin:\$PATH\""
    ;;
esac

say ""
"$INSTALL_DIR/brewkeg" version
say ""
say "  Next:  brewkeg setup"
say "  Get your key at ${BREWKEG_BASE_URL:-https://brewkeg.dev}/dashboard"
say ""
say "  Undo anything brewkeg changes with:  brewkeg restore"