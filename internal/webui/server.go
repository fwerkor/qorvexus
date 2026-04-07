package webui

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"qorvexus/internal/session"
	"qorvexus/internal/social"
	"qorvexus/internal/taskqueue"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
)

type App interface {
	Status() Status
	RunPrompt(ctx context.Context, prompt string, model string, sessionID string) (string, error)
	ListSessions() ([]session.State, error)
	ListQueue() []taskqueue.Task
	SearchMemory(query string, limit int) (string, error)
	ListSelfImprovements(ctx context.Context, limit int) (string, error)
	ListRecentSocial(ctx context.Context, limit int) (string, error)
	ListSocialConnectors(ctx context.Context) (string, error)
	ListCommitments(ctx context.Context, limit int) (string, error)
	CommitmentSummary(ctx context.Context) (string, error)
	ScanCommitments(ctx context.Context) (string, error)
	ListAudit(ctx context.Context, limit int) (string, error)
	MineSelfImprovements(ctx context.Context, limit int) (string, error)
	CaptureSelfImprovement(ctx context.Context, title string, description string, kind string, promote bool, model string) (string, error)
	LoadConfigText() (string, error)
	SocialWebhookAdapters() []social.WebhookAdapter
	SaveConfigText(raw string) error
	HandleSocialEnvelope(ctx context.Context, env social.Envelope) (string, error)
	RetryQueueTask(ctx context.Context, id string) (string, error)
	UpdateCommitmentStatus(ctx context.Context, id string, status string) (string, error)
	UpdateSelfImprovementStatus(ctx context.Context, id string, status string) (string, error)
}

type Status struct {
	StartedAt                time.Time `json:"started_at"`
	DefaultModel             string    `json:"default_model"`
	SchedulerEnabled         bool      `json:"scheduler_enabled"`
	QueueEnabled             bool      `json:"queue_enabled"`
	MemoryEnabled            bool      `json:"memory_enabled"`
	SelfEnabled              bool      `json:"self_enabled"`
	SocialEnabled            bool      `json:"social_enabled"`
	WebAddress               string    `json:"web_address"`
	OwnerOnboardingRequired  bool      `json:"owner_onboarding_required"`
	OwnerOnboardingSessionID string    `json:"owner_onboarding_session_id,omitempty"`
	OwnerOnboardingPrompt    string    `json:"owner_onboarding_prompt,omitempty"`
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
	mux.HandleFunc("/api/self", s.handleSelf)
	mux.HandleFunc("/api/social/recent", s.handleSocialRecent)
	mux.HandleFunc("/api/social/connectors", s.handleSocialConnectors)
	mux.HandleFunc("/api/commitments", s.handleCommitments)
	mux.HandleFunc("/api/commitments/summary", s.handleCommitmentSummary)
	mux.HandleFunc("/api/commitments/scan", s.handleCommitmentScan)
	mux.HandleFunc("/api/audit", s.handleAudit)
	mux.HandleFunc("/api/self/mine", s.handleSelfMine)
	mux.HandleFunc("/api/self/capture", s.handleSelfCapture)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/social/inbound", s.handleSocialInbound)
	mux.HandleFunc("/api/queue/retry", s.handleQueueRetry)
	mux.HandleFunc("/api/commitments/status", s.handleCommitmentStatus)
	mux.HandleFunc("/api/self/status", s.handleSelfStatus)
	for _, adapter := range s.app.SocialWebhookAdapters() {
		adapter := adapter
		mux.HandleFunc(adapter.Path(), func(w http.ResponseWriter, r *http.Request) {
			s.handleSocialWebhook(w, r, adapter)
		})
	}
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
	out, err := s.app.RunPrompt(ownerContext(r.Context()), input.Prompt, input.Model, input.SessionID)
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

func (s *Server) handleSelf(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.ListSelfImprovements(ownerContext(r.Context()), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleSocialRecent(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.ListRecentSocial(ownerContext(r.Context()), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleSocialConnectors(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.ListSocialConnectors(ownerContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleCommitments(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.ListCommitments(ownerContext(r.Context()), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleCommitmentSummary(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.CommitmentSummary(ownerContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleCommitmentScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := s.app.ScanCommitments(ownerContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.ListAudit(ownerContext(r.Context()), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleSelfMine(w http.ResponseWriter, r *http.Request) {
	raw, err := s.app.MineSelfImprovements(ownerContext(r.Context()), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleSelfCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		Promote     bool   `json:"promote"`
		Model       string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.app.CaptureSelfImprovement(ownerContext(r.Context()), input.Title, input.Description, input.Kind, input.Promote, input.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out})
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

func (s *Server) handleSocialInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var env social.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.app.HandleSocialEnvelope(r.Context(), env)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"response": out})
}

func (s *Server) handleSocialWebhook(w http.ResponseWriter, r *http.Request, adapter social.WebhookAdapter) {
	env, ok, err := adapter.ParseWebhook(r)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "method not allowed"):
			http.Error(w, err.Error(), http.StatusMethodNotAllowed)
		case strings.Contains(err.Error(), "secret token"):
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, "channel": adapter.Name()})
		return
	}
	if _, err := s.app.HandleSocialEnvelope(r.Context(), env); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": adapter.Name()})
}

func SaveConfigText(path string, raw string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	return os.WriteFile(path, []byte(raw), 0o644)
}

func (s *Server) handleQueueRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.app.RetryQueueTask(ownerContext(r.Context()), input.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out})
}

func (s *Server) handleCommitmentStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.app.UpdateCommitmentStatus(ownerContext(r.Context()), input.ID, input.Status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out})
}

func (s *Server) handleSelfStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.app.UpdateSelfImprovementStatus(ownerContext(r.Context()), input.ID, input.Status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func ownerContext(ctx context.Context) context.Context {
	return tool.WithConversationContext(ctx, types.ConversationContext{
		Channel:  "web",
		SenderID: "owner",
		Trust:    types.TrustOwner,
		IsOwner:  true,
	})
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Qorvexus Console</title>
  <style>
    :root {
      color-scheme: light;
      --bg:#f3efe7;
      --bg-deep:#e6ddcf;
      --panel:#fffdf8;
      --panel-strong:#f8f2e7;
      --ink:#182022;
      --muted:#667178;
      --line:#d7cec0;
      --accent:#0b6e5e;
      --accent-strong:#0a5348;
      --accent-soft:#dff4ee;
      --gold:#b57927;
      --gold-soft:#f8ead3;
      --danger:#9a3b2b;
      --shadow:0 18px 48px rgba(34, 33, 29, .08);
    }
    * { box-sizing:border-box; }
    body {
      margin:0;
      font-family:"IBM Plex Sans","Avenir Next","Segoe UI",sans-serif;
      color:var(--ink);
      background:
        radial-gradient(circle at top left, rgba(11,110,94,.10), transparent 28%),
        radial-gradient(circle at top right, rgba(181,121,39,.12), transparent 24%),
        linear-gradient(180deg, #f9f6f0 0%, var(--bg) 100%);
    }
    .shell { max-width: 1320px; margin: 0 auto; padding: 28px 20px 40px; }
    .hero {
      display:grid;
      grid-template-columns: minmax(0, 1.45fr) minmax(320px, .85fr);
      gap:18px;
      margin-bottom:18px;
    }
    .panel {
      background:linear-gradient(180deg, rgba(255,255,255,.92), rgba(255,253,248,.96));
      border:1px solid rgba(215,206,192,.9);
      border-radius:24px;
      box-shadow:var(--shadow);
    }
    .hero-main {
      padding:26px;
      min-height:220px;
      background:
        linear-gradient(135deg, rgba(11,110,94,.08), rgba(255,255,255,.5) 45%, rgba(181,121,39,.10)),
        linear-gradient(180deg, rgba(255,255,255,.96), rgba(255,250,244,.96));
    }
    .eyebrow {
      display:inline-flex;
      align-items:center;
      gap:8px;
      padding:7px 12px;
      border-radius:999px;
      background:var(--accent-soft);
      color:var(--accent-strong);
      font-size:12px;
      font-weight:700;
      letter-spacing:.06em;
      text-transform:uppercase;
    }
    h1, h2, h3 {
      font-family:"Iowan Old Style","Palatino Linotype","Book Antiqua",serif;
      margin:0;
      letter-spacing:-.02em;
    }
    h1 { font-size:clamp(2rem, 5vw, 3.4rem); margin-top:16px; }
    h2 { font-size:1.45rem; margin-bottom:10px; }
    h3 { font-size:1.05rem; margin-bottom:10px; }
    p { margin:0; }
    .lede {
      margin-top:12px;
      max-width:60ch;
      color:var(--muted);
      line-height:1.6;
      font-size:1rem;
    }
    .chip-row, .metric-row, .button-row, .split-row, .mini-grid {
      display:flex;
      flex-wrap:wrap;
      gap:10px;
    }
    .chip {
      display:inline-flex;
      align-items:center;
      gap:8px;
      padding:8px 12px;
      border:1px solid rgba(11,110,94,.14);
      border-radius:999px;
      background:rgba(255,255,255,.75);
      color:var(--ink);
      font-size:13px;
    }
    .chip strong { color:var(--accent-strong); }
    .hero-main .chip-row { margin-top:18px; }
    .hero-side {
      padding:22px;
      display:flex;
      flex-direction:column;
      gap:14px;
      background:
        linear-gradient(180deg, rgba(255,253,248,.95), rgba(248,242,231,.98));
    }
    .status-card {
      padding:16px;
      border:1px solid var(--line);
      border-radius:18px;
      background:rgba(255,255,255,.7);
    }
    .metric {
      flex:1 1 120px;
      min-width:120px;
      padding:14px 14px 12px;
      border-radius:18px;
      background:var(--panel-strong);
      border:1px solid var(--line);
    }
    .metric-label {
      font-size:12px;
      text-transform:uppercase;
      letter-spacing:.08em;
      color:var(--muted);
      margin-bottom:8px;
    }
    .metric-value {
      font-size:1.2rem;
      font-weight:700;
    }
    .alert {
      padding:14px 16px;
      border-radius:18px;
      border:1px solid rgba(181,121,39,.28);
      background:linear-gradient(180deg, rgba(248,234,211,.96), rgba(255,249,238,.98));
    }
    .alert strong {
      display:block;
      margin-bottom:6px;
      color:#6a4510;
    }
    .alert p + p { margin-top:8px; }
    .layout {
      display:grid;
      grid-template-columns: 250px minmax(0, 1fr);
      gap:18px;
      align-items:start;
    }
    .sidebar {
      position:sticky;
      top:18px;
      padding:16px;
    }
    .nav {
      display:grid;
      gap:8px;
      margin-top:6px;
    }
    .nav button {
      text-align:left;
      justify-content:flex-start;
      width:100%;
      padding:13px 14px;
      border-radius:16px;
      background:transparent;
      color:var(--ink);
      border:1px solid transparent;
      font-weight:600;
    }
    .nav button.active {
      background:var(--accent-soft);
      color:var(--accent-strong);
      border-color:rgba(11,110,94,.18);
    }
    .nav button.secondary {
      background:transparent;
      border-color:transparent;
    }
    .workspace {
      display:grid;
      gap:18px;
    }
    .tab-pane {
      display:none;
      gap:18px;
    }
    .tab-pane.active {
      display:grid;
    }
    .overview-grid,
    .ops-grid,
    .data-grid,
    .social-grid {
      display:grid;
      gap:18px;
      grid-template-columns: repeat(12, minmax(0, 1fr));
    }
    .span-12 { grid-column: span 12; }
    .span-8 { grid-column: span 8; }
    .span-7 { grid-column: span 7; }
    .span-6 { grid-column: span 6; }
    .span-5 { grid-column: span 5; }
    .span-4 { grid-column: span 4; }
    .card {
      padding:20px;
    }
    .card-head {
      display:flex;
      align-items:flex-start;
      justify-content:space-between;
      gap:12px;
      margin-bottom:14px;
    }
    .card-head p {
      color:var(--muted);
      font-size:14px;
      line-height:1.55;
    }
    .mini-grid .metric {
      background:rgba(255,255,255,.68);
    }
    label {
      display:block;
      margin-bottom:8px;
      font-size:13px;
      font-weight:700;
      color:var(--muted);
      letter-spacing:.03em;
      text-transform:uppercase;
    }
    input, textarea {
      width:100%;
      border:1px solid var(--line);
      background:rgba(255,255,255,.9);
      color:var(--ink);
      border-radius:16px;
      padding:12px 14px;
      font:inherit;
      outline:none;
      transition:border-color .18s ease, box-shadow .18s ease, transform .18s ease;
    }
    input:focus, textarea:focus {
      border-color:rgba(11,110,94,.4);
      box-shadow:0 0 0 4px rgba(11,110,94,.10);
    }
    textarea {
      min-height:150px;
      resize:vertical;
      line-height:1.55;
    }
    .prompt-box {
      min-height:220px;
    }
    button {
      border:none;
      border-radius:14px;
      padding:12px 16px;
      font:inherit;
      font-weight:700;
      cursor:pointer;
      transition:transform .16s ease, opacity .16s ease, background .16s ease;
      background:var(--accent);
      color:#fff;
    }
    button:hover { transform:translateY(-1px); }
    button.secondary {
      background:#fff;
      color:var(--ink);
      border:1px solid var(--line);
    }
    button.ghost {
      background:transparent;
      color:var(--accent-strong);
      border:1px dashed rgba(11,110,94,.28);
    }
    .button-row { margin-top:14px; }
    .split-row > * { flex:1 1 220px; }
    .mono, pre, code {
      font-family:"IBM Plex Mono","SFMono-Regular","Consolas",monospace;
    }
    pre {
      margin:0;
      border-radius:18px;
      padding:16px;
      border:1px solid #dfd7ca;
      background:
        linear-gradient(180deg, rgba(248,245,239,.96), rgba(245,240,231,.96));
      overflow:auto;
      white-space:pre-wrap;
      word-break:break-word;
      min-height:120px;
      line-height:1.55;
      font-size:13px;
    }
    .result {
      min-height:180px;
    }
    .compact {
      min-height:120px;
      max-height:320px;
    }
    .muted { color:var(--muted); }
    table {
      width:100%;
      border-collapse:collapse;
      font-size:14px;
    }
    th, td {
      text-align:left;
      padding:10px 8px;
      border-bottom:1px solid #ece3d6;
      vertical-align:top;
    }
    th {
      font-size:12px;
      text-transform:uppercase;
      letter-spacing:.06em;
      color:var(--muted);
    }
    .stack { display:grid; gap:14px; }
    .hidden { display:none; }
    @media (max-width: 1080px) {
      .hero, .layout { grid-template-columns: 1fr; }
      .sidebar { position:static; }
      .span-8, .span-7, .span-6, .span-5, .span-4 { grid-column: span 12; }
    }
    @media (max-width: 720px) {
      .shell { padding:18px 14px 30px; }
      .hero-main, .hero-side, .card, .sidebar { padding:16px; }
      h1 { font-size:2.2rem; }
      .nav { grid-template-columns: repeat(2, minmax(0,1fr)); }
      .nav button { text-align:center; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <section class="hero">
      <div class="panel hero-main">
        <span class="eyebrow">Qorvexus Console</span>
        <h1>Operate the agent without fighting the interface.</h1>
        <p class="lede">The command center stays focused on the things you actually need first: run work, inspect memory, watch queue health, and only then drop into social, self-improvement, or raw config.</p>
        <div class="chip-row">
          <span class="chip"><strong>Default model</strong> {{.DefaultModel}}</span>
          <span class="chip"><strong>Memory</strong> {{.MemoryEnabled}}</span>
          <span class="chip"><strong>Queue</strong> {{.QueueEnabled}}</span>
          <span class="chip"><strong>Scheduler</strong> {{.SchedulerEnabled}}</span>
          <span class="chip"><strong>Self</strong> {{.SelfEnabled}}</span>
          <span class="chip"><strong>Social</strong> {{.SocialEnabled}}</span>
        </div>
      </div>
      <div class="panel hero-side">
        <div class="status-card">
          <div class="metric-label">Runtime</div>
          <div class="metric-value mono">{{.WebAddress}}</div>
          <p class="muted" style="margin-top:8px;">Started at {{.StartedAt}}</p>
        </div>
        <div class="metric-row">
          <div class="metric">
            <div class="metric-label">Default Session</div>
            <div class="metric-value mono" id="hero-session">{{if .OwnerOnboardingRequired}}{{.OwnerOnboardingSessionID}}{{else}}ad-hoc{{end}}</div>
          </div>
          <div class="metric">
            <div class="metric-label">Primary Focus</div>
            <div class="metric-value" id="hero-focus">{{if .OwnerOnboardingRequired}}Onboarding{{else}}Operations{{end}}</div>
          </div>
        </div>
        {{if .OwnerOnboardingRequired}}
        <div class="alert">
          <strong>Owner onboarding still needs a reply.</strong>
          <p class="muted">Continue in session <span class="mono">{{.OwnerOnboardingSessionID}}</span>.</p>
          <pre>{{.OwnerOnboardingPrompt}}</pre>
        </div>
        {{else}}
        <div class="status-card">
          <div class="metric-label">Status</div>
          <p class="muted">Owner profile is already present, so new sessions can start with remembered identity, rules, and preferences.</p>
        </div>
        {{end}}
      </div>
    </section>

    <section class="layout">
      <aside class="panel sidebar">
        <h3>Workspace</h3>
        <p class="muted" style="margin-bottom:12px;">Keep one surface visible at a time.</p>
        <div class="nav">
          <button id="tab-button-overview" class="active" onclick="showTab('overview')">Overview</button>
          <button id="tab-button-operations" onclick="showTab('operations')">Operations</button>
          <button id="tab-button-memory" onclick="showTab('memory')">Memory</button>
          <button id="tab-button-social" onclick="showTab('social')">Social</button>
          <button id="tab-button-config" onclick="showTab('config')">Config</button>
        </div>
      </aside>

      <main class="workspace">
        <section id="tab-overview" class="tab-pane active">
          <div class="overview-grid">
            <article class="panel card span-7">
              <div class="card-head">
                <div>
                  <h2>Quick Run</h2>
                  <p>Run a prompt immediately without hunting through the rest of the dashboard.</p>
                </div>
              </div>
              <div class="split-row">
                <div>
                  <label for="run-model">Model Override</label>
                  <input id="run-model" placeholder="Optional">
                </div>
                <div>
                  <label for="run-session">Session ID</label>
                  <input id="run-session" placeholder="Optional">
                </div>
              </div>
              <div style="margin-top:14px;">
                <label for="run-prompt">Prompt</label>
                <textarea id="run-prompt" class="prompt-box" placeholder="Ask Qorvexus to do something concrete..."></textarea>
              </div>
              <div class="button-row">
                <button onclick="runPrompt()">Run Prompt</button>
                <button class="secondary" onclick="prefillOnboarding()">Use Onboarding Session</button>
                <button class="ghost" onclick="showTab('memory')">Search Memory Instead</button>
              </div>
            </article>

            <article class="panel card span-5">
              <div class="card-head">
                <div>
                  <h2>Immediate Context</h2>
                  <p>What the runtime thinks is important right now.</p>
                </div>
              </div>
              <div class="mini-grid">
                <div class="metric">
                  <div class="metric-label">Current Tab</div>
                  <div class="metric-value" id="current-tab-label">Overview</div>
                </div>
                <div class="metric">
                  <div class="metric-label">Web Address</div>
                  <div class="metric-value mono">{{.WebAddress}}</div>
                </div>
              </div>
              <div style="margin-top:14px;">
                <label>Run Output</label>
                <pre id="run-output" class="result">{{if .OwnerOnboardingRequired}}{{.OwnerOnboardingPrompt}}{{else}}Ready. Run a prompt or open another workspace tab.{{end}}</pre>
              </div>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2>System Snapshot</h2>
                  <p>High-signal runtime state without dropping you straight into raw JSON.</p>
                </div>
                <div class="button-row" style="margin-top:0;">
                  <button class="secondary" onclick="loadStatus()">Refresh Status</button>
                  <button class="secondary" onclick="loadSessions()">Refresh Sessions</button>
                  <button class="secondary" onclick="loadQueue()">Refresh Queue</button>
                </div>
              </div>
              <div class="overview-grid">
                <div class="span-4 stack">
                  <div>
                    <label>Sessions</label>
                    <div id="sessions"></div>
                  </div>
                </div>
                <div class="span-4 stack">
                  <div>
                    <label>Queue</label>
                    <div id="queue"></div>
                  </div>
                </div>
                <div class="span-4 stack">
                  <div>
                    <label>Raw Status</label>
                    <pre id="status-output" class="compact"></pre>
                  </div>
                </div>
              </div>
            </article>
          </div>
        </section>

        <section id="tab-operations" class="tab-pane">
          <div class="ops-grid">
            <article class="panel card span-6">
              <div class="card-head">
                <div>
                  <h2>Queue Recovery</h2>
                  <p>Retry a failed task without digging through raw queue output first.</p>
                </div>
              </div>
              <label for="queue-retry-id">Queue Task ID</label>
              <input id="queue-retry-id" placeholder="task-...">
              <div class="button-row">
                <button onclick="retryQueue()">Retry Task</button>
                <button class="secondary" onclick="loadQueue()">Refresh Queue</button>
              </div>
              <pre id="queue-action-output" class="compact">Retry results will appear here.</pre>
            </article>

            <article class="panel card span-6">
              <div class="card-head">
                <div>
                  <h2>Commitments</h2>
                  <p>Review due work, run a scan, and update commitment status from one place.</p>
                </div>
              </div>
              <div class="button-row">
                <button class="secondary" onclick="loadCommitments()">Refresh Commitments</button>
                <button class="secondary" onclick="loadCommitmentSummary()">Refresh Summary</button>
                <button onclick="scanCommitments()">Run Scan</button>
              </div>
              <div style="margin-top:14px;" class="split-row">
                <div>
                  <label for="commitment-id">Commitment ID</label>
                  <input id="commitment-id" placeholder="commitment-...">
                </div>
                <div>
                  <label for="commitment-status">New Status</label>
                  <input id="commitment-status" placeholder="open / done / blocked">
                </div>
              </div>
              <div class="button-row">
                <button onclick="updateCommitmentStatus()">Update Commitment</button>
              </div>
            </article>

            <article class="panel card span-6">
              <label>Commitment Summary</label>
              <pre id="commitments-summary-output" class="compact"></pre>
            </article>

            <article class="panel card span-6">
              <label>Commitment Feed</label>
              <pre id="commitments-output" class="compact"></pre>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2>Audit Trail</h2>
                  <p>See the effects of retries, scans, and status updates without mixing them into every other card.</p>
                </div>
                <button class="secondary" onclick="loadAudit()">Refresh Audit</button>
              </div>
              <pre id="audit-output" class="compact"></pre>
            </article>
          </div>
        </section>

        <section id="tab-memory" class="tab-pane">
          <div class="data-grid">
            <article class="panel card span-5">
              <div class="card-head">
                <div>
                  <h2>Memory Explorer</h2>
                  <p>Search long-term memory directly instead of scrolling through unrelated widgets.</p>
                </div>
              </div>
              <label for="memory-query">Search Query</label>
              <input id="memory-query" placeholder="owner, timezone, project, preferences...">
              <div class="button-row">
                <button onclick="loadMemory()">Search Memory</button>
                <button class="secondary" onclick="document.getElementById('memory-query').value='owner'; loadMemory();">Owner Profile</button>
                <button class="secondary" onclick="document.getElementById('memory-query').value='project'; loadMemory();">Projects</button>
              </div>
              <pre id="memory-output" class="result">Run a memory search to inspect what the agent remembers.</pre>
            </article>

            <article class="panel card span-7">
              <div class="card-head">
                <div>
                  <h2>Self Evolution</h2>
                  <p>Keep backlog review and new idea capture together, without crowding the main workflow.</p>
                </div>
              </div>
              <div class="button-row">
                <button class="secondary" onclick="loadSelf()">Refresh Backlog</button>
                <button class="secondary" onclick="mineSelf()">Mine Ideas</button>
              </div>
              <div class="split-row" style="margin-top:14px;">
                <div>
                  <label>Backlog</label>
                  <pre id="self-output" class="compact"></pre>
                </div>
                <div>
                  <label>Mined Ideas</label>
                  <pre id="self-mine-output" class="compact"></pre>
                </div>
              </div>
              <div class="split-row" style="margin-top:14px;">
                <div>
                  <label for="self-id">Backlog ID</label>
                  <input id="self-id" placeholder="backlog item id">
                </div>
                <div>
                  <label for="self-status">New Status</label>
                  <input id="self-status" placeholder="planned / active / done">
                </div>
              </div>
              <div class="button-row">
                <button onclick="updateSelfStatus()">Update Self Status</button>
              </div>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2>Capture Improvement</h2>
                  <p>Turn a good idea into backlog state, and optionally promote it into queued execution.</p>
                </div>
              </div>
              <div class="split-row">
                <div>
                  <label for="capture-title">Title</label>
                  <input id="capture-title" placeholder="Captured idea title">
                </div>
                <div>
                  <label for="capture-kind">Kind</label>
                  <input id="capture-kind" placeholder="reliability / automation / safety">
                </div>
                <div>
                  <label for="capture-model">Promotion Model</label>
                  <input id="capture-model" placeholder="optional">
                </div>
              </div>
              <div style="margin-top:14px;">
                <label for="capture-description">Description</label>
                <textarea id="capture-description" placeholder="Describe the improvement you want Qorvexus to remember or work on."></textarea>
              </div>
              <div class="button-row">
                <button onclick="captureSelf(false)">Capture Idea</button>
                <button class="secondary" onclick="captureSelf(true)">Capture And Promote</button>
              </div>
            </article>
          </div>
        </section>

        <section id="tab-social" class="tab-pane">
          <div class="social-grid">
            <article class="panel card span-5">
              <div class="card-head">
                <div>
                  <h2>Connectors</h2>
                  <p>Quick visibility into available social channels.</p>
                </div>
                <button class="secondary" onclick="loadConnectors()">Refresh Connectors</button>
              </div>
              <pre id="social-connectors-output" class="compact"></pre>
            </article>

            <article class="panel card span-7">
              <div class="card-head">
                <div>
                  <h2>Recent Social Activity</h2>
                  <p>Inspect inbound and outbound history without mixing it with config or memory tools.</p>
                </div>
                <button class="secondary" onclick="loadSocial()">Refresh Social Log</button>
              </div>
              <pre id="social-output" class="compact"></pre>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2>Simulate Inbound Message</h2>
                  <p>Test a connector flow from one compact form.</p>
                </div>
              </div>
              <div class="split-row">
                <div>
                  <label for="social-channel">Channel</label>
                  <input id="social-channel" placeholder="telegram">
                </div>
                <div>
                  <label for="social-thread">Thread ID</label>
                  <input id="social-thread" placeholder="chat-1">
                </div>
                <div>
                  <label for="social-sender-id">Sender ID</label>
                  <input id="social-sender-id" placeholder="user-1">
                </div>
                <div>
                  <label for="social-sender-name">Sender Name</label>
                  <input id="social-sender-name" placeholder="Alice">
                </div>
              </div>
              <div style="margin-top:14px;">
                <label for="social-text">Inbound Text</label>
                <textarea id="social-text" placeholder="Paste the incoming message here."></textarea>
              </div>
              <div class="button-row">
                <button onclick="simulateSocial()">Simulate Inbound Social</button>
              </div>
            </article>
          </div>
        </section>

        <section id="tab-config" class="tab-pane">
          <div class="data-grid">
            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2>Runtime Config</h2>
                  <p>Raw configuration is available, but it now lives in its own dedicated workspace instead of crowding the main flow.</p>
                </div>
                <div class="button-row" style="margin-top:0;">
                  <button class="secondary" onclick="loadConfig()">Reload</button>
                  <button onclick="saveConfig()">Save Config</button>
                </div>
              </div>
              <textarea id="config-text" style="min-height:520px;"></textarea>
            </article>
          </div>
        </section>
      </main>
    </section>
  </div>
  <script>
    async function api(path, options) {
      const res = await fetch(path, options);
      if (!res.ok) throw new Error(await res.text());
      const type = res.headers.get("content-type") || "";
      return type.includes("application/json") ? res.json() : res.text();
    }

    function setText(id, value) {
      const node = document.getElementById(id);
      if (!node) return;
      node.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
    }

    function renderTable(rows, cols) {
      if (!rows || !rows.length) return "<p class='muted'>No data yet.</p>";
      let html = "<table><thead><tr>" + cols.map(c => "<th>" + c + "</th>").join("") + "</tr></thead><tbody>";
      for (const row of rows) {
        html += "<tr>" + cols.map(c => "<td>" + escapeHTML(row[c] ?? "") + "</td>").join("") + "</tr>";
      }
      html += "</tbody></table>";
      return html;
    }

    function escapeHTML(value) {
      return String(value)
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;");
    }

    function showTab(name) {
      for (const pane of document.querySelectorAll(".tab-pane")) {
        pane.classList.toggle("active", pane.id === "tab-" + name);
      }
      for (const button of document.querySelectorAll(".nav button")) {
        button.classList.toggle("active", button.id === "tab-button-" + name);
      }
      const label = name.charAt(0).toUpperCase() + name.slice(1);
      setText("current-tab-label", label);
    }

    function prefillOnboarding() {
      loadStatus().then(() => showTab("overview"));
    }

    async function loadConfig() {
      const data = await api("/api/config");
      document.getElementById("config-text").value = data.config;
    }

    async function saveConfig() {
      await api("/api/config", {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify({config:document.getElementById("config-text").value})
      });
      alert("Config saved.");
    }

    async function loadStatus() {
      const data = await api("/api/status");
      setText("status-output", data);
      if (data.owner_onboarding_required) {
        if (!document.getElementById("run-session").value) {
          document.getElementById("run-session").value = data.owner_onboarding_session_id || "";
        }
        if (data.owner_onboarding_prompt) {
          setText("run-output", data.owner_onboarding_prompt);
          setText("hero-session", data.owner_onboarding_session_id || "owner-onboarding");
          setText("hero-focus", "Onboarding");
        }
      } else {
        setText("hero-focus", "Operations");
      }
      return data;
    }

    async function runPrompt() {
      const payload = {
        prompt: document.getElementById("run-prompt").value,
        model: document.getElementById("run-model").value,
        session_id: document.getElementById("run-session").value,
      };
      const data = await api("/api/run", {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("run-output", data.output);
      loadSessions();
      loadAudit();
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
      setText("memory-output", data);
    }

    async function loadSelf() {
      const data = await api("/api/self");
      setText("self-output", data);
    }

    async function mineSelf() {
      const data = await api("/api/self/mine");
      setText("self-mine-output", data);
    }

    async function updateSelfStatus() {
      const payload = {
        id: document.getElementById("self-id").value,
        status: document.getElementById("self-status").value,
      };
      const data = await api("/api/self/status", {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("self-mine-output", data);
      loadSelf();
      loadAudit();
    }

    async function captureSelf(promote) {
      const payload = {
        title: document.getElementById("capture-title").value,
        description: document.getElementById("capture-description").value,
        kind: document.getElementById("capture-kind").value,
        promote,
        model: document.getElementById("capture-model").value,
      };
      const data = await api("/api/self/capture", {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("self-mine-output", data);
      loadSelf();
      loadQueue();
      loadAudit();
    }

    async function loadSocial() {
      const data = await api("/api/social/recent");
      setText("social-output", data);
    }

    async function loadConnectors() {
      const data = await api("/api/social/connectors");
      setText("social-connectors-output", data);
    }

    async function loadCommitments() {
      const data = await api("/api/commitments");
      setText("commitments-output", data);
    }

    async function loadCommitmentSummary() {
      const data = await api("/api/commitments/summary");
      setText("commitments-summary-output", data);
    }

    async function scanCommitments() {
      const data = await api("/api/commitments/scan", {method:"POST"});
      setText("commitments-summary-output", data);
      loadCommitments();
      loadCommitmentSummary();
      loadQueue();
      loadAudit();
    }

    async function simulateSocial() {
      const payload = {
        channel: document.getElementById("social-channel").value,
        thread_id: document.getElementById("social-thread").value,
        sender_id: document.getElementById("social-sender-id").value,
        sender_name: document.getElementById("social-sender-name").value,
        text: document.getElementById("social-text").value
      };
      const data = await api("/api/social/inbound", {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("social-output", data);
      loadSocial();
      loadCommitments();
      loadCommitmentSummary();
      loadQueue();
      loadAudit();
    }

    async function updateCommitmentStatus() {
      const payload = {
        id: document.getElementById("commitment-id").value,
        status: document.getElementById("commitment-status").value,
      };
      const data = await api("/api/commitments/status", {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("commitments-output", data);
      loadCommitments();
      loadCommitmentSummary();
      loadAudit();
    }

    async function loadAudit() {
      const data = await api("/api/audit");
      setText("audit-output", data);
    }

    async function retryQueue() {
      const payload = { id: document.getElementById("queue-retry-id").value };
      const data = await api("/api/queue/retry", {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("queue-action-output", data);
      setText("audit-output", data);
      loadQueue();
      loadAudit();
    }

    loadConfig();
    loadStatus();
    loadSessions();
    loadQueue();
    loadSelf();
    loadSocial();
    loadConnectors();
    loadCommitments();
    loadCommitmentSummary();
    loadAudit();
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
