// brewkeg desktop — window logic. Wails injects window.go.main.App, so bindings
// are called directly: no bundler, no build step.
//
// ?demo=1 renders the same window with fake data, so the design can be reviewed
// in a plain browser without launching the app.
const real = window.go?.main?.App;
const demo = !real && new URLSearchParams(location.search).has("demo");
const api = real ?? {
  GetState: async () => DEMO_STATE,
  RefreshSpec: async () => DEMO_STATE.spec || { version: 1, targets: [] },
  CheckKey: async () => ({ ok: true, valid: true, status: 200, message: "Key works — claude-haiku-4-5 in 412ms.", latencyMs: 412 }),
  CheckForUpdate: async () => ({ available: true, current: "0.1.0", latest: "0.2.0", url: "#", notes: "Adds OpenCode" }),
  OpenUpdate: () => {},
  CopyToClipboard: () => {},
  OpenRepo: () => {},
  Configure: async () => DEMO_RESULT,
  Restore: async () => "~/.claude/settings.json — restored original\n~/.zshrc — removed",
  OpenDashboard: () => {},
  RevealBackupDir: () => {},
};

const ICONS = {
  "claude-cli": "icons/anthropic.svg",
  desktop: "icons/anthropic.svg",
  codex: "icons/openai.svg",
};

const DEMO_STATE = {
  version: "0.1.0",
  baseUrl: "https://brewkeg.dev",
  maskedKey: "bk_live_****ac41",
  apiKey: "bk_live_demo_9f3ac1c07d2e4b5680ac41",
  targets: [
    { id: "claude-cli", label: "Claude Code", installed: true, enabled: false, path: "~/.claude/settings.json" },
    { id: "codex", label: "Codex CLI", installed: true, enabled: true, path: "~/.codex/config.toml" },
    { id: "desktop", label: "Claude Desktop", installed: false, enabled: false, path: "in-app" },
  ],
};

const DEMO_RESULT = {
  ok: true,
  backupId: "20261007-163657",
  message: "Connected 2 tools.",
  restart: [
    { what: "Claude Code", action: "quit and reopen" },
    { what: "Claude Desktop", action: "quit and reopen" },
    { what: "Your terminal", action: "open a new window" },
  ],
  relaunched: [{ app: "Codex desktop", action: "restarted", detail: "bundle com.openai.codex" }],
  results: [
    { id: "claude-cli", label: "Claude Code", ok: true, paths: ["~/.claude/settings.json", "~/.zshrc"], manual: "" },
    { id: "desktop", label: "Claude Desktop", ok: true, paths: [], manual: "Developer → Configure Third-Party Inference…\nBase URL: https://brewkeg.dev" },
  ],
};

const el = (id) => document.getElementById(id);
let state = null;
let restartHints = [];

boot().catch((e) => say(`Could not start: ${e}`, true));

async function boot() {
  state = await api.GetState();
  el("version").textContent = state.version;
  if (state.staleCache) el("promise").textContent = "Will clear the cached model list.";
  render();
  checkUpdate();
  el("get-key").addEventListener("click", () => api.OpenDashboard());
  el("peek").addEventListener("click", togglePeek);
  el("undo").addEventListener("click", undo);
  el("apply").addEventListener("click", apply);
  el("check").addEventListener("click", check);
  el("apikey").addEventListener("input", () => hideCheck());
  el("update").addEventListener("click", () => api.OpenUpdate(el("update").dataset.url));
  el("repo").addEventListener("click", () => api.OpenRepo());
  el("copy-restart").addEventListener("click", copyRestart);
  el("get-key").title = `Get your API key at ${state.baseUrl}/dashboard`;
  refreshSpec();
  if (state.apiKey) {
    // Reopening should never cost you the key you already gave us.
    el("apikey").value = state.apiKey;
  }
  if (state.maskedKey) {
    el("key-label").textContent = "Using";
    el("key-note").insertAdjacentHTML("beforeend", ` <code>${state.maskedKey}</code>`);
  }
}

// The gateway decides what to write for each tool, so a change to how Codex or
// Claude Code stores its config reaches this window without a new release. The
// window already renders from the built-in spec, so this only ever upgrades it.
async function refreshSpec() {
  try {
    const spec = await api.RefreshSpec();
    if (!spec || !spec.targets || !spec.targets.length) return;
    if ((spec.version ?? 0) <= (state.specVersion ?? 0)) return;
    state = { ...state, specVersion: spec.version, targets: spec.targets.map((t) => ({
      id: t.id, label: t.label, icon: t.icon, note: t.note,
      installed: true, enabled: t.enabled?.neverDetectable ? false : !!t.enabled,
      path: (t.files?.[0]?.path || "").replace(/^~\//, "~/"),
    })) };
    render();
  } catch {
    /* offline: the built-in spec is already correct */
  }
}

async function checkUpdate() {
  try {
    const u = await api.CheckForUpdate();
    if (!u.available) return;
    const b = el("update");
    b.dataset.url = u.url;
    b.hidden = false;
    el("update-text").textContent = u.notes ? `Update ${u.latest} — ${u.notes}` : `Update ${u.latest} available`;
  } catch {
    /* no network, no banner — a failed check must never block setup */
  }
}

// A key is tested against the live gateway before anything is written, so a
// typo never reaches the user's config files.
async function check() {
  const key = el("apikey").value.trim();
  if (!key) {
    showCheck("Paste your API key first.", "bad");
    return;
  }
  const line = showCheck("Testing…", "testing");
  el("check").disabled = true;
  try {
    const r = await api.CheckKey(key);
    line.textContent = r.message;
    // Only a rejection locks the button. Out of quota is still the right key,
    // and "could not reach brewkeg" is not evidence about the key at all.
    const bad = r.rejected;
    line.className = "checkline" + (bad ? " bad" : r.ok ? "" : " warn");
    updateButton();
    // A rejected key locks the button outright — there is nothing to run until
    // a different key is entered.
    if (bad) el("apply").disabled = true;
    el("apikey").dataset.verdict = bad ? "rejected" : r.reachable ? "ok" : "unknown";
  } catch (e) {
    showCheck(`Test failed: ${e}`, "bad");
  } finally {
    el("check").disabled = false;
  }
}

function showCheck(text, cls) {
  const line = el("checkline");
  line.hidden = false;
  line.textContent = text;
  line.className = "checkline" + (cls ? " " + cls : "");
  return line;
}

function hideCheck() {
  el("checkline").hidden = true;
  updateButton();
}

function togglePeek() {
  const input = el("apikey");
  const showing = input.type === "text";
  input.type = showing ? "password" : "text";
  el("peek").textContent = showing ? "Show" : "Hide";
}

function render() {
  const list = el("services");
  list.innerHTML = "";
  for (const t of state.targets) {
    const li = document.createElement("li");

    const row = document.createElement("div");
    row.className = "service";
    row.setAttribute("role", "switch");
    row.setAttribute("aria-selected", String(!!t.enabled));
    row.tabIndex = 0;

    const icon = document.createElement("img");
    icon.className = "service-icon";
    icon.src = ICONS[t.id] ?? "";
    icon.alt = "";
    if (!ICONS[t.id]) icon.hidden = true;

    const name = document.createElement("div");
    name.className = "service-name";
    name.textContent = t.label;

    const sw = document.createElement("div");
    sw.className = "switch";
    sw.id = `sw-${t.id}`;
    sw.setAttribute("role", "switch");
    sw.setAttribute("aria-checked", String(t.enabled));
    sw.setAttribute("aria-label", t.label);

    // No config path here on purpose: it is noise in a "what am I connecting"
    // list, it leaked the absolute home directory, and the docket after a
    // successful run is where a path actually belongs.
    row.append(icon, name, sw);
    row.addEventListener("click", () => toggle(t.id));
    row.addEventListener("keydown", (e) => {
      if (e.key === " " || e.key === "Enter") {
        e.preventDefault();
        toggle(t.id);
      }
    });
    li.appendChild(row);
    list.appendChild(li);
  }
  updateButton();
}

function toggle(id) {
  const sw = el(`sw-${id}`);
  const on = sw.getAttribute("aria-checked") !== "true";
  sw.setAttribute("aria-checked", String(on));
  sw.closest(".service")?.setAttribute("aria-selected", String(on));
  updateButton();
}

function selected() {
  return state.targets
    .filter((t) => el(`sw-${t.id}`).getAttribute("aria-checked") === "true")
    .map((t) => t.id);
}

// connected is what the machine actually has right now, captured before the
// user touches anything. The button is enabled by *difference* from this, not
// by how many switches are on: "untoggle everything" is a real, useful action,
// and a button that disables itself the moment the last switch goes off makes
// disconnecting the one thing this app cannot do.
function connected() {
  return state.targets.filter((t) => t.enabled).map((t) => t.id);
}

function pendingRemoval() {
  const on = new Set(connected());
  return state.targets.filter((t) => on.has(t.id) && !selected().includes(t.id));
}

function updateButton() {
  const sel = selected();
  const dropping = pendingRemoval();
  const btn = el("apply");
  const dirty = sel.length !== connected().length || dropping.length > 0;

  if (sel.length === 0 && dropping.length > 0) {
    btn.textContent = `Disconnect ${dropping.length} tool${dropping.length === 1 ? "" : "s"}`;
  } else if (sel.length === 0) {
    btn.textContent = "Connect";
  } else {
    btn.textContent = `Connect ${sel.length} tool${sel.length === 1 ? "" : "s"}`;
  }
  btn.disabled = !dirty;
}

async function apply() {
  const key = el("apikey").value.trim();
  // Disconnecting is the one action that carries no key: once the tools are
  // pointed away from us, nothing is left to authenticate. Demanding a secret
  // to undo our own edits would lock out exactly the user whose key expired.
  const disconnectOnly = selected().length === 0 && pendingRemoval().length > 0;
  if (!key && !disconnectOnly) {
    say("Paste your API key.", true);
    el("apikey").focus();
    return;
  }
  const btn = el("apply");
  btn.disabled = true;
  try {
    // The Go side re-checks and refuses a rejected key, so a stale verdict here
    // (or no verdict at all) can never write a bad key into a config.
    const res = await api.Configure(key, selected(), state.baseUrl, "", "", "");
    if (!res.ok && !res.results?.length) {
      showCheck(res.message, res.key?.rejected ? "bad" : "warn");
      return;
    }
    showDocket(res);
    if (res.warning) say(res.warning);
  } catch (e) {
    say(`Failed: ${e}`, true);
  } finally {
    updateButton();
  }
}

// A config file is only read when the app starts. Without this the user closes
// the window, sees no change, and assumes brewkeg failed.
// One list, two states. Apps we already cycled are struck through as done so
// the panel answers "is there anything left for me?" rather than repeating work
// the user did not have to do.
function showRestart(items) {
  const box = el("restart");
  const list = el("restart-list");
  list.innerHTML = "";
  if (!items || !items.length) {
    box.hidden = true;
    return;
  }
  el("restart-title").textContent =
    items.some((i) => i.done) && items.every((i) => i.done)
      ? "Restarted for you"
      : "Restart to apply";
  for (const it of items) {
    const li = document.createElement("li");
    li.textContent = it.done ? `${it.what} — restarted` : `${it.what} — ${it.action}`;
    if (it.done) li.className = "done";
    list.appendChild(li);
  }
  box.hidden = false;
}

function copyRestart() {
  const pending = restartHints.filter((h) => !h.done);
  const text = pending.length
    ? ["Restart these to apply:", ...pending.map((h) => `• ${h.what} — ${h.action}`)].join("\n")
    : "Everything has been restarted.";
  api.CopyToClipboard(text);
  el("copy-restart").textContent = "Copied";
  setTimeout(() => (el("copy-restart").textContent = "Copy"), 1500);
}

function showDocket(r) {
  el("backup-id").textContent = r.backupId;
  const lines = el("docket-lines");
  lines.innerHTML = "";

  for (const res of r.results || []) {
    const li = document.createElement("li");
    if (!res.ok) li.className = "fail";

    const mark = document.createElement("span");
    mark.className = "mark";
    mark.textContent = res.ok ? "✓" : "✕";

    const body = document.createElement("span");
    body.className = res.manual ? "manual" : "path";
    body.textContent = res.ok ? (res.manual || (res.paths || []).join("\n")) : res.error;

    const where = document.createElement("span");
    where.className = "where";
    // "written" would be a lie for a disconnect. The docket is the only place
    // the user sees what actually happened to each file, so it has to say
    // which direction each one went.
    where.textContent = res.ok ? (res.manual ? "in-app" : res.removed ? "cleared" : "written") : "";

    li.append(mark, body, where);
    lines.appendChild(li);
  }
  // Anything we could not cycle stays in the list as a hint, because it still
  // has to happen by hand. Anything we did cycle is shown as done, not advice.
  const handled = new Map();
  for (const x of r.relaunched || []) {
    if (x.action === "restarted") handled.set(x.app, true);
    else if (x.action === "could not restart") {
      handled.set(x.app, { what: x.app, action: x.detail || "quit and reopen it" });
    }
  }
  restartHints = (r.restart || [])
    .filter((h) => !handled.has(h.what))
    .map((h) => ({ ...h, done: false }));
  for (const [name, v] of handled) {
    if (v === true) restartHints.push({ what: name, done: true });
    else restartHints.push({ ...v, done: false });
  }
  showRestart(restartHints);
  el("docket").hidden = false;
  // The promise line is now what the docket is showing — don't say it twice.
  el("promise").hidden = true;
  el("docket").scrollIntoView({ behavior: "smooth", block: "end" });
  say(r.message, !r.ok);
}

async function undo() {
  if (!confirm("Revert your config files?")) return;
  try {
    await api.Restore("");
    state = await api.GetState();
    render();
    el("docket").hidden = true;
    el("restart").hidden = true;
    restartHints = [];
    say("Reverted.");
  } catch (e) {
    say(`Failed: ${e}`, true);
  }
}

function say(text, bad) {
  const n = el("note");
  n.hidden = !text;
  n.textContent = text;
  n.className = "note" + (bad ? " bad" : "");
}
