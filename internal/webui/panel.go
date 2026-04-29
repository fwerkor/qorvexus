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
    .chat-shell { display: grid; grid-template-columns: 320px minmax(0, 1fr); gap: 14px; min-height: calc(100vh - 116px); }
    .session-list { border: 1px solid var(--line); border-radius: 8px; overflow: hidden; background: var(--panel); min-height: 0; }
    .session-tools { padding: 10px; border-bottom: 1px solid var(--line); display: grid; gap: 8px; }
    .session-items { max-height: calc(100vh - 244px); overflow: auto; }
    .session-item {
      display: grid;
      gap: 4px;
      width: 100%;
      border: 0;
      border-bottom: 1px solid var(--line);
      border-radius: 0;
      background: #fff;
      color: var(--ink);
      text-align: left;
      padding: 10px 12px;
      cursor: pointer;
    }
    .session-item:hover, .session-item.active { background: var(--accent-soft); }
    .session-id { font-weight: 700; overflow-wrap: anywhere; }
    .session-preview { color: var(--muted); font-size: 12px; line-height: 1.35; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
    .session-meta { color: var(--muted); font-size: 12px; display: flex; justify-content: space-between; gap: 8px; }
    .chat-panel { border: 1px solid var(--line); border-radius: 8px; background: var(--panel); min-width: 0; display: grid; grid-template-rows: auto minmax(0, 1fr); }
    .chat-header { padding: 12px 14px; border-bottom: 1px solid var(--line); display: flex; justify-content: space-between; gap: 12px; align-items: center; }
    .chat-title { min-width: 0; }
    .chat-title strong { display: block; overflow-wrap: anywhere; }
    .chat-title span { color: var(--muted); font-size: 12px; }
    .chat-stream { padding: 14px; overflow: auto; max-height: calc(100vh - 186px); display: grid; align-content: start; gap: 12px; }
    .chat-group { display: grid; gap: 10px; border-bottom: 1px solid var(--line); padding-bottom: 14px; }
    .chat-group:last-child { border-bottom: 0; padding-bottom: 0; }
    .chat-group-head { color: var(--muted); font-size: 12px; display: flex; justify-content: space-between; gap: 10px; }
    .message { display: grid; grid-template-columns: 92px minmax(0, 1fr); gap: 10px; align-items: start; }
    .message-role { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: 0; padding-top: 8px; }
    .message-body {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 10px 12px;
      background: #fbfcfd;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      line-height: 1.5;
    }
    .message.user .message-body { background: #f2f7ff; border-color: #cfe0f8; }
    .message.assistant .message-body { background: #f6fbf8; border-color: #cde9dc; }
    .message.system .message-body { background: #fffaf0; border-color: #ead8aa; }
    .message.tool .message-body { background: #f7f7f8; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; }
    .message-tools { margin-top: 8px; color: var(--muted); font-size: 12px; }
    details.tool-detail { margin-top: 8px; border-top: 1px solid var(--line); padding-top: 8px; }
    details.tool-detail summary { cursor: pointer; color: var(--muted); font-size: 12px; }
    .tool-json { margin-top: 8px; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
    .chat-compose { border-top: 1px solid var(--line); padding: 12px 14px; display: grid; gap: 8px; }
    .chat-compose textarea { min-height: 88px; max-height: 220px; font-size: 13px; font-family: inherit; }
    @media (max-width: 920px) {
      header { align-items: flex-start; height: auto; padding: 12px 14px; flex-direction: column; }
      main { grid-template-columns: 1fr; padding: 12px; }
      nav { position: static; display: flex; overflow-x: auto; }
      nav button { min-width: 118px; text-align: center; }
      .span-8, .span-6, .span-4, .col-3, .col-4, .col-6 { grid-column: span 12; }
      .metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .chat-shell { grid-template-columns: 1fr; }
      .session-items { max-height: 300px; }
      .chat-stream { max-height: none; }
      .message { grid-template-columns: 1fr; gap: 4px; }
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
      <button id="tab-chats" onclick="showView('chats')">Chats</button>
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
    <section id="view-chats" class="view">
      <div class="chat-shell">
        <aside class="session-list">
          <div class="session-tools">
            <input id="session-search" placeholder="Search sessions or messages" oninput="renderChatSessions()">
            <div class="actions">
              <button class="secondary" onclick="selectSession('__all__')">All</button>
              <button class="secondary" onclick="loadChatSessions(true)">Refresh</button>
              <label style="display:flex;align-items:center;gap:6px;margin:0;color:var(--muted)"><input id="session-auto" type="checkbox" checked style="width:auto;min-height:0"> Auto</label>
            </div>
          </div>
          <div id="session-list" class="session-items"></div>
        </aside>
        <section class="chat-panel">
          <div class="chat-header">
            <div class="chat-title">
              <strong id="chat-heading">All sessions</strong>
              <span id="chat-subtitle">Live transcript</span>
            </div>
            <div class="actions">
              <button class="danger" onclick="deleteSelectedSession()">Delete</button>
              <button class="secondary" onclick="scrollChatBottom()">Bottom</button>
            </div>
          </div>
          <div id="chat-stream" class="chat-stream"></div>
          <div class="chat-compose">
            <textarea id="chat-input" placeholder="Send a message to the selected session"></textarea>
            <div class="actions">
              <input id="chat-model" placeholder="model override" style="width:220px">
              <button onclick="sendChatMessage()">Send</button>
              <span id="chat-action-status" class="meta"></span>
            </div>
          </div>
        </section>
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
    let chatSessions = [];
    let selectedSessionID = "__all__";
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
        {label:"ID", html:r => "<button class='secondary' onclick='openChatSession(" + JSON.stringify(r.id) + ")'>" + escapeHTML(r.id) + "</button>"},
        {label:"Model", key:"model"},
        {label:"Channel", html:r => escapeHTML(r.context?.channel || "")},
        {label:"Updated", html:r => escapeHTML(fmtTime(r.updated_at))},
        {label:"Messages", html:r => escapeHTML((r.messages || []).length)},
      ]);
    }
    async function loadChatSessions(options = {}) {
      if (typeof options === "boolean") options = { keepScroll: options };
      const keepScroll = Boolean(options.keepScroll);
      const passive = Boolean(options.passive);
      const stream = document.getElementById("chat-stream");
      const nearBottom = stream.scrollHeight - stream.scrollTop - stream.clientHeight < 80;
      const shouldHoldTranscript = passive && shouldPreserveChatDOM();
      chatSessions = await api("/api/sessions");
      renderChatSessions();
      if (!shouldHoldTranscript) {
        renderChatTranscript();
        restoreOpenToolDetails();
        if (!keepScroll || nearBottom) scrollChatBottom();
      } else {
        updateChatHeader();
      }
    }
    function openChatSession(id) {
      showView("chats");
      selectSession(id);
    }
    function selectSession(id) {
      selectedSessionID = id || "__all__";
      renderChatSessions();
      renderChatTranscript();
      updateChatActions();
      scrollChatBottom();
    }
    function sessionPreview(session) {
      const messages = session.messages || [];
      for (let i = messages.length - 1; i >= 0; i--) {
        const msg = messages[i] || {};
        const content = messageText(msg).trim();
        if (content) return content.slice(0, 160);
        if ((msg.tool_calls || []).length) return "Tool calls: " + msg.tool_calls.map(c => c.name || c.id || "tool").join(", ");
      }
      return "No messages yet.";
    }
    function renderChatSessions() {
      const q = document.getElementById("session-search")?.value?.toLowerCase().trim() || "";
      const sessions = chatSessions.filter(s => {
        if (!q) return true;
        const haystack = [s.id, s.model, s.context?.channel, s.context?.sender_name, sessionPreview(s), ...(s.messages || []).map(messageText)].join("\n").toLowerCase();
        return haystack.includes(q);
      });
      const allActive = selectedSessionID === "__all__" ? " active" : "";
      const allButton = "<button class='session-item" + allActive + "' onclick='selectSession(\"__all__\")'><span class='session-id'>All sessions</span><span class='session-preview'>" + sessions.length + " visible sessions</span><span class='session-meta'><span>live</span><span>" + totalMessageCount(sessions) + " messages</span></span></button>";
      document.getElementById("session-list").innerHTML = allButton + sessions.map(s => {
        const active = selectedSessionID === s.id ? " active" : "";
        return "<button class='session-item" + active + "' onclick='selectSession(" + JSON.stringify(s.id) + ")'>" +
          "<span class='session-id'>" + escapeHTML(s.id) + "</span>" +
          "<span class='session-preview'>" + escapeHTML(sessionPreview(s)) + "</span>" +
          "<span class='session-meta'><span>" + escapeHTML(s.context?.channel || s.model || "") + "</span><span>" + escapeHTML(fmtTime(s.updated_at)) + "</span></span>" +
        "</button>";
      }).join("");
    }
    function totalMessageCount(sessions) {
      return sessions.reduce((sum, s) => sum + ((s.messages || []).length), 0);
    }
    function visibleChatSessions() {
      return selectedSessionID === "__all__" ? chatSessions : chatSessions.filter(s => s.id === selectedSessionID);
    }
    function updateChatHeader() {
      const sessions = visibleChatSessions();
      const heading = selectedSessionID === "__all__" ? "All sessions" : selectedSessionID;
      document.getElementById("chat-heading").textContent = heading;
      document.getElementById("chat-subtitle").textContent = sessions.length + " session(s), " + totalMessageCount(sessions) + " message(s)";
      updateChatActions();
    }
    function shouldPreserveChatDOM() {
      const stream = document.getElementById("chat-stream");
      const active = document.activeElement;
      const focusedInsideChat = active && stream && stream.contains(active);
      const openDetails = stream && stream.querySelector("details[open]");
      const nearBottom = !stream || stream.scrollHeight - stream.scrollTop - stream.clientHeight < 80;
      return focusedInsideChat || openDetails || !nearBottom;
    }
    function captureOpenToolDetails() {
      return new Set(Array.from(document.querySelectorAll("#chat-stream details.tool-detail[open]")).map(el => el.dataset.detailId).filter(Boolean));
    }
    function restoreOpenToolDetails(openIDs) {
      const ids = openIDs || captureOpenToolDetails();
      ids.forEach(id => {
        const el = document.querySelector("#chat-stream details.tool-detail[data-detail-id='" + cssEscape(id) + "']");
        if (el) el.open = true;
      });
    }
    function cssEscape(value) {
      if (window.CSS && CSS.escape) return CSS.escape(value);
      return String(value).replace(/['\\]/g, "\\$&");
    }
    function renderChatTranscript() {
      const openIDs = captureOpenToolDetails();
      const sessions = selectedSessionID === "__all__" ? chatSessions : chatSessions.filter(s => s.id === selectedSessionID);
      updateChatHeader();
      if (!sessions.length) {
        document.getElementById("chat-stream").innerHTML = "<div class='empty'>No session selected.</div>";
        return;
      }
      document.getElementById("chat-stream").innerHTML = sessions.map(renderSessionTranscript).join("");
      restoreOpenToolDetails(openIDs);
    }
    function renderSessionTranscript(session) {
      const messages = session.messages || [];
      const head = "<div class='chat-group-head'><strong>" + escapeHTML(session.id) + "</strong><span>" + escapeHTML([session.model, session.context?.channel, fmtTime(session.updated_at)].filter(Boolean).join(" | ")) + "</span></div>";
      const body = messages.length ? messages.map((msg, index) => renderMessage(msg, session.id, index)).join("") : "<div class='empty'>No messages.</div>";
      return "<div class='chat-group'>" + head + body + "</div>";
    }
    function renderMessage(msg, sessionID, index) {
      const role = String(msg.role || "message");
      const content = messageText(msg) || "(empty)";
      const detailPrefix = sessionID + ":" + index;
      const callHTML = renderToolCallDetails(msg.tool_calls || [], detailPrefix);
      const toolHTML = msg.tool_call_id || role === "tool" ? renderToolResultDetail(msg, detailPrefix) : "";
      return "<div class='message " + escapeHTML(role) + "'><div class='message-role'>" + escapeHTML(role) + "</div><div class='message-body'>" + escapeHTML(content) + callHTML + toolHTML + "</div></div>";
    }
    function renderToolCallDetails(calls, detailPrefix) {
      if (!calls.length) return "";
      return calls.map((call, index) => {
        const name = call.name || call.id || "tool";
        const args = prettyJSON(call.arguments || "{}");
        const id = detailPrefix + ":call:" + index;
        return "<details class='tool-detail' data-detail-id='" + escapeHTML(id) + "'><summary>Tool call " + escapeHTML(index + 1) + ": " + escapeHTML(name) + "</summary><div class='tool-json'>" + escapeHTML(args) + "</div></details>";
      }).join("");
    }
    function renderToolResultDetail(msg, detailPrefix) {
      const label = msg.name || msg.tool_call_id || "tool";
      const body = msg.content || "";
      const id = detailPrefix + ":result";
      return "<details class='tool-detail' data-detail-id='" + escapeHTML(id) + "'><summary>Tool result: " + escapeHTML(label) + "</summary><div class='tool-json'>" + escapeHTML(prettyJSON(body)) + "</div></details>";
    }
    function prettyJSON(value) {
      if (typeof value !== "string") return JSON.stringify(value, null, 2);
      const trimmed = value.trim();
      if (!trimmed) return "";
      try { return JSON.stringify(JSON.parse(trimmed), null, 2); } catch (_) { return value; }
    }
    function messageText(msg) {
      if (!msg) return "";
      if (String(msg.content || "").trim()) return String(msg.content || "");
      const parts = msg.parts || [];
      if (!parts.length) return "";
      return parts.map(p => p.text || (p.image_url ? "[image] " + p.image_url : "")).filter(Boolean).join("\n");
    }
    function scrollChatBottom() {
      const el = document.getElementById("chat-stream");
      if (el) el.scrollTop = el.scrollHeight;
    }
    function updateChatActions() {
      const single = selectedSessionID && selectedSessionID !== "__all__";
      const input = document.getElementById("chat-input");
      const model = document.getElementById("chat-model");
      const status = document.getElementById("chat-action-status");
      if (input) input.disabled = !single;
      if (model) model.disabled = !single;
      if (status && !single) status.textContent = "Select one session to send or delete.";
      if (status && single) status.textContent = "";
    }
    async function sendChatMessage() {
      if (!selectedSessionID || selectedSessionID === "__all__") return;
      const input = document.getElementById("chat-input");
      const status = document.getElementById("chat-action-status");
      const prompt = input.value.trim();
      if (!prompt) return;
      status.textContent = "Sending...";
      const payload = {
        session_id: selectedSessionID,
        model: document.getElementById("chat-model").value.trim(),
        prompt
      };
      try {
        const data = await api("/api/run", { method:"POST", headers:{ "Content-Type":"application/json" }, body:JSON.stringify(payload) });
        input.value = "";
        status.textContent = "Sent.";
        await loadChatSessions({ keepScroll: false });
      } catch (err) {
        status.textContent = err.message || String(err);
      }
    }
    async function deleteSelectedSession() {
      if (!selectedSessionID || selectedSessionID === "__all__") return;
      if (!confirm("Delete session " + selectedSessionID + "?")) return;
      const status = document.getElementById("chat-action-status");
      status.textContent = "Deleting...";
      try {
        await api("/api/sessions/delete", { method:"POST", headers:{ "Content-Type":"application/json" }, body:JSON.stringify({ id: selectedSessionID }) });
        selectedSessionID = "__all__";
        status.textContent = "Deleted.";
        await Promise.all([loadSessions(), loadChatSessions({ keepScroll: false })]);
      } catch (err) {
        status.textContent = err.message || String(err);
      }
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
      await Promise.allSettled([loadStatus(), loadSessions(), loadChatSessions({ keepScroll: true }), loadQueue(), loadPlans(), loadAudit(), loadSocial(), loadConnectors()]);
    }
    setInterval(() => {
      const checkbox = document.getElementById("session-auto");
      if (checkbox && checkbox.checked) loadChatSessions({ keepScroll: true, passive: true }).catch(() => {});
    }, 3000);
    refreshAll();
    loadConfig();
  </script>
</body>
</html>`
