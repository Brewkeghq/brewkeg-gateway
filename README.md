# brewkeg

Two binaries, one engine, one repo — `github.com/brewkeghq/brewkeg-cli`.

| Binary | Install | Use when |
|---|---|---|
| `brewkeg` (CLI) | `curl -fsSL https://brewkeg.dev/install.sh \| sh` | scripting, CI, or you live in a terminal |
| `brewkeg` (desktop app) | download the `.dmg` / `.zip` / `.tar.gz` from a release | one click: paste your key, flip the switches |

Both are the same `internal/brewkeg` engine, so they can never disagree about
what they write to your config files.

## The desktop app

Open it, paste your brewkeg API key, choose which tools to turn on, press the
button. It writes the config, prints a docket of every file it touched, and
keeps an undo. No terminal, no editing TOML.

```
Point your tools at brewkeg.          ← Instrument Serif italic, brand headline
┌ SERVICES ─────────────────── 2 selected ┐
│ 🅰 Claude Code        ~/.claude/settings.json   [on] │
│ Codex CLI                 ~/.codex/config.toml      [on] │
└──────────────────────────────────────────────────────────┘
┌ BACKUP 20261007-163657-565            [ BACKED UP ] ┐
│ ✓ ~/.claude/settings.json                    written │
│ ✓ Claude Desktop → Developer menu…           in app  │
└────────────────────────────────────────────────────────┘
```

The window is 600×580. It checks GitHub releases on open and shows a lime
banner when a newer version exists, then takes you to the release. It is a
notifier, not a silent self-install — the app never replaces its own executable
behind your back.

Build it:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
cd desktop && wails build -clean          # → desktop/build/bin/brewkeg.app
cd .. && wails dev -d desktop/frontend    # live reload during design work
```

Design review without launching the app: open
`desktop/frontend/index.html?demo=1` in a browser. Same HTML/CSS/JS, fake data.

The UI uses the product's own tokens (white, `#0f1115` ink, `#bef264` accent,
Geist + Geist Mono, Instrument Serif italic headline), with the woff2/ttf files
vendored into `desktop/frontend/fonts/` so it renders identically offline.

## The CLI

```bash
brewkeg setup                 # interactive: pick services, paste key
brewkeg setup --api-key bk_live_…
brewkeg status                # what is configured, where
brewkeg backups               # list every backup on this machine
brewkeg restore               # undo the most recent setup
brewkeg restore --dry-run     # show what would change, touch nothing
```

## Config comes from the server

The app does not hardcode what to write. On launch it fetches
`/api/client-config` from brewkeg.dev (generated from `web2/lib/client-config.ts`)
and applies whatever it finds: file paths, env var names, TOML keys, models,
labels and icons are all server data.

So when Codex changes how it stores its config, we edit that one file and every
installed app follows on its next launch — no release, no store review. Model
ids come from the same catalog that prices the billing page, so the spec cannot
drift from what we actually serve.

The **logic** stays in the app: how to merge JSON, how to edit a TOML table in
place, how to mark a block, how to back up and restore. The server says *what*,
the app decides *how*, safely. Offline or on an unreachable gateway the built-in
spec is used and setup works exactly as before. Apps also refuse a spec older
than the one they ship with, so rolling the server file back cannot walk an
installed app backwards.

## What it writes

| Target | File |
|---|---|
| Claude Code CLI | `~/.claude/settings.json` (`env` block, merged — other keys preserved) + exports in your shell rc, inside a marked block |
| Codex CLI | `~/.codex/config.toml` — `[model_providers.brewkeg]` table plus the root `model_provider` key, both marked |
| Claude Desktop | Nothing. Its gateway lives in the Developer menu, so both front-ends print the exact steps instead of guessing at a file it does not own. |

A pre-existing `model_provider = "openai"` is commented out rather than deleted,
so the change is visible and reversible.

Every target above is a file we read, merge into surgically, back up first, and can
restore byte-exact. Tools that don't offer that surface are not written to — see
[docs/unsupported-targets.md](docs/unsupported-targets.md) for why Cursor and
Antigravity are not targets, and what would unblock each.

## Restore is the contract

Before the first write, every touched file is copied to
`~/.brewkeg/backups/<id>/` alongside a `manifest.json`. That copy is stored with
brewkeg's own footprint already stripped out, so **any** backup is a true
pre-brewkeg state — running setup twice and restoring once still gets you back
to before you ever ran it, not back to the previous run's key.

On restore:

- a file that existed before is written back byte-for-byte
- a file brewkeg created is deleted if nothing else is in it, otherwise only
  brewkeg's marked block is removed

## Build and test

```bash
go vet ./... && go test ./...     # engine + CLI + desktop bindings
go build -o brewkeg .             # CLI
cd desktop && wails build -clean  # desktop app
goreleaser check                  # validate the CLI release config
```

CI on tag `cli-v*` (`.github/workflows/cli-release.yml`): GoReleaser publishes the
CLI binaries, then a per-OS matrix builds the desktop app with Wails and attaches
the packaged app to the same release.