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
  maskedKey: "",
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
  results: [
    { id: "claude-cli", label: "Claude Code", ok: true, paths: ["~/.claude/settings.json", "~/.zshrc"], manual: "" },
    { id: "desktop", label: "Claude Desktop", ok: true, paths: [], manual: "Developer → Configure Third-Party Inference…\nBase URL: https://brewkeg.dev" },
  ],
};

const el = (id) => document.getElementById(id);
let state = null;

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
  el("get-key").title = `Get your API key at ${state.baseUrl}/dashboard`;
  refreshSpec();
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
    line.className = "checkline" + (r.ok ? "" : r.valid ? " warn" : " bad");
    el("apply").disabled = selected().length === 0 || !r.ok;
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

    const icon = document.createElement("img");
    icon.className = "service-icon";
    icon.src = ICONS[t.id] ?? "";
    icon.alt = "";
    if (!ICONS[t.id]) icon.hidden = true;

    const name = document.createElement("div");
    name.className = "service-name";
    name.textContent = t.label;

    const path = document.createElement("div");
    path.className = "service-path";
    path.textContent = shorten(t.path || "in-app");
    path.title = t.path || "";

    const sw = document.createElement("div");
    sw.className = "switch";
    sw.id = `sw-${t.id}`;
    sw.setAttribute("role", "switch");
    sw.setAttribute("aria-checked", String(t.enabled));
    sw.setAttribute("aria-label", t.label);

    row.append(icon, name, path, sw);
    row.addEventListener("click", () => toggle(t.id));
    li.appendChild(row);
    list.appendChild(li);
  }
  updateButton();
}

function toggle(id) {
  const sw = el(`sw-${id}`);
  sw.setAttribute("aria-checked", String(sw.getAttribute("aria-checked") !== "true"));
  updateButton();
}

function selected() {
  return state.targets
    .filter((t) => el(`sw-${t.id}`).getAttribute("aria-checked") === "true")
    .map((t) => t.id);
}

function updateButton() {
  const n = selected().length;
  const btn = el("apply");
  btn.textContent = n === 0 ? "Connect" : `Connect ${n} tool${n === 1 ? "" : "s"}`;
  btn.disabled = n === 0;
}

async function apply() {
  const key = el("apikey").value.trim();
  if (!key) {
    say("Paste your API key.", true);
    el("apikey").focus();
    return;
  }
  const btn = el("apply");
  btn.disabled = true;
  try {
    showDocket(await api.Configure(key, selected(), state.baseUrl, "", ""));
  } catch (e) {
    say(`Failed: ${e}`, true);
  } finally {
    updateButton();
  }
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
    where.textContent = res.ok ? (res.manual ? "in-app" : "written") : "";

    li.append(mark, body, where);
    lines.appendChild(li);
  }
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

function shorten(p) {
  return p.replace(/^\/Users\/[^/]+/, "~");
}