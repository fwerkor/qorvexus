package webui

const controlPanelHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Qorvexus</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f8;
      --panel: #ffffff;
      --ink: #172026;
      --muted: #66727c;
      --line: #d8dee4;
      --accent: #13795b;
      --accent-soft: #e6f3ee;
      --danger: #b42318;
      --warn: #9a6700;
      --ok: #13795b;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      font-size: 14px;
    }
    header {
      height: 58px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 0 22px;
      border-bottom: 1px solid var(--line);
      background: var(--panel);
      position: sticky;
      top: 0;
      z-index: 5;
    }
    .brand { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
    .brand h1 { margin: 0; font-size: 17px; letter-spacing: 0; }
    .brand span, .meta { color: var(--muted); font-size: 12px; white-space: nowrap; }
    main {
      display: grid;
      grid-template-columns: 220px minmax(0, 1fr);
      gap: 18px;
      max-width: 1440px;
      margin: 0 auto;
      padding: 18px;
    }
    nav {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 8px;
      height: max-content;
      position: sticky;
      top: 76px;
    }
    nav button {
      width: 100%;
      min-height: 38px;
      text-align: left;
      border: 0;
      border-radius: 6px;
      background: transparent;
      color: var(--ink);
      padding: 9px 10px;
      font: inherit;
      cursor: pointer;
    }
    nav button.active { background: var(--accent-soft); color: var(--accent); font-weight: 700; }
    .view { display: none; gap: 14px; }
    .view.active { display: grid; }
    .grid { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 14px; }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 14px;
      min-width: 0;
    }
    .span-12 { grid-column: span 12; }
    .span-8 { grid-column: span 8; }
    .span-6 { grid-column: span 6; }
    .span-4 { grid-column: span 4; }
    .head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
    h2 { margin: 0; font-size: 15px; letter-spacing: 0; }
    label { display: block; color: var(--muted); font-size: 12px; margin-bottom: 6px; }
    input, select, textarea {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--ink);
      padding: 9px 10px;
      font: inherit;
      min-height: 38px;
    }
    textarea { min-height: 520px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; line-height: 1.5; }
    button {
      min-height: 34px;
      border: 1px solid var(--accent);
      border-radius: 6px;
      background: var(--accent);
      color: #fff;
      padding: 7px 11px;
      font: inherit;
      cursor: pointer;
    }
    button.secondary { background: #fff; color: var(--ink); border-color: var(--line); }
    button.danger { background: var(--danger); border-color: var(--danger); }
    .actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
    .metric-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
    .metric { border: 1px solid var(--line); border-radius: 8px; padding: 11px; background: #fbfcfd; min-height: 76px; }
    .metric .k { color: var(--muted); font-size: 12px; margin-bottom: 7px; }
    .metric .v { font-weight: 700; overflow-wrap: anywhere; }
    .status { display: inline-flex; align-items: center; border-radius: 999px; padding: 3px 8px; font-size: 12px; background: #eef1f4; color: var(--muted); }
    .status.ok, .status.succeeded, .status.active { background: #e6f3ee; color: var(--ok); }
    .status.failed, .status.error { background: #fdecec; color: var(--danger); }
    .status.running, .status.queued, .status.planned { background: #fff4d6; color: var(--warn); }
    pre {
      margin: 0;
      padding: 12px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #f9fafb;
      overflow: auto;
      max-height: 520px;
      white-space: pre-wrap;
      font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
      font-size: 12px;
      line-height: 1.5;
    }
    table { width: 100%; border-collapse: collapse; table-layout: fixed; }
    th, td { border-bottom: 1px solid var(--line); padding: 9px 8px; text-align: left; vertical-align: top; overflow-wrap: anywhere; }
    th { color: var(--muted); font-size: 12px; font-weight: 700; background: #fbfcfd; }
    tr:last-child td { border-bottom: 0; }
    .empty { color: var(--muted); padding: 18px 4px; }
    .row { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 10px; align-items: end; }
    .col-3 { grid-column: span 3; }
    .col-4 { grid-column: span 4; }
    .col-6 { grid-column: span 6; }
    .col-12 { grid-column: span 12; }
    @media (max-width: 920px) {
      header { align-items: flex-start; height: auto; padding: 12px 14px; flex-direction: column; }
      main { grid-template-columns: 1fr; padding: 12px; }
      nav { position: static; display: flex; overflow-x: auto; }
      nav button { min-width: 118px; text-align: center; }
      .span-8, .span-6, .span-4, .col-3, .col-4, .col-6 { grid-column: span 12; }
      .metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    }
  </style>
</head>
<body>
  <header>
    <div class="brand">
      <h1>Qorvexus</h1>
      <span>{{.RuntimeMode}}</span>
    </div>
    <div class="meta">{{.WebAddress}} | {{.DefaultModel}}</div>
  </header>
  <main>
    <nav>
      <button id="tab-status" class="active" onclick="showView('status')">Status</button>
      <button id="tab-tasks" onclick="showView('tasks')">Tasks</button>
      <button id="tab-config" onclick="showView('config')">Config</button>
      <button id="tab-logs" onclick="showView('logs')">Logs</button>
    </nav>
    <section id="view-status" class="view active">
      <div class="grid">
        <div class="panel span-12">
          <div class="head">
            <h2>Runtime</h2>
            <div class="actions">
              <button class="secondary" onclick="refreshAll()">Refresh</button>
              <button class="secondary" onclick="runtimeAction('restart')">Restart</button>
              <button onclick="runtimeAction('apply')">Build</button>
            </div>
          </div>
          <div class="metric-grid" id="status-metrics"></div>
        </div>
        <div class="panel span-6">
          <div class="head"><h2>Sessions</h2><button class="secondary" onclick="loadSessions()">Refresh</button></div>
          <div id="sessions"></div>
        </div>
        <div class="panel span-6">
          <div class="head"><h2>Connectors</h2><button class="secondary" onclick="loadConnectors()">Refresh</button></div>
          <pre id="connectors">Loading...</pre>
        </div>
      </div>
    </section>
    <section id="view-tasks" class="view">
      <div class="grid">
        <div class="panel span-12">
          <div class="head">
            <h2>Queue</h2>
            <div class="actions">
              <input id="retry-id" placeholder="queue id" style="width:220px">
              <button class="secondary" onclick="retryQueue()">Retry</button>
              <button class="secondary" onclick="loadQueue()">Refresh</button>
            </div>
          </div>
          <div id="queue"></div>
        </div>
        <div class="panel span-12">
          <div class="head">
            <h2>Plans</h2>
            <div class="actions">
              <input id="plan-id" placeholder="plan id" style="width:220px">
              <input id="plan-limit" type="number" min="1" max="20" value="4" style="width:90px">
              <button class="secondary" onclick="viewPlan()">View</button>
              <button onclick="advancePlan()">Advance</button>
              <button class="secondary" onclick="loadPlans()">Refresh</button>
            </div>
          </div>
          <div id="plans"></div>
        </div>
        <div class="panel span-12">
          <h2>Task Output</h2>
          <pre id="task-output">Ready.</pre>
        </div>
      </div>
    </section>
    <section id="view-config" class="view">
      <div class="panel">
        <div class="head">
          <h2>Runtime Config</h2>
          <div class="actions">
            <button class="secondary" onclick="loadConfig()">Reload</button>
            <button onclick="saveConfig()">Save</button>
          </div>
        </div>
        <textarea id="config-text" spellcheck="false"></textarea>
        <pre id="config-output">Ready.</pre>
      </div>
    </section>
    <section id="view-logs" class="view">
      <div class="grid">
        <div class="panel span-6">
          <div class="head"><h2>Audit</h2><button class="secondary" onclick="loadAudit()">Refresh</button></div>
          <pre id="audit">Loading...</pre>
        </div>
        <div class="panel span-6">
          <div class="head"><h2>Social</h2><button class="secondary" onclick="loadSocial()">Refresh</button></div>
          <pre id="social">Loading...</pre>
        </div>
      </div>
    </section>
  </main>
  <script>
    const api = async (path, options = {}) => {
      const res = await fetch(path, options);
      const text = await res.text();
      if (!res.ok) throw new Error(text || res.statusText);
      try { return JSON.parse(text); } catch (_) { return text; }
    };
    const text = (id, value) => { document.getElementById(id).textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2); };
    const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, c => ({ "&":"&amp;", "<":"&lt;", ">":"&gt;", '"':"&quot;", "'":"&#39;" }[c]));
    const fmtTime = (value) => value ? new Date(value).toLocaleString() : "";
    function showView(name) {
      document.querySelectorAll(".view").forEach(v => v.classList.toggle("active", v.id === "view-" + name));
      document.querySelectorAll("nav button").forEach(b => b.classList.toggle("active", b.id === "tab-" + name));
    }
    function pill(value) {
      const v = String(value ?? "");
      return "<span class='status " + escapeHTML(v.toLowerCase()) + "'>" + escapeHTML(v) + "</span>";
    }
    function table(rows, cols) {
      if (!rows || rows.length === 0) return "<div class='empty'>No records.</div>";
      return "<table><thead><tr>" + cols.map(c => "<th>" + escapeHTML(c.label) + "</th>").join("") + "</tr></thead><tbody>" +
        rows.map(row => "<tr>" + cols.map(c => "<td>" + (c.html ? c.html(row) : escapeHTML(row[c.key] ?? "")) + "</td>").join("") + "</tr>").join("") +
        "</tbody></table>";
    }
    async function loadStatus() {
      const data = await api("/api/status");
      const items = [
        ["Started", fmtTime(data.started_at)],
        ["Model", data.default_model],
        ["Scheduler", data.scheduler_enabled],
        ["Queue", data.queue_enabled],
        ["Memory", data.memory_enabled],
        ["Social", data.social_enabled],
        ["Self", data.self_enabled],
        ["Apply", data.runtime_apply_enabled],
        ["Source", data.source_root || ""],
        ["Binary", data.executable_path || ""],
        ["Onboarding", data.owner_onboarding_required ? "required" : "ready"],
        ["Address", data.web_address],
      ];
      document.getElementById("status-metrics").innerHTML = items.map(([k,v]) =>
        "<div class='metric'><div class='k'>" + escapeHTML(k) + "</div><div class='v'>" + escapeHTML(v) + "</div></div>"
      ).join("");
    }
    async function loadSessions() {
      const data = await api("/api/sessions");
      document.getElementById("sessions").innerHTML = table(data.slice(0, 20), [
        {label:"ID", key:"id"},
        {label:"Model", key:"model"},
        {label:"Channel", html:r => escapeHTML(r.context?.channel || "")},
        {label:"Updated", html:r => escapeHTML(fmtTime(r.updated_at))},
        {label:"Messages", html:r => escapeHTML((r.messages || []).length)},
      ]);
    }
    async function loadQueue() {
      const data = await api("/api/queue");
      document.getElementById("queue").innerHTML = table(data, [
        {label:"ID", key:"id"},
        {label:"Status", html:r => pill(r.status)},
        {label:"Name", key:"name"},
        {label:"Plan", html:r => escapeHTML([r.plan_id, r.step_id].filter(Boolean).join(" / "))},
        {label:"Updated", html:r => escapeHTML(fmtTime(r.finished_at || r.started_at || r.created_at))},
        {label:"Error", key:"error"},
      ]);
    }
    async function loadPlans() {
      const data = await api("/api/plans?limit=25");
      document.getElementById("plans").innerHTML = table(data, [
        {label:"ID", key:"id"},
        {label:"Status", html:r => pill(r.status)},
        {label:"Goal", key:"goal"},
        {label:"Steps", html:r => escapeHTML((r.steps || []).length)},
        {label:"Updated", html:r => escapeHTML(fmtTime(r.updated_at))},
      ]);
    }
    async function viewPlan() {
      const id = document.getElementById("plan-id").value.trim();
      if (!id) return;
      const data = await api("/api/plans/view?id=" + encodeURIComponent(id));
      text("task-output", data);
    }
    async function advancePlan() {
      const payload = { id: document.getElementById("plan-id").value.trim(), limit: Number(document.getElementById("plan-limit").value || 4) };
      const data = await api("/api/plans/advance", { method:"POST", headers:{ "Content-Type":"application/json" }, body:JSON.stringify(payload) });
      text("task-output", data);
      await Promise.all([loadPlans(), loadQueue()]);
    }
    async function retryQueue() {
      const payload = { id: document.getElementById("retry-id").value.trim() };
      const data = await api("/api/queue/retry", { method:"POST", headers:{ "Content-Type":"application/json" }, body:JSON.stringify(payload) });
      text("task-output", data);
      await loadQueue();
    }
    async function loadConfig() {
      const data = await api("/api/config");
      document.getElementById("config-text").value = data.config || "";
    }
    async function saveConfig() {
      const data = await api("/api/config", { method:"POST", headers:{ "Content-Type":"application/json" }, body:JSON.stringify({ config: document.getElementById("config-text").value }) });
      text("config-output", data);
    }
    async function runtimeAction(action) {
      const data = await api("/api/runtime/control", { method:"POST", headers:{ "Content-Type":"application/json" }, body:JSON.stringify({ action, run_tests: true, reason: "webui " + action }) });
      text("task-output", data);
    }
    async function loadAudit() { text("audit", await api("/api/audit")); }
    async function loadSocial() { text("social", await api("/api/social/recent")); }
    async function loadConnectors() { text("connectors", await api("/api/social/connectors")); }
    async function refreshAll() {
      await Promise.allSettled([loadStatus(), loadSessions(), loadQueue(), loadPlans(), loadAudit(), loadSocial(), loadConnectors()]);
    }
    refreshAll();
    loadConfig();
  </script>
</body>
</html>`
