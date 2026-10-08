# CLAUDE.md

Guidance for Claude Code when working in **this repo** — the standalone
`brewkeg-gateway` CLI. This file lives here, not in the parent `reclaude`
checkout, because this directory is its own git repository and the parent
`.gitignore`s it. The parent has its own `CLAUDE.md` covering the web edge and
the Go proxy; nothing here is inherited from it.

## What this is

One Go engine, two binaries, one repo (`github.com/brewkeghq/brewkeg-gateway`):

| Binary | Name | Purpose |
|---|---|---|
| `brewkeg` | terminal CLI | `setup`, `restore`, `backups`, `status`, `uninstall` |
| `gateway` | Wails desktop app | the same engine with a window; also `gateway --apply` / `--undo` headless |

They share `internal/brewkeg/`, so the window and the script can never disagree
about what gets written to a config file. `TestHeadlessAndWindowProduceIdenticalConfig`
is the guard on that — keep it passing.

The app is **Brewkeg Gateway** in the UI — window title, menu bar, header. The
bundle and binary stay `gateway`/`gateway.app`: the artifact names and the
`--apply` flags are part of the shipped contract
tile. The binary stays `brewkeg`, because renaming it breaks `install.sh` and
every existing install.

## The one rule

**A user's existing config is never replaced wholesale.** Only brewkeg-owned
keys are found and edited in place, every touched file is copied to
`~/.brewkeg/backups/<id>/` before the first write, and restore must be
byte-exact.

That rule is not a slogan — it is what the tests are for:

- `TestBlockStripIsByteExact`, `TestEvictRoundTrips*` — apply → evict returns
  the *byte-identical* original. Unit-testing the helpers alone passes; only the
  round trip catches the real bugs, and it has caught several.
- A "surgical merge" means the user's tables, MCP servers, comments and unrelated
  env vars survive. If a write reformats a file beyond what the edit needs,
  that is a bug.

## Server owns the data, this repo owns the logic

The app does **not** hardcode what to write. On launch it fetches
`/api/client-config` (from `brewkeg.dev`) and applies whatever it finds:
file paths, env var names, TOML keys, model ids, labels and icons are all
server data, defined in the parent repo at `web2/lib/client-config.ts`.

So a new tool is a spec edit plus an engine change, not an app release. And when
a tool changes how it stores its config, we edit that one server file and every
installed app follows on its next launch.

An app refuses a spec **older** than the one it shipped with, so rolling the
server file back cannot walk an installed app backwards.

## Commands

```bash
go vet ./... && go test ./...     # vet + test all three packages
make build                         # CLI binary for this machine
make desktop                       # -> desktop/build/bin/gateway.app
```

Build the desktop app with `make desktop`, never with bare `wails build` — the
Makefile stages `assets/appicon.png` first. If Wails reports success but
`gateway.app/Contents/MacOS/` is empty, delete `desktop/build/bin` and rebuild;
that race has bitten twice and is not a code fault.

## Layout

- `internal/brewkeg/` — the engine. One file per concern:
  `paths` `block` `backup` `targets` `spec` `validate` `picker` `keystore`
  `userconfig` `claudedesktop` `zcode` `relaunch` `remove`
- `main.go` `setup.go` `restore.go` `prompt.go` — the CLI
- `desktop/` — `app.go` (bindings) `headless.go` (`--apply` / `--undo`)
  `update.go`, `main.go`, `frontend/`
- `docs/unsupported-targets.md` — why Cursor and Antigravity are **not**
  targets. Read it before adding a tool: most of them have no file we own.

## Adding a tool

1. Find out where it actually stores its config, from a **real install** —
   never from a forum post or a doc that contradicts one. Read the app bundle
   if the format is undocumented. See `docs/unsupported-targets.md` for what
   "no file we own" looks like, and `claudedesktop.go` for what reverse
   engineering an Electron app actually yields.
2. If there is no readable file, do not ship a target that guesses one. Print
   the steps instead.
3. Add a `kind` if the existing editors cannot express the shape. `zcode-provider`
   exists because ZCode's registry is two nested arrays of rule objects that no
   flat key merge can touch.
4. Write both halves: `Apply` **and** `removeTarget`. Turning a tool off has to
   take brewkeg back out; a target that can be connected but not disconnected is
   a bug, and the "empty selection" button is how a user finds it.
5. Detect state from the tool's own config, never from our marker comments.
   Someone who wired brewkeg in by hand must see the same switch state as
   someone who used the app.
6. Tests with a temp `HOME`. **Never touch the developer's real apps** — the
   `BREWKEG_NO_RELAUNCH` guard lives in the engine, not at the call site.

## Rules this repo lives by

1. **Never commit or push.** The user does that. No branches, no tags, no
   releases without being asked.
2. **Never expose a credential in output.** Not in a log line, not in a docket,
   not in a test fixture. Keys live in `~/.brewkeg/key` at 0600 inside an
   owner-only directory — `MkdirAll` alone does not tighten an existing
   `~/.brewkeg`, so `EnsurePrivateDir` chmods as well.
3. **A store must not read from one of its own outputs.** The remembered key
   comes from `~/.brewkeg/key`, not from `~/.claude/settings.json`; reading the
   latter broke for anyone who only ever configured Codex.
4. **Only a definitive rejection blocks a write.** `Blocked()` means the service
   saw the input and refused it. Offline, a 5xx, or an unrecognised 4xx are not
   auth verdicts — proceeding loudly with a warning is the correct behaviour, and
   blocking there locks out exactly the user who is offline.
5. **Preserve JSON types.** `"allowDevTools": "true"` is a string to Electron
   and fails silently; `inferenceModels` must be an array,
   `modelDiscoveryEnabled` a boolean.
6. **Restore in place.** When evicting a value we commented out, substitute it
   where the comment sat. Re-appending it near an anchor silently reflows a
   file the user never agreed to change.
7. **Refuse to write when the result would not parse.** A config that cannot be
   parsed is a tool the user cannot start — worth more than the convenience.

## Things that will bite you

- **`pkill` is never allowed to ask whether a process is running.** Use
  `pgrep`/`osascript 'application id "…" is running'`. An earlier version used
  `pkill` for detection, which meant asking "are you open?" killed the app.
- **Restart only what we finished.** A target that returns manual steps is not
  done, and bouncing its app reads as "it restarted but nothing happened".
- **Only restart an app that was already running.** Launching something the user
  deliberately closed is not a restart.
- **Not every path in one app follows the same rule.** Claude Desktop reads
  `configLibrary/` from `userData + "-3p"` but `developer_settings.json` from
  plain `userData`. Assuming one rule for both produces a bug that looks like a
  fix.
- **Reverse-engineered strings may be composed at runtime.** Grepping Claude
  Desktop's bundle for `"Claude-3p"` returns zero hits; it is `userData + "-3p"`.
  A zero-hit grep is evidence the string is built, not evidence it is invented.
- **Bugs here are silent by nature.** A wrong key name in a tool's config is
  accepted by the file and ignored by the tool. Mutation-check any guard test:
  neuter the thing it protects and confirm it goes red. A test that passes on a
  broken implementation is worse than no test.