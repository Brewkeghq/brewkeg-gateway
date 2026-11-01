#!/bin/sh
# brewkeg installer — the CLI and the Gateway desktop app, one command.
#   curl -fsSL https://brewkeg.dev/install.sh | sh
#
# Env:
#   BREWKEG_INSTALL_DIR   where the CLI binary goes (default ~/.local/bin)
#   BREWKEG_APP_DIR       where the app goes (default /Applications on macOS)
#   BREWKEG_MANIFEST_URL  override the manifest URL (default: the GitHub
#                         release "latest" redirect, no API call)
#   BREWKEG_REPO          override the GitHub repo (owner/name)
#   BREWKEG_SKIP_APP=1    install the CLI only
#   BREWKEG_OPEN_APP=1    open the app after installing it
#
# Flags: --no-app (CLI only), --app-only (app only), --help
set -eu

REPO="${BREWKEG_REPO:-brewkeghq/brewkeg-gateway}"
INSTALL_DIR="${BREWKEG_INSTALL_DIR:-$HOME/.local/bin}"
APP_NAME=brewkeg-gateway
MANIFEST_URL="${BREWKEG_MANIFEST_URL:-https://github.com/${REPO}/releases/latest/download/latest.txt}"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# Every fetch carries a deadline. Without one, a connection that opens and then
# goes quiet hangs the installer forever: no output, no error, nothing to press
# — and `curl -fsSL` alone has no timeout of any kind. A stalled manifest or
# API call now gives up in a minute and the installer moves on; the binaries
# are allowed longer because they are megabytes, not kilobytes.
#
# `--retry` covers the transient failures (timeouts, 5xx, 429) that a flaky
# connection produces. It does not cover 404 — the app archive is attached by a
# job that runs after this one, so a 404 there must stay a fast, quiet "not yet".
fetch()    { curl -fsSL --connect-timeout 10 --max-time 60  --retry 2 --retry-delay 1 "$@"; }
download() { curl -fsSL --connect-timeout 10 --max-time 300 --retry 2 --retry-delay 1 "$@"; }

want_cli=1
want_app=1
for arg in "$@"; do
  case "$arg" in
    --no-app|--cli-only) want_app=0 ;;
    --app-only) want_cli=0; want_app=1 ;;
    -h|--help)
      say "brewkeg installer"
      say "  (no flags)         install the CLI and the Gateway desktop app"
      say "  --no-app           install the CLI only"
      say "  --app-only         install the desktop app only"
      exit 0
      ;;
    *) die "unknown option: $arg" ;;
  esac
done
[ "${BREWKEG_SKIP_APP:-0}" = "1" ] && want_app=0

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

case "$os" in
  darwin) app_asset_os=macos; app_ext=zip ;;
  linux)  app_asset_os=linux; app_ext=tar.gz ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# ── resolve the release ─────────────────────────────────────────────────────
# The manifest is the source of truth: it names the exact CLI asset per
# platform. The installer used to DERIVE that name, and the derivation was
# wrong, so every install 404'd on a file that did exist. Reading the name
# beats computing it.
#
# The fallback exists for releases published before the manifest did. It
# costs one GitHub API call, which is anonymous and rate-limited — fine as a
# fallback, not fine as the main path.
version=""
tag=""
asset=""
if manifest=$(fetch "$MANIFEST_URL" 2>/dev/null); then
  version=$(printf '%s\n' "$manifest" | sed -n 's/^version=//p' | head -n1)
  tag=$(printf '%s\n' "$manifest" | sed -n 's/^tag=//p' | head -n1)
  asset=$(printf '%s\n' "$manifest" | sed -n "s/^asset_${os}_${arch}=//p" | head -n1)
fi

if [ -z "$version" ] || [ -z "$asset" ]; then
  say "→ no install manifest, falling back to the GitHub API"
  tag=$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$tag" ] || die "could not read the latest release from GitHub"
  version=$(printf '%s' "$tag" | sed 's/^cli-//; s/^v//')
  [ -n "$version" ] || die "could not read a version from the tag $tag"
  # GoReleaser's `{{ .Os }}` is lowercase: the asset is brewkeg_0.2.2_darwin_arm64.
  asset="brewkeg_${version}_${os}_${arch}"
fi

base="https://github.com/${REPO}/releases/download/${tag}"
sums="$base/brewkeg_${version}_checksums.txt"

# One fetch, used by both the CLI and the app.
have_sums=0
if fetch "$sums" -o "$tmp/sums" 2>/dev/null && [ -s "$tmp/sums" ]; then
  have_sums=1
fi

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# ── the CLI ────────────────────────────────────────────────────────────────
if [ "$want_cli" = 1 ]; then
  say "→ fetching brewkeg ${version} for ${os}/${arch}"
  download "$base/$asset" -o "$tmp/brewkeg" || die "download failed: $base/$asset"
  chmod +x "$tmp/brewkeg"

  if [ "$have_sums" = 1 ]; then
    say "→ verifying checksum"
    expected=$(awk -v f="$asset" '$2 == f || $2 == "*"f {print $1}' "$tmp/sums" | head -n1)
    [ -n "$expected" ] || die "no checksum for $asset in the release"
    [ "$expected" = "$(sha256_of "$tmp/brewkeg")" ] || die "checksum mismatch — refusing to install"
    say "  checksum ok"
  else
    say "  (no checksum file published for this release, skipping verification)"
  fi

  mkdir -p "$INSTALL_DIR"
  say "→ installing to $INSTALL_DIR/brewkeg"
  mv "$tmp/brewkeg" "$INSTALL_DIR/brewkeg"
fi

# ── the desktop app ────────────────────────────────────────────────────────
# The app asset is named from the tag, so it is known without the manifest.
# The desktop job attaches it to the release AFTER the CLI job, so a release
# can exist without it — a 404 here must warn, never abort, or the CLI install
# that already succeeded would be reported as a failure.
app_asset="${APP_NAME}_${tag}_${app_asset_os}.${app_ext}"
app_installed=0
if [ "$want_app" = 1 ]; then
  say ""
  say "→ fetching $APP_NAME ${version} for ${os}"
  if download "$base/$app_asset" -o "$tmp/app.$app_ext" 2>/dev/null; then
    # The app's checksum lives beside it, published by the desktop job. The
    # release's checksums.txt cannot carry it: three runners would race to
    # append to it.
    if fetch "$base/$app_asset.sha256" -o "$tmp/app.sha256" 2>/dev/null && [ -s "$tmp/app.sha256" ]; then
      expected=$(awk '{print $1}' "$tmp/app.sha256" | head -n1)
      [ -n "$expected" ] || die "empty checksum for $app_asset"
      [ "$expected" = "$(sha256_of "$tmp/app.$app_ext")" ] || die "checksum mismatch on $app_asset — refusing to install"
      say "  checksum ok"
    else
      say "  (no checksum published for the app in this release, skipping verification)"
    fi

    say "→ unpacking"
    case "$app_ext" in
      zip)    ditto -x -k "$tmp/app.$app_ext" "$tmp/unpacked" ;;
      tar.gz) mkdir -p "$tmp/unpacked" && tar xzf "$tmp/app.$app_ext" -C "$tmp/unpacked" ;;
    esac

    app="$tmp/unpacked/$APP_NAME.app"
    [ -d "$app" ] || app="$tmp/unpacked/$APP_NAME"   # Linux ships a bare binary
    [ -e "$app" ] || die "unexpected archive contents: no $APP_NAME found"

    # Strip the quarantine flag so Gatekeeper/SmartScreen never engages.
    # Without -r on purpose: it recurses into the bundle and fails on files
    # that carry no such attribute, which aborts the whole install.
    if [ "$os" = darwin ]; then
      xattr -d com.apple.quarantine "$app" 2>/dev/null || true
      find "$app" -name '._*' -delete 2>/dev/null || true
    fi

    if [ "$os" = darwin ]; then
      app_dir="${BREWKEG_APP_DIR:-/Applications}"
      # /Applications needs a writable session on every macOS user account, but
      # a locked-down or managed machine can deny it. Falling back to
      # ~/Applications beats aborting over permissions.
      if [ ! -w "$app_dir" ] && [ -z "${BREWKEG_APP_DIR:-}" ]; then
        say "→ $app_dir is not writable, using $HOME/Applications instead"
        app_dir="$HOME/Applications"
      fi
    else
      app_dir="$HOME/.local/share/brewkeg"
    fi

    # Replace rather than merge. Merging an old bundle into a new one leaves
    # stale Resources behind, and the user ends up with a build whose front
    # end is half old and half new.
    if [ -e "$app_dir/$APP_NAME.app" ] || [ -e "$app_dir/$APP_NAME" ]; then
      say "→ replacing the existing copy in $app_dir"
      rm -rf "${app_dir:?}/$APP_NAME.app" "${app_dir:?}/$APP_NAME"
    fi
    mkdir -p "$app_dir"

    say "→ installing to $app_dir"
    if [ "$os" = darwin ]; then
      mv "$app" "$app_dir/$APP_NAME.app"
    else
      mkdir -p "$app_dir/$APP_NAME"
      mv "$app" "$app_dir/$APP_NAME/$APP_NAME"
      chmod +x "$app_dir/$APP_NAME/$APP_NAME"
    fi
    app_installed=1
  else
    say "  (no $APP_NAME build for this release yet — the CLI is installed, the app was not)"
  fi
fi

# ── wrap up ────────────────────────────────────────────────────────────────
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "  $INSTALL_DIR is not on your PATH. Add this to your shell rc:"
    say "    export PATH=\"\$HOME/.local/bin:\$PATH\""
    ;;
esac

if [ "$want_cli" = 1 ]; then
  say ""
  "$INSTALL_DIR/brewkeg" version
fi

say ""
if [ "$app_installed" = 1 ]; then
  case "$os" in
    darwin)
      say "  Gateway is installed. Open it from your Applications folder."
      say "  macOS will ask for permission to edit files in your home folder the"
      say "  first time you flip a switch. That is the app doing its job."
      ;;
    linux)
      say "  Gateway is installed to $HOME/.local/share/brewkeg/$APP_NAME/$APP_NAME"
      ;;
  esac
fi
say "  Next:  brewkeg setup"
say "  Get your key at ${BREWKEG_BASE_URL:-https://brewkeg.dev}/dashboard"
say "  Undo anything brewkeg changes with:  brewkeg reset"
say ""

if [ "${BREWKEG_OPEN_APP:-0}" = "1" ] && [ "$app_installed" = 1 ]; then
  case "$os" in
    darwin) open "$app_dir/$APP_NAME.app" ;;
    linux)  "$app_dir/$APP_NAME/$APP_NAME" & ;;
  esac
fi
