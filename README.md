# brewkeg CLI

One command to point Claude Code, Codex CLI and Claude Desktop at brewkeg.

```bash
curl -fsSL https://brewkeg.dev/install.sh | sh
brewkeg setup
```

Single static binary. No Node, no Python, no npm, no runtime dependencies.
Go stdlib only — see `go.mod`.

## Commands

| Command | What it does |
|---|---|
| `brewkeg setup` | Interactive. Pick services, paste key, done. |
| `brewkeg setup --api-key bk_live_…` | Non-interactive. |
| `brewkeg status` | What is configured, where. |
| `brewkeg backups` | List every backup on this machine. |
| `brewkeg restore` | Undo the most recent setup. |
| `brewkeg restore --backup <id>` | Undo a specific one. |
| `brewkeg restore --dry-run` | Show what would change, touch nothing. |

Flags: `--base-url`, `--model`, `--fast-model`, `--backup`, `--dry-run`.

## What it writes

| Target | File |
|---|---|
| Claude Code CLI | `~/.claude/settings.json` (`env` block, merged — other keys preserved) + exports in your shell rc, inside a marked block |
| Codex CLI | `~/.codex/config.toml` — `[model_providers.brewkeg]` table plus the root `model_provider` key, both marked |
| Claude Desktop | Nothing. Its gateway lives in the Developer menu, so the CLI prints the exact steps instead of guessing at a file it does not own. |

Anything already there is preserved. A pre-existing
`model_provider = "openai"` is commented out rather than deleted, so the change
is visible and reversible.

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

## Build

```bash
go build -o brewkeg .       # local build
goreleaser check            # validate release config
goreleaser release --snapshot --clean
```

Releases publish `brewkeg_<version>_<os>_<arch>` binaries for
darwin/linux/windows × amd64/arm64, plus a checksum file the installer verifies.
See `.goreleaser.yaml`.