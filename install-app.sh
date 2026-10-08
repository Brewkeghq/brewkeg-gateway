#!/bin/sh
# Brewkeg Gateway desktop installer — downloads the latest signed-for-your-OS
# build, drops it in /Applications (or the Windows/Linux equivalent) and opens it.
#   curl -fsSL https://brewkeg.dev/install-app.sh | sh
#
# Why this exists instead of a download link: a browser stamps
# com.apple.quarantine on everything it fetches, and Gatekeeper then refuses to
# open the app with "Apple could not verify ... is free of malware". curl does
# not set that flag, so an app fetched this way opens with no dialog and no
# certificate. On Windows the same is true of the Zone.Identifier that makes
# SmartScreen show "Windows protected your PC".
set -eu

REPO="${BREWKEG_REPO:-brewkeghq/brewkeg-gateway}"
APP_NAME=brewkeg-gateway

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin) asset_os=macos;    install_dir="${BREWKEG_APP_DIR:-/Applications}" ;;
  linux)  asset_os=linux;    install_dir="$HOME/.local/share/brewkeg" ;;
  *) die "unsupported OS: $os — macOS and Linux only. Windows: install-app.ps1" ;;
esac

# /Applications needs a writable session on every macOS user account, but a
# locked-down or managed machine can deny it. Falling back to ~/Applications
# beats aborting the install when the only problem is permissions.
if [ "$os" = darwin ] && [ ! -w "$install_dir" ] && [ -z "${BREWKEG_APP_DIR:-}" ]; then
  say "→ $install_dir is not writable, using $HOME/Applications instead"
  install_dir="$HOME/Applications"
  mkdir -p "$install_dir"
fi

tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)
[ -n "$tag" ] || die "could not read the latest release from GitHub"

# The tag carries the v; the asset name does not. They are different strings.
version=$(printf '%s' "$tag" | sed 's/^v//')
[ -n "$version" ] || die "could not read a version from the tag $tag"

case "$os" in
  darwin) ext=zip ;;
  linux)  ext=tar.gz ;;
esac
asset="${APP_NAME}_${tag}_${asset_os}.${ext}"
base="https://github.com/${REPO}/releases/download/${tag}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "→ downloading Brewkeg Gateway ${version} for ${os}"
curl -fsSL "$base/$asset" -o "$tmp/$APP_NAME.$ext" || die "download failed: $base/$asset"

say "→ unpacking"
case "$ext" in
  zip)    ditto -x -k "$tmp/$APP_NAME.$ext" "$tmp/unpacked" ;;
  tar.gz) mkdir -p "$tmp/unpacked" && tar xzf "$tmp/$APP_NAME.$ext" -C "$tmp/unpacked" ;;
esac

app="$tmp/unpacked/$APP_NAME.app"
[ -d "$app" ] || app="$tmp/unpacked/$APP_NAME"   # Linux ships a bare binary
[ -e "$app" ] || die "unexpected archive contents: no $APP_NAME found"

# Strip the quarantine flag so Gatekeeper/SmartScreen never engages. Without
# -r on purpose: it recurses into the bundle and fails on files that carry no
# such attribute, which aborts the whole install on some releases. The flag
# that matters lives on the .app itself.
if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$app" 2>/dev/null || true
  # ditto writes an AppleDouble sidecar for anything the quarantine flag marks;
  # a leftover ._file inside the bundle makes the app look tampered with.
  find "$app" -name '._*' -delete 2>/dev/null || true
fi

# Replace rather than merge. Merging an old bundle into a new one leaves stale
# Resources behind, and the user ends up with a build whose front end is half
# old and half new.
if [ -e "$install_dir/$APP_NAME.app" ] || [ -e "$install_dir/$APP_NAME" ]; then
  say "→ replacing the existing copy in $install_dir"
  rm -rf "${install_dir:?}/$APP_NAME.app" "${install_dir:?}/$APP_NAME"
fi
mkdir -p "$install_dir"

say "→ installing to $install_dir"
if [ "$os" = darwin ]; then
  mv "$app" "$install_dir/$APP_NAME.app"
else
  mkdir -p "$install_dir/$APP_NAME"
  mv "$app" "$install_dir/$APP_NAME/$APP_NAME"
  chmod +x "$install_dir/$APP_NAME/$APP_NAME"
fi

say "→ starting Brewkeg Gateway"
case "$os" in
  darwin) open "$install_dir/$APP_NAME.app" ;;
  linux)  "$install_dir/$APP_NAME/$APP_NAME" & ;;
esac

say ""
say "  Installed Brewkeg Gateway ${version}"
say "  Paste your key at ${BREWKEG_BASE_URL:-https://brewkeg.dev}/dashboard/keys"
say ""
case "$os" in
  darwin)
    say "  macOS will ask for permission to edit files in your home folder the"
    say "  first time you flip a switch. That is the app doing its job."
    ;;
  linux)
    say "  Installed to $install_dir/$APP_NAME/$APP_NAME"
    say "  To uninstall: rm -rf $install_dir/$APP_NAME"
    ;;
esac
say ""
say "  Undo anything brewkeg changes with:  brewkeg reset"