# Tools we deliberately don't write to

Every target in [`internal/brewkeg`](../internal/brewkeg) is a file we own, read, merge
into surgically, back up first, and can restore byte-exact. That is the whole promise.
Some tools don't offer that surface at all, and the honest move is to say so rather
than write into a store we don't control.

This file records those tools and **why**, so the next person doesn't re-research it.

Reviewed: 2026-10-07.

---

## Cursor

**Verdict: deferred. Not blocked — waiting on a tested normalizer.**

Cursor does support BYOK. It is a real, documented surface. The problem is that it
isn't a file.

### Why not automatic

Cursor stores its AI settings in an account-synced store, not on disk in a form we
can merge. Vercel's AI Gateway docs are explicit about it:

> Cursor keeps API-key settings in its account-synced store instead of a file on
> disk, so you must finish the setup in Cursor.

On disk, what exists is `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`
(a SQLite database owned by a running Electron process). The key lands under a row
keyed `cursorAuth/openAIKey`; the base URL and its enable flag live inside a large
JSON blob in the same database. The values are hex-encoded, not encrypted — which
does not make it ours to write.

Every gateway vendor that supports Cursor (Vercel, Modular, MiniMax, LangWatch,
Superagent, bettertoken) ships printed steps. None of them write the database.

### Three traps, if we ever ship it

1. **Model names beginning `claude-` route to the wrong key slot.** Cursor picks
   which saved key to send based on the *start of the model name*. A model named
   `claude-…` uses the **Anthropic** slot, not the OpenAI one — so with only an
   OpenAI key saved, the request goes out with no credential and Cursor reports
   *"Model name is not valid"*. Our entire catalog is `claude-*`.
   **Consequence: Cursor-facing ids must be aliases that do not begin with
   `claude-`** (e.g. `bk-sonnet-5`, `bk-opus-5`), not catalog ids.
2. **`Override OpenAI Base URL` is global, not per-model.** Turning it on breaks
   Cursor's own Composer and Grok models with *"This model does not support custom
   API keys"* — Cursor refuses to serve its own models with a user key attached.
   Cursor staff have acknowledged both the global behaviour and the per-model
   feature request. Enabling it is a trade we should make explicit to the user,
   not something to do silently on their behalf.
3. **Cursor sends request bodies our endpoints may not accept.** Older builds
   (`3.14.x`) send flat tool definitions whose `type` is not `function`; newer
   builds send Responses-shaped bodies with `input` instead of `messages`. Vercel
   built a dedicated `/cursor/v1` normalizer because pointing Cursor at a bare
   `/v1` returns `400 on tools.0.type`. We serve `/v1/responses`, but it has never
   been tested against a real Cursor client.

Also worth knowing when we do ship: **Tab / autocomplete never uses the custom key**
— it stays on Cursor's own infrastructure. And BYOK traffic still transits Cursor's
servers.

### What would unblock it

- A real Cursor install to test our `/v1/responses` against, and to confirm whether
  the `claude-` prefix trap actually fires against our ids (it is a forum report,
  not something we have reproduced).
- The alias-id naming layer from trap 1.
- A decision on trap 2's trade-off, and copy that warns about it.

---

## Antigravity

**Verdict: not supported. There is no surface for us to write.**

### The IDE has no BYOK hook

Google Antigravity's IDE offers no official bring-your-own-key or custom-provider
option for its agent model. The only thing resembling one is
`~/.antigravity/settings.json` with `antigravity.ai.provider` /
`antigravity.openai-compatible.*` keys — and the thread that proposes them reports
verbatim that **it does not work**: the IDE either fails to route to the custom
endpoint or falls back to default, with no model picker to drive it.

We could not corroborate the keys against an installed app (none on the machine
that did this review). `~/.antigravity/argv.json` does confirm Antigravity is a
VS Code fork, so it is not reading some undocumented store we'd have missed.

Everything that *does* add custom models to Antigravity is a third-party patch that
reverse-engineers Google's internal Cloud Code API (`v1internal:fetchAvailableModels`,
`v1internal:streamGenerateContent`) and **repacks the app binary** to inject a local
proxy. Shipping that is not a config edit; it is a fork of someone else's signed
application. Out of scope by a wide margin.

### The Antigravity CLI has an official hook we don't speak

`agy` (Antigravity CLI) *does* support a custom endpoint officially, via
`~/.gemini/antigravity-cli/settings.json` with `"modelProvider": "gemini"` plus the
`GEMINI_API_KEY` and `GOOGLE_GEMINI_BASE_URL` environment variables.

It is **Gemini-protocol only**. The official troubleshooting table lists `gemini` as
the only accepted `modelProvider` value, so this route cannot take an
OpenAI-compatible endpoint.

**We do not serve the Gemini protocol.** Our routes are:

```
/v1/messages              (Anthropic format)
/v1/messages/count_tokens
/v1/responses
/v1/models
/health · /statusline · /internal/routes
```

See [`proxy/internal/server/server.go`](../../proxy/internal/server/server.go). The
string `gemini` in our codebase is an **upstream we translate *to*** — we accept
Anthropic-format requests and reshape them for a Gemini-family pool. That is the
opposite direction from what `GOOGLE_GEMINI_BASE_URL` needs, which wants to *call*
`…/v1beta/models/<model>:generateContent`.

So the Antigravity CLI path is blocked on a missing endpoint, not a missing feature
in our engine. Adding a Gemini-native `/v1beta` surface to the proxy would unblock
it — that is `proxy/` work, which the parent repo's `CLAUDE.md` forbids without
explicit authorization.

### What would unblock it

- A Gemini-native surface on the proxy (`generateContent` + `streamGenerateContent`).
  Not a client-tooling change; a gateway protocol addition.
- The Antigravity CLI actually installed, to test against.

---

## Summary

| Tool | Surface | Verdict |
|---|---|---|
| Cursor | account-synced SQLite, GUI-only | Deferred — needs a tested request normalizer + non-`claude-` alias ids |
| Antigravity (IDE) | none | Not supported — no official hook; only binary-repacking patches exist |
| Antigravity (CLI) | env vars, Gemini-protocol only | Blocked — we don't serve Gemini; needs a `/v1beta` surface on the proxy |

If you are reading this because a user asked for one of these: the honest answer is
the row above, not a config edit we cannot safely make.