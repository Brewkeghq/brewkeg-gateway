# Brewkeg Gateway

Point your coding tools at brewkeg. Two binaries, one engine.

<img src="docs/gateway.png" width="420" alt="Brewkeg Gateway: paste a key, flip a switch per tool">

| | Install |
|---|---|
| `brewkeg` CLI | `curl -fsSL https://brewkeg.dev/install.sh \| sh` |
| Brewkeg Gateway (desktop app) | see below |

## Desktop app (Brewkeg Gateway)

**Do not download the `.zip` from a browser.** A browser stamps
`com.apple.quarantine` / `Zone.Identifier` on everything it fetches, and
Gatekeeper then refuses to open the app (*"Apple could not verify … is free of
malware"*) or SmartScreen does the same on Windows (*"Windows protected your
PC"*). `curl` sets neither flag, so an app fetched by the installer opens with
no dialog and no certificate.

```bash
curl -fsSL https://brewkeg.dev/install-app.sh | sh     # macOS + Linux
```

```powershell
irm https://brewkeg.dev/install-app.ps1 | iex          # Windows
```

It downloads the latest release for your OS, replaces any existing copy,
installs to `/Applications` (macOS) or `%LOCALAPPDATA%\Programs` (Windows),
clears the quarantine flag and starts the app.

The Windows install is per-user on purpose: it needs no admin rights, because an
app that edits your dotfiles has no business demanding elevation to paste an API
key.

Prefer a GUI? Download the zip, then run this once:

```bash
xattr -d com.apple.quarantine /Applications/brewkeg-gateway.app
```

Open, paste your key, flip a switch. Every toggle backs up first and can be
undone. Open `desktop/frontend/index.html?demo=1` to see the UI without
launching it.

## CLI

```bash
brewkeg setup              # pick tools, paste key
brewkeg setup --api-key bk_live_…
brewkeg status
brewkeg backups
brewkeg restore --dry-run
brewkeg restore
brewkeg reset                 # disconnect everything, clear caches, forget the key
```

## What it writes

| Tool | File |
|---|---|
| Claude Code | `~/.claude/settings.json` (`env`, merged) + exports in your shell rc |
| Codex | `~/.codex/config.toml` — `[model_providers.brewkeg]` |
| ZCode | `~/.zcode/v2/provider_config.json` |
| Claude Desktop | `~/Library/Application Support/Claude-3p/configLibrary/` |

Existing keys are merged, never replaced. A displaced value is commented out as
`# brewkeg replaced:`. Everything is backed up to `~/.brewkeg/backups/<id>/`
before the first write and restores byte-exact.

`brewkeg reset` (or **Brewkeg Gateway → Reset Brewkeg Gateway…**) returns the machine to its
pre-brewkeg state inside one backup, then forgets the key. The desktop menu asks
first and lists what it found.

Not supported: [docs/unsupported-targets.md](docs/unsupported-targets.md).

## "Could not verify … is free of malware" / "Windows protected your PC"

That is a **missing code signature**, not malware. Both desktop platforms block
unsigned downloads by default, and neither can be suppressed from inside the
app.

| OS | What you'll see | Why |
|---|---|---|
| macOS | *"Apple could not verify …"* | Gatekeeper. A browser puts the quarantine flag on anything it downloads; an unnotarised app cannot be opened. |
| Windows | *"Windows protected your PC"* | SmartScreen. Unsigned `.exe`, same shape. |
| Linux | nothing | No equivalent gate. |

The fix is a **Developer ID Application** certificate plus Apple notarisation
(macOS), and a CA-issued **Authenticode** certificate (Windows). CI is wired for
both — add these repository secrets and the next tag signs automatically:

| Secret | Used for |
|---|---|
| `MACOS_CERT_P12` | base64 `.p12` Developer ID Application export |
| `MACOS_CERT_PASSWORD` | its password |
| `APPLE_ID`, `APPLE_APP_PASSWORD`, `APPLE_TEAM_ID` | `notarytool` submission |
| `WINDOWS_CERT_PFX` | base64 Authenticode `.pfx` |
| `WINDOWS_CERT_PASSWORD` | its password |

Without them the build still succeeds and the artifacts are simply unsigned.

**Unblock the copy you already have:**

```bash
# macOS — right-click → Open also works, once per download
xattr -dr com.apple.quarantine /Applications/brewkeg-gateway.app

# Windows — Properties → Unblock at the bottom of the General tab
```

## Build

```bash
go vet ./... && go test ./...
cd desktop && wails build -clean
goreleaser check
```

---

© 2026 brewkeg. All rights reserved. Proprietary — see [LICENSE](LICENSE).
