// brewkeg desktop — window logic. Wails injects window.go.main.App, so bindings
// are called directly: no bundler, no build step.
//
// ?demo=1 renders the same window with fake data, so the design can be reviewed
// in a plain browser without launching the app.
const real = window.go?.main?.App;
const demo = !real && new URLSearchParams(location.search).has("demo");
const api = real ?? {
  GetState: async () => DEMO_STATE,
  CheckForUpdate: async () => ({ available: true, current: "0.1.0", latest: "0.2.0", url: "#", notes: "Adds OpenCode" }),
  OpenUpdate: () => {},
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
  render();
  checkUpdate();
  el("get-key").addEventListener("click", () => api.OpenDashboard());
  el("peek").addEventListener("click", togglePeek);
  el("undo").addEventListener("click", undo);
  el("apply").addEventListener("click", apply);
  el("update").addEventListener("click", () => api.OpenUpdate(el("update").dataset.url));
  el("get-key").title = `Get your API key at ${state.baseUrl}/dashboard`;
  if (state.maskedKey) {
    el("key-label").textContent = "Using";
    el("key-note").insertAdjacentHTML("beforeend", ` <code>${state.maskedKey}</code>`);
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