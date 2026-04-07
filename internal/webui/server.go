package webui

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"qorvexus/internal/session"
	"qorvexus/internal/taskqueue"
)

type App interface {
	Status() Status
	RunPrompt(ctx context.Context, prompt string, model string, sessionID string) (string, error)
	ListSessions() ([]session.State, error)
	ListQueue() []taskqueue.Task
	SearchMemory(query string, limit int) (string, error)
	LoadConfigText() (string, error)
	SaveConfigText(raw string) error
}

type Status struct {
	StartedAt        time.Time `json:"started_at"`
	DefaultModel     string    `json:"default_model"`
	SchedulerEnabled bool      `json:"scheduler_enabled"`
	QueueEnabled     bool      `json:"queue_enabled"`
	MemoryEnabled    bool      `json:"memory_enabled"`
	WebAddress       string    `json:"web_address"`
}

type Server struct {
	app  App
	tmpl *template.Template
}

func NewServer(app App) (*Server, error) {
	tmpl, err := template.New("dashboard").Parse(dashboardHTML)
	if err != nil {
		return nil, err
	}
	return &Server{app: app, tmpl: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/run", s.handleRun)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/queue", s.handleQueue)
	mux.HandleFunc("/api/memory", s.handleMemory)
	mux.HandleFunc("/api/config", s.handleConfig)
	return mux
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	_ = s.tmpl.Execute(w, s.app.Status())
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.Status())
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.app.RunPrompt(r.Context(), input.Prompt, input.Model, input.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": out})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.ListQueue())
}

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	raw, err := s.app.SearchMemory(query, 25)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		raw, err := s.app.LoadConfigText()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"config": raw})
	case http.MethodPost:
		var input struct {
			Config string `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.app.SaveConfigText(input.Config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Qorvexus Control Panel</title>
  <style>
    :root { color-scheme: light; --bg:#f5f1e8; --card:#fffdf7; --ink:#1e1d19; --muted:#6c6558; --line:#d9cfbf; --accent:#146356; --accent-soft:#d8efe8; --warn:#8a4b08; }
    body { margin:0; font-family: Georgia, "Times New Roman", serif; background: radial-gradient(circle at top right, #efe7d7, var(--bg) 45%); color:var(--ink); }
    .wrap { max-width: 1180px; margin: 0 auto; padding: 24px; }
    h1,h2 { margin: 0 0 12px; }
    .hero { display:flex; justify-content:space-between; gap:24px; align-items:flex-start; margin-bottom:24px; }
    .hero-card, .card { background:var(--card); border:1px solid var(--line); border-radius:18px; padding:18px; box-shadow:0 10px 30px rgba(60,40,10,.05); }
    .hero-card { flex:1; }
    .grid { display:grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap:18px; }
    .stats { display:flex; gap:10px; flex-wrap:wrap; margin-top:12px; }
    .chip { background:var(--accent-soft); color:var(--accent); border-radius:999px; padding:6px 10px; font-size:13px; }
    textarea, input { width:100%; box-sizing:border-box; padding:10px 12px; border-radius:12px; border:1px solid var(--line); background:#fff; font:inherit; }
    textarea { min-height: 180px; resize: vertical; }
    button { background:var(--accent); color:#fff; border:none; border-radius:12px; padding:10px 14px; font:inherit; cursor:pointer; }
    button.secondary { background:#fff; color:var(--ink); border:1px solid var(--line); }
    .row { display:flex; gap:10px; }
    .row > * { flex:1; }
    pre { background:#f8f5ef; border:1px solid var(--line); border-radius:12px; padding:12px; overflow:auto; white-space:pre-wrap; word-break:break-word; }
    table { width:100%; border-collapse:collapse; font-size:14px; }
    th, td { text-align:left; padding:8px; border-bottom:1px solid var(--line); vertical-align:top; }
    .muted { color:var(--muted); }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="hero">
      <div class="hero-card">
        <h1>Qorvexus Control Panel</h1>
        <p class="muted">Configure the agent, inspect status, run ad-hoc prompts, and monitor sessions, memory, and the background queue.</p>
        <div class="stats">
          <span class="chip">Default model: {{.DefaultModel}}</span>
          <span class="chip">Scheduler: {{.SchedulerEnabled}}</span>
          <span class="chip">Queue: {{.QueueEnabled}}</span>
          <span class="chip">Memory: {{.MemoryEnabled}}</span>
        </div>
      </div>
      <div class="hero-card">
        <h2>Runtime</h2>
        <p class="muted">Started at {{.StartedAt}}</p>
        <p class="muted">Listening on {{.WebAddress}}</p>
      </div>
    </div>
    <div class="grid">
      <section class="card">
        <h2>Run Prompt</h2>
        <div class="row">
          <input id="run-model" placeholder="Model override (optional)">
          <input id="run-session" placeholder="Session ID (optional)">
        </div>
        <p></p>
        <textarea id="run-prompt" placeholder="Ask Qorvexus to do something..."></textarea>
        <p></p>
        <button onclick="runPrompt()">Run</button>
        <pre id="run-output"></pre>
      </section>
      <section class="card">
        <h2>Config</h2>
        <textarea id="config-text"></textarea>
        <p></p>
        <div class="row">
          <button onclick="saveConfig()">Save Config</button>
          <button class="secondary" onclick="loadConfig()">Reload</button>
        </div>
      </section>
      <section class="card">
        <h2>Sessions</h2>
        <button class="secondary" onclick="loadSessions()">Refresh Sessions</button>
        <div id="sessions"></div>
      </section>
      <section class="card">
        <h2>Queue</h2>
        <button class="secondary" onclick="loadQueue()">Refresh Queue</button>
        <div id="queue"></div>
      </section>
      <section class="card">
        <h2>Memory</h2>
        <div class="row">
          <input id="memory-query" placeholder="Search memory">
          <button onclick="loadMemory()">Search</button>
        </div>
        <pre id="memory-output"></pre>
      </section>
      <section class="card">
        <h2>Status</h2>
        <button class="secondary" onclick="loadStatus()">Refresh Status</button>
        <pre id="status-output"></pre>
      </section>
    </div>
  </div>
  <script>
    async function api(path, options) {
      const res = await fetch(path, options);
      if (!res.ok) throw new Error(await res.text());
      const type = res.headers.get("content-type") || "";
      return type.includes("application/json") ? res.json() : res.text();
    }
    async function loadConfig() {
      const data = await api("/api/config");
      document.getElementById("config-text").value = data.config;
    }
    async function saveConfig() {
      await api("/api/config", {method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({config:document.getElementById("config-text").value})});
      alert("Config saved.");
    }
    async function loadStatus() {
      const data = await api("/api/status");
      document.getElementById("status-output").textContent = JSON.stringify(data, null, 2);
    }
    async function runPrompt() {
      const payload = {
        prompt: document.getElementById("run-prompt").value,
        model: document.getElementById("run-model").value,
        session_id: document.getElementById("run-session").value,
      };
      const data = await api("/api/run", {method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(payload)});
      document.getElementById("run-output").textContent = data.output;
    }
    async function loadSessions() {
      const data = await api("/api/sessions");
      document.getElementById("sessions").innerHTML = renderTable(data, ["id","model","updated_at"]);
    }
    async function loadQueue() {
      const data = await api("/api/queue");
      document.getElementById("queue").innerHTML = renderTable(data, ["id","status","name","created_at"]);
    }
    async function loadMemory() {
      const q = encodeURIComponent(document.getElementById("memory-query").value);
      const data = await api("/api/memory?q=" + q);
      document.getElementById("memory-output").textContent = JSON.stringify(data, null, 2);
    }
    function renderTable(rows, cols) {
      if (!rows.length) return "<p class='muted'>No data yet.</p>";
      let html = "<table><thead><tr>" + cols.map(c => "<th>" + c + "</th>").join("") + "</tr></thead><tbody>";
      for (const row of rows) {
        html += "<tr>" + cols.map(c => "<td>" + (row[c] ?? "") + "</td>").join("") + "</tr>";
      }
      html += "</tbody></table>";
      return html;
    }
    loadConfig(); loadStatus(); loadSessions(); loadQueue();
  </script>
</body>
</html>`

func LoadConfigText(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func SaveConfigText(path string, raw string) error {
	return os.WriteFile(path, []byte(raw), 0o644)
}
