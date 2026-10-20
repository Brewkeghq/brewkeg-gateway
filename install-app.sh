#!/bin/sh
# Brewkeg Gateway desktop installer — downloads the latest build for your OS,
# drops it in /Applications (or the Windows/Linux equivalent) and opens it.
#   curl -fsSL https://brewkeg.dev/install-app.sh | sh
#
# Why this exists instead of a download link: a browser stamps
# com.apple.quarantine on everything it fetches, and Gatekeeper then refuses to
# open the app with "Apple could not verify ... is free of malware". curl does
# not set that flag, so an app fetched this way opens with no dialog and no
# certificate. On Windows the same is true of the Zone.Identifier that makes
# SmartScreen show "Windows protected your PC".
#
# This is a wrapper, not a second implementation. install.sh knows how to
# install the app; this only asks it to. One copy of the logic, so the app
# install can never drift from the CLI install.
set -eu

REPO="${BREWKEG_REPO:-brewkeghq/brewkeg-gateway}"
SOURCE="https://raw.githubusercontent.com/${REPO}/main/install.sh"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "$SOURCE" -o "$tmp/install.sh" || {
  echo "error: could not fetch the installer from GitHub" >&2
  echo "       clone and run it directly: https://github.com/${REPO}" >&2
  exit 1
}

# --app-only: the desktop app, not the CLI. BREWKEG_OPEN_APP=1: open it when
# it lands, which is what this script has always done.
BREWKEG_OPEN_APP=1 sh "$tmp/install.sh" --app-only
