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

	"qorvexus/internal/config"
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
	if _, err := config.ParseRaw(path, []byte(raw)); err != nil {
		return err
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
    .lang-switch {
      display:flex;
      gap:8px;
      margin-top:16px;
    }
    .lang-switch button {
      padding:8px 12px;
      border-radius:999px;
      font-size:13px;
    }
    .lang-switch button.active {
      background:var(--accent-strong);
      color:#fff;
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
        <span class="eyebrow" data-i18n="eyebrow">Qorvexus Console</span>
        <h1 data-i18n="hero_title">Operate the agent without fighting the interface.</h1>
        <p class="lede" data-i18n="hero_lede">The command center stays focused on the things you actually need first: run work, inspect memory, watch queue health, and only then drop into social, self-improvement, or raw config.</p>
        <div class="lang-switch">
          <button id="lang-zh-CN" class="secondary" onclick="setLanguage('zh-CN')">中文</button>
          <button id="lang-en" class="secondary" onclick="setLanguage('en')">English</button>
        </div>
        <div class="chip-row">
          <span class="chip"><strong data-i18n="chip_default_model">Default model</strong> {{.DefaultModel}}</span>
          <span class="chip"><strong data-i18n="chip_memory">Memory</strong> {{.MemoryEnabled}}</span>
          <span class="chip"><strong data-i18n="chip_queue">Queue</strong> {{.QueueEnabled}}</span>
          <span class="chip"><strong data-i18n="chip_scheduler">Scheduler</strong> {{.SchedulerEnabled}}</span>
          <span class="chip"><strong data-i18n="chip_self">Self</strong> {{.SelfEnabled}}</span>
          <span class="chip"><strong data-i18n="chip_social">Social</strong> {{.SocialEnabled}}</span>
        </div>
      </div>
      <div class="panel hero-side">
        <div class="status-card">
          <div class="metric-label" data-i18n="runtime">Runtime</div>
          <div class="metric-value mono">{{.WebAddress}}</div>
          <p class="muted" style="margin-top:8px;"><span data-i18n="started_at">Started at</span> {{.StartedAt}}</p>
        </div>
        <div class="metric-row">
          <div class="metric">
            <div class="metric-label" data-i18n="default_session">Default Session</div>
            <div class="metric-value mono" id="hero-session">{{if .OwnerOnboardingRequired}}{{.OwnerOnboardingSessionID}}{{else}}ad-hoc{{end}}</div>
          </div>
          <div class="metric">
            <div class="metric-label" data-i18n="primary_focus">Primary Focus</div>
            <div class="metric-value" id="hero-focus" data-i18n-dynamic="{{if .OwnerOnboardingRequired}}focus_onboarding{{else}}focus_operations{{end}}">{{if .OwnerOnboardingRequired}}Onboarding{{else}}Operations{{end}}</div>
          </div>
        </div>
        {{if .OwnerOnboardingRequired}}
        <div class="alert">
          <strong data-i18n="owner_onboarding_needed">Owner onboarding still needs a reply.</strong>
          <p class="muted"><span data-i18n="continue_in_session">Continue in session</span> <span class="mono">{{.OwnerOnboardingSessionID}}</span>.</p>
          <pre>{{.OwnerOnboardingPrompt}}</pre>
        </div>
        {{else}}
        <div class="status-card">
          <div class="metric-label" data-i18n="status">Status</div>
          <p class="muted" data-i18n="owner_profile_present">Owner profile is already present, so new sessions can start with remembered identity, rules, and preferences.</p>
        </div>
        {{end}}
      </div>
    </section>

    <section class="layout">
      <aside class="panel sidebar">
        <h3 data-i18n="workspace">Workspace</h3>
        <p class="muted" style="margin-bottom:12px;" data-i18n="workspace_hint">Keep one surface visible at a time.</p>
        <div class="nav">
          <button id="tab-button-overview" class="active" onclick="showTab('overview')" data-i18n="tab_overview">Overview</button>
          <button id="tab-button-operations" onclick="showTab('operations')" data-i18n="tab_operations">Operations</button>
          <button id="tab-button-memory" onclick="showTab('memory')" data-i18n="tab_memory">Memory</button>
          <button id="tab-button-social" onclick="showTab('social')" data-i18n="tab_social">Social</button>
          <button id="tab-button-config" onclick="showTab('config')" data-i18n="tab_config">Config</button>
        </div>
      </aside>

      <main class="workspace">
        <section id="tab-overview" class="tab-pane active">
          <div class="overview-grid">
            <article class="panel card span-7">
              <div class="card-head">
                <div>
                  <h2 data-i18n="quick_run">Quick Run</h2>
                  <p data-i18n="quick_run_desc">Run a prompt immediately without hunting through the rest of the dashboard.</p>
                </div>
              </div>
              <div class="split-row">
                <div>
                  <label for="run-model" data-i18n="model_override">Model Override</label>
                  <input id="run-model" placeholder="Optional" data-i18n-placeholder="optional">
                </div>
                <div>
                  <label for="run-session" data-i18n="session_id">Session ID</label>
                  <input id="run-session" placeholder="Optional" data-i18n-placeholder="optional">
                </div>
              </div>
              <div style="margin-top:14px;">
                <label for="run-prompt" data-i18n="prompt">Prompt</label>
                <textarea id="run-prompt" class="prompt-box" placeholder="Ask Qorvexus to do something concrete..." data-i18n-placeholder="run_prompt_placeholder"></textarea>
              </div>
              <div class="button-row">
                <button onclick="runPrompt()" data-i18n="run_prompt">Run Prompt</button>
                <button class="secondary" onclick="prefillOnboarding()" data-i18n="use_onboarding_session">Use Onboarding Session</button>
                <button class="ghost" onclick="showTab('memory')" data-i18n="search_memory_instead">Search Memory Instead</button>
              </div>
            </article>

            <article class="panel card span-5">
              <div class="card-head">
                <div>
                  <h2 data-i18n="immediate_context">Immediate Context</h2>
                  <p data-i18n="immediate_context_desc">What the runtime thinks is important right now.</p>
                </div>
              </div>
              <div class="mini-grid">
                <div class="metric">
                  <div class="metric-label" data-i18n="current_tab">Current Tab</div>
                  <div class="metric-value" id="current-tab-label" data-i18n-dynamic="tab_overview">Overview</div>
                </div>
                <div class="metric">
                  <div class="metric-label" data-i18n="web_address">Web Address</div>
                  <div class="metric-value mono">{{.WebAddress}}</div>
                </div>
              </div>
              <div style="margin-top:14px;">
                <label data-i18n="run_output">Run Output</label>
                <pre id="run-output" class="result" {{if not .OwnerOnboardingRequired}}data-i18n="run_output_ready"{{end}}>{{if .OwnerOnboardingRequired}}{{.OwnerOnboardingPrompt}}{{else}}Ready. Run a prompt or open another workspace tab.{{end}}</pre>
              </div>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2 data-i18n="system_snapshot">System Snapshot</h2>
                  <p data-i18n="system_snapshot_desc">High-signal runtime state without dropping you straight into raw JSON.</p>
                </div>
                <div class="button-row" style="margin-top:0;">
                  <button class="secondary" onclick="loadStatus()" data-i18n="refresh_status">Refresh Status</button>
                  <button class="secondary" onclick="loadSessions()" data-i18n="refresh_sessions">Refresh Sessions</button>
                  <button class="secondary" onclick="loadQueue()" data-i18n="refresh_queue">Refresh Queue</button>
                </div>
              </div>
              <div class="overview-grid">
                <div class="span-4 stack">
                  <div>
                    <label data-i18n="sessions">Sessions</label>
                    <div id="sessions"></div>
                  </div>
                </div>
                <div class="span-4 stack">
                  <div>
                    <label data-i18n="queue">Queue</label>
                    <div id="queue"></div>
                  </div>
                </div>
                <div class="span-4 stack">
                  <div>
                    <label data-i18n="raw_status">Raw Status</label>
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
                  <h2 data-i18n="queue_recovery">Queue Recovery</h2>
                  <p data-i18n="queue_recovery_desc">Retry a failed task without digging through raw queue output first.</p>
                </div>
              </div>
              <label for="queue-retry-id" data-i18n="queue_task_id">Queue Task ID</label>
              <input id="queue-retry-id" placeholder="task-..." data-i18n-placeholder="queue_task_id_placeholder">
              <div class="button-row">
                <button onclick="retryQueue()" data-i18n="retry_task">Retry Task</button>
                <button class="secondary" onclick="loadQueue()" data-i18n="refresh_queue">Refresh Queue</button>
              </div>
              <pre id="queue-action-output" class="compact" data-i18n="retry_results_here">Retry results will appear here.</pre>
            </article>

            <article class="panel card span-6">
              <div class="card-head">
                <div>
                  <h2 data-i18n="commitments">Commitments</h2>
                  <p data-i18n="commitments_desc">Review due work, run a scan, and update commitment status from one place.</p>
                </div>
              </div>
              <div class="button-row">
                <button class="secondary" onclick="loadCommitments()" data-i18n="refresh_commitments">Refresh Commitments</button>
                <button class="secondary" onclick="loadCommitmentSummary()" data-i18n="refresh_summary">Refresh Summary</button>
                <button onclick="scanCommitments()" data-i18n="run_scan">Run Scan</button>
              </div>
              <div style="margin-top:14px;" class="split-row">
                <div>
                  <label for="commitment-id" data-i18n="commitment_id">Commitment ID</label>
                  <input id="commitment-id" placeholder="commitment-..." data-i18n-placeholder="commitment_id_placeholder">
                </div>
                <div>
                  <label for="commitment-status" data-i18n="new_status">New Status</label>
                  <input id="commitment-status" placeholder="open / done / blocked" data-i18n-placeholder="new_status_placeholder">
                </div>
              </div>
              <div class="button-row">
                <button onclick="updateCommitmentStatus()" data-i18n="update_commitment">Update Commitment</button>
              </div>
            </article>

            <article class="panel card span-6">
              <label data-i18n="commitment_summary">Commitment Summary</label>
              <pre id="commitments-summary-output" class="compact"></pre>
            </article>

            <article class="panel card span-6">
              <label data-i18n="commitment_feed">Commitment Feed</label>
              <pre id="commitments-output" class="compact"></pre>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2 data-i18n="audit_trail">Audit Trail</h2>
                  <p data-i18n="audit_trail_desc">See the effects of retries, scans, and status updates without mixing them into every other card.</p>
                </div>
                <button class="secondary" onclick="loadAudit()" data-i18n="refresh_audit">Refresh Audit</button>
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
                  <h2 data-i18n="memory_explorer">Memory Explorer</h2>
                  <p data-i18n="memory_explorer_desc">Search long-term memory directly instead of scrolling through unrelated widgets.</p>
                </div>
              </div>
              <label for="memory-query" data-i18n="search_query">Search Query</label>
              <input id="memory-query" placeholder="owner, timezone, project, preferences..." data-i18n-placeholder="memory_query_placeholder">
              <div class="button-row">
                <button onclick="loadMemory()" data-i18n="search_memory">Search Memory</button>
                <button class="secondary" onclick="document.getElementById('memory-query').value='owner'; loadMemory();" data-i18n="owner_profile">Owner Profile</button>
                <button class="secondary" onclick="document.getElementById('memory-query').value='project'; loadMemory();" data-i18n="projects">Projects</button>
              </div>
              <pre id="memory-output" class="result" data-i18n="memory_output_ready">Run a memory search to inspect what the agent remembers.</pre>
            </article>

            <article class="panel card span-7">
              <div class="card-head">
                <div>
                  <h2 data-i18n="self_evolution">Self Evolution</h2>
                  <p data-i18n="self_evolution_desc">Keep backlog review and new idea capture together, without crowding the main workflow.</p>
                </div>
              </div>
              <div class="button-row">
                <button class="secondary" onclick="loadSelf()" data-i18n="refresh_backlog">Refresh Backlog</button>
                <button class="secondary" onclick="mineSelf()" data-i18n="mine_ideas">Mine Ideas</button>
              </div>
              <div class="split-row" style="margin-top:14px;">
                <div>
                  <label data-i18n="backlog">Backlog</label>
                  <pre id="self-output" class="compact"></pre>
                </div>
                <div>
                  <label data-i18n="mined_ideas">Mined Ideas</label>
                  <pre id="self-mine-output" class="compact"></pre>
                </div>
              </div>
              <div class="split-row" style="margin-top:14px;">
                <div>
                  <label for="self-id" data-i18n="backlog_id">Backlog ID</label>
                  <input id="self-id" placeholder="backlog item id" data-i18n-placeholder="backlog_id_placeholder">
                </div>
                <div>
                  <label for="self-status" data-i18n="new_status">New Status</label>
                  <input id="self-status" placeholder="planned / active / done" data-i18n-placeholder="self_status_placeholder">
                </div>
              </div>
              <div class="button-row">
                <button onclick="updateSelfStatus()" data-i18n="update_self_status">Update Self Status</button>
              </div>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2 data-i18n="capture_improvement">Capture Improvement</h2>
                  <p data-i18n="capture_improvement_desc">Turn a good idea into backlog state, and optionally promote it into queued execution.</p>
                </div>
              </div>
              <div class="split-row">
                <div>
                  <label for="capture-title" data-i18n="title">Title</label>
                  <input id="capture-title" placeholder="Captured idea title" data-i18n-placeholder="capture_title_placeholder">
                </div>
                <div>
                  <label for="capture-kind" data-i18n="kind">Kind</label>
                  <input id="capture-kind" placeholder="reliability / automation / safety" data-i18n-placeholder="capture_kind_placeholder">
                </div>
                <div>
                  <label for="capture-model" data-i18n="promotion_model">Promotion Model</label>
                  <input id="capture-model" placeholder="optional" data-i18n-placeholder="optional">
                </div>
              </div>
              <div style="margin-top:14px;">
                <label for="capture-description" data-i18n="description">Description</label>
                <textarea id="capture-description" placeholder="Describe the improvement you want Qorvexus to remember or work on." data-i18n-placeholder="capture_description_placeholder"></textarea>
              </div>
              <div class="button-row">
                <button onclick="captureSelf(false)" data-i18n="capture_idea">Capture Idea</button>
                <button class="secondary" onclick="captureSelf(true)" data-i18n="capture_and_promote">Capture And Promote</button>
              </div>
            </article>
          </div>
        </section>

        <section id="tab-social" class="tab-pane">
          <div class="social-grid">
            <article class="panel card span-5">
              <div class="card-head">
                <div>
                  <h2 data-i18n="connectors">Connectors</h2>
                  <p data-i18n="connectors_desc">Quick visibility into available social channels.</p>
                </div>
                <button class="secondary" onclick="loadConnectors()" data-i18n="refresh_connectors">Refresh Connectors</button>
              </div>
              <pre id="social-connectors-output" class="compact"></pre>
            </article>

            <article class="panel card span-7">
              <div class="card-head">
                <div>
                  <h2 data-i18n="recent_social_activity">Recent Social Activity</h2>
                  <p data-i18n="recent_social_activity_desc">Inspect inbound and outbound history without mixing it with config or memory tools.</p>
                </div>
                <button class="secondary" onclick="loadSocial()" data-i18n="refresh_social_log">Refresh Social Log</button>
              </div>
              <pre id="social-output" class="compact"></pre>
            </article>

            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2 data-i18n="simulate_inbound_message">Simulate Inbound Message</h2>
                  <p data-i18n="simulate_inbound_message_desc">Test a connector flow from one compact form.</p>
                </div>
              </div>
              <div class="split-row">
                <div>
                  <label for="social-channel" data-i18n="channel">Channel</label>
                  <input id="social-channel" placeholder="telegram">
                </div>
                <div>
                  <label for="social-thread" data-i18n="thread_id">Thread ID</label>
                  <input id="social-thread" placeholder="chat-1">
                </div>
                <div>
                  <label for="social-sender-id" data-i18n="sender_id">Sender ID</label>
                  <input id="social-sender-id" placeholder="user-1">
                </div>
                <div>
                  <label for="social-sender-name" data-i18n="sender_name">Sender Name</label>
                  <input id="social-sender-name" placeholder="Alice">
                </div>
              </div>
              <div style="margin-top:14px;">
                <label for="social-text" data-i18n="inbound_text">Inbound Text</label>
                <textarea id="social-text" placeholder="Paste the incoming message here." data-i18n-placeholder="social_text_placeholder"></textarea>
              </div>
              <div class="button-row">
                <button onclick="simulateSocial()" data-i18n="simulate_inbound_social">Simulate Inbound Social</button>
              </div>
            </article>
          </div>
        </section>

        <section id="tab-config" class="tab-pane">
          <div class="data-grid">
            <article class="panel card span-12">
              <div class="card-head">
                <div>
                  <h2 data-i18n="runtime_config">Runtime Config</h2>
                  <p data-i18n="runtime_config_desc">Raw configuration is available, but it now lives in its own dedicated workspace instead of crowding the main flow.</p>
                </div>
                <div class="button-row" style="margin-top:0;">
                  <button class="secondary" onclick="loadConfig()" data-i18n="reload">Reload</button>
                  <button onclick="saveConfig()" data-i18n="save_config">Save Config</button>
                </div>
              </div>
              <textarea id="config-text" style="min-height:520px;" placeholder="Loading runtime config..." data-i18n-placeholder="loading_runtime_config"></textarea>
            </article>
          </div>
        </section>
      </main>
    </section>
  </div>
  <script>
    const translations = {
      "en": {
        title: "Qorvexus Console",
        eyebrow: "Qorvexus Console",
        hero_title: "Operate the agent without fighting the interface.",
        hero_lede: "The command center stays focused on the things you actually need first: run work, inspect memory, watch queue health, and only then drop into social, self-improvement, or raw config.",
        chip_default_model: "Default model",
        chip_memory: "Memory",
        chip_queue: "Queue",
        chip_scheduler: "Scheduler",
        chip_self: "Self",
        chip_social: "Social",
        runtime: "Runtime",
        started_at: "Started at",
        default_session: "Default Session",
        primary_focus: "Primary Focus",
        focus_onboarding: "Onboarding",
        focus_operations: "Operations",
        owner_onboarding_needed: "Owner onboarding still needs a reply.",
        continue_in_session: "Continue in session",
        status: "Status",
        owner_profile_present: "Owner profile is already present, so new sessions can start with remembered identity, rules, and preferences.",
        workspace: "Workspace",
        workspace_hint: "Keep one surface visible at a time.",
        tab_overview: "Overview",
        tab_operations: "Operations",
        tab_memory: "Memory",
        tab_social: "Social",
        tab_config: "Config",
        quick_run: "Quick Run",
        quick_run_desc: "Run a prompt immediately without hunting through the rest of the dashboard.",
        model_override: "Model Override",
        session_id: "Session ID",
        prompt: "Prompt",
        optional: "Optional",
        run_prompt_placeholder: "Ask Qorvexus to do something concrete...",
        run_prompt: "Run Prompt",
        use_onboarding_session: "Use Onboarding Session",
        search_memory_instead: "Search Memory Instead",
        immediate_context: "Immediate Context",
        immediate_context_desc: "What the runtime thinks is important right now.",
        current_tab: "Current Tab",
        web_address: "Web Address",
        run_output: "Run Output",
        run_output_ready: "Ready. Run a prompt or open another workspace tab.",
        system_snapshot: "System Snapshot",
        system_snapshot_desc: "High-signal runtime state without dropping you straight into raw JSON.",
        refresh_status: "Refresh Status",
        refresh_sessions: "Refresh Sessions",
        refresh_queue: "Refresh Queue",
        sessions: "Sessions",
        queue: "Queue",
        raw_status: "Raw Status",
        queue_recovery: "Queue Recovery",
        queue_recovery_desc: "Retry a failed task without digging through raw queue output first.",
        queue_task_id: "Queue Task ID",
        queue_task_id_placeholder: "task-...",
        retry_task: "Retry Task",
        retry_results_here: "Retry results will appear here.",
        commitments: "Commitments",
        commitments_desc: "Review due work, run a scan, and update commitment status from one place.",
        refresh_commitments: "Refresh Commitments",
        refresh_summary: "Refresh Summary",
        run_scan: "Run Scan",
        commitment_id: "Commitment ID",
        commitment_id_placeholder: "commitment-...",
        new_status: "New Status",
        new_status_placeholder: "open / done / blocked",
        update_commitment: "Update Commitment",
        commitment_summary: "Commitment Summary",
        commitment_feed: "Commitment Feed",
        audit_trail: "Audit Trail",
        audit_trail_desc: "See the effects of retries, scans, and status updates without mixing them into every other card.",
        refresh_audit: "Refresh Audit",
        memory_explorer: "Memory Explorer",
        memory_explorer_desc: "Search long-term memory directly instead of scrolling through unrelated widgets.",
        search_query: "Search Query",
        memory_query_placeholder: "owner, timezone, project, preferences...",
        search_memory: "Search Memory",
        owner_profile: "Owner Profile",
        projects: "Projects",
        memory_output_ready: "Run a memory search to inspect what the agent remembers.",
        self_evolution: "Self Evolution",
        self_evolution_desc: "Keep backlog review and new idea capture together, without crowding the main workflow.",
        refresh_backlog: "Refresh Backlog",
        mine_ideas: "Mine Ideas",
        backlog: "Backlog",
        mined_ideas: "Mined Ideas",
        backlog_id: "Backlog ID",
        backlog_id_placeholder: "backlog item id",
        self_status_placeholder: "planned / active / done",
        update_self_status: "Update Self Status",
        capture_improvement: "Capture Improvement",
        capture_improvement_desc: "Turn a good idea into backlog state, and optionally promote it into queued execution.",
        title: "Title",
        capture_title_placeholder: "Captured idea title",
        kind: "Kind",
        capture_kind_placeholder: "reliability / automation / safety",
        promotion_model: "Promotion Model",
        description: "Description",
        capture_description_placeholder: "Describe the improvement you want Qorvexus to remember or work on.",
        capture_idea: "Capture Idea",
        capture_and_promote: "Capture And Promote",
        connectors: "Connectors",
        connectors_desc: "Quick visibility into available social channels.",
        refresh_connectors: "Refresh Connectors",
        recent_social_activity: "Recent Social Activity",
        recent_social_activity_desc: "Inspect inbound and outbound history without mixing it with config or memory tools.",
        refresh_social_log: "Refresh Social Log",
        simulate_inbound_message: "Simulate Inbound Message",
        simulate_inbound_message_desc: "Test a connector flow from one compact form.",
        channel: "Channel",
        thread_id: "Thread ID",
        sender_id: "Sender ID",
        sender_name: "Sender Name",
        inbound_text: "Inbound Text",
        social_text_placeholder: "Paste the incoming message here.",
        simulate_inbound_social: "Simulate Inbound Social",
        runtime_config: "Runtime Config",
        runtime_config_desc: "Raw configuration is available, but it now lives in its own dedicated workspace instead of crowding the main flow.",
        reload: "Reload",
        save_config: "Save Config",
        loading_runtime_config: "Loading runtime config...",
        config_empty: "# Runtime config loaded, but the file is empty.\n",
        config_load_failed: "# Failed to load runtime config.\n# ",
        config_saved: "Config saved."
      },
      "zh-CN": {
        title: "Qorvexus 控制台",
        eyebrow: "Qorvexus 控制台",
        hero_title: "以更顺手的方式操作智能体，而不是和界面较劲。",
        hero_lede: "这个控制台优先展示你真正常用的事情：运行任务、查看记忆、观察队列健康，再进入社交、自我进化或原始配置。",
        chip_default_model: "默认模型",
        chip_memory: "记忆",
        chip_queue: "队列",
        chip_scheduler: "调度",
        chip_self: "自我进化",
        chip_social: "社交",
        runtime: "运行时",
        started_at: "启动时间",
        default_session: "默认会话",
        primary_focus: "当前重点",
        focus_onboarding: "主人引导",
        focus_operations: "运行操作",
        owner_onboarding_needed: "主人 onboarding 仍然需要回复。",
        continue_in_session: "请在该会话中继续",
        status: "状态",
        owner_profile_present: "主人档案已经存在，新会话会自动带入已记住的身份、规则与偏好。",
        workspace: "工作区",
        workspace_hint: "一次只专注一个界面区域。",
        tab_overview: "总览",
        tab_operations: "运维",
        tab_memory: "记忆",
        tab_social: "社交",
        tab_config: "配置",
        quick_run: "快速运行",
        quick_run_desc: "直接执行 prompt，不必在整页内容里来回找入口。",
        model_override: "模型覆盖",
        session_id: "会话 ID",
        prompt: "提示词",
        optional: "可选",
        run_prompt_placeholder: "给 Qorvexus 一个明确的任务……",
        run_prompt: "运行 Prompt",
        use_onboarding_session: "使用 Onboarding 会话",
        search_memory_instead: "改为搜索记忆",
        immediate_context: "即时上下文",
        immediate_context_desc: "当前运行时认为最重要的信息。",
        current_tab: "当前标签页",
        web_address: "Web 地址",
        run_output: "运行结果",
        run_output_ready: "已就绪。现在可以运行一个 prompt，或切换到其它工作区。",
        system_snapshot: "系统快照",
        system_snapshot_desc: "直接查看高价值运行状态，而不是一上来就面对整块原始 JSON。",
        refresh_status: "刷新状态",
        refresh_sessions: "刷新会话",
        refresh_queue: "刷新队列",
        sessions: "会话",
        queue: "队列",
        raw_status: "原始状态",
        queue_recovery: "队列恢复",
        queue_recovery_desc: "无需先看完整队列输出，就可以重试失败任务。",
        queue_task_id: "队列任务 ID",
        queue_task_id_placeholder: "task-...",
        retry_task: "重试任务",
        retry_results_here: "重试结果会显示在这里。",
        commitments: "承诺事项",
        commitments_desc: "在一个区域里查看待办承诺、执行扫描并更新状态。",
        refresh_commitments: "刷新承诺",
        refresh_summary: "刷新摘要",
        run_scan: "执行扫描",
        commitment_id: "承诺 ID",
        commitment_id_placeholder: "commitment-...",
        new_status: "新状态",
        new_status_placeholder: "open / done / blocked",
        update_commitment: "更新承诺",
        commitment_summary: "承诺摘要",
        commitment_feed: "承诺列表",
        audit_trail: "审计轨迹",
        audit_trail_desc: "把重试、扫描和状态更新的结果集中查看，不再打散在各个卡片里。",
        refresh_audit: "刷新审计",
        memory_explorer: "记忆浏览器",
        memory_explorer_desc: "直接搜索长期记忆，而不是在无关组件里滚动查找。",
        search_query: "搜索词",
        memory_query_placeholder: "owner、timezone、project、preferences……",
        search_memory: "搜索记忆",
        owner_profile: "主人档案",
        projects: "项目",
        memory_output_ready: "执行一次记忆搜索，查看智能体当前记住了什么。",
        self_evolution: "自我进化",
        self_evolution_desc: "把 backlog 审查和新想法录入放在一起，但不干扰主流程。",
        refresh_backlog: "刷新 Backlog",
        mine_ideas: "挖掘想法",
        backlog: "Backlog",
        mined_ideas: "挖掘结果",
        backlog_id: "Backlog ID",
        backlog_id_placeholder: "backlog item id",
        self_status_placeholder: "planned / active / done",
        update_self_status: "更新自我进化状态",
        capture_improvement: "记录改进项",
        capture_improvement_desc: "把一个好想法变成 backlog，必要时直接推进为排队执行。",
        title: "标题",
        capture_title_placeholder: "改进想法标题",
        kind: "类型",
        capture_kind_placeholder: "reliability / automation / safety",
        promotion_model: "推进模型",
        description: "描述",
        capture_description_placeholder: "描述你希望 Qorvexus 记住或推进的改进点。",
        capture_idea: "记录想法",
        capture_and_promote: "记录并推进",
        connectors: "连接器",
        connectors_desc: "快速查看当前可用的社交渠道。",
        refresh_connectors: "刷新连接器",
        recent_social_activity: "最近社交活动",
        recent_social_activity_desc: "查看最近的收发内容，而不和配置或记忆工具混在一起。",
        refresh_social_log: "刷新社交日志",
        simulate_inbound_message: "模拟入站消息",
        simulate_inbound_message_desc: "用一个紧凑表单测试连接器处理流程。",
        channel: "渠道",
        thread_id: "线程 ID",
        sender_id: "发送者 ID",
        sender_name: "发送者名称",
        inbound_text: "入站文本",
        social_text_placeholder: "把收到的消息粘贴到这里。",
        simulate_inbound_social: "模拟入站社交消息",
        runtime_config: "运行时配置",
        runtime_config_desc: "原始配置依然可用，但它现在在单独工作区里，不再挤占主流程。",
        reload: "重新加载",
        save_config: "保存配置",
        loading_runtime_config: "正在加载运行时配置……",
        config_empty: "# 运行时配置已加载，但文件为空。\n",
        config_load_failed: "# 加载运行时配置失败。\n# ",
        config_saved: "配置已保存。"
      }
    };

    let currentLanguage = "en";

    async function api(path, options) {
      const res = await fetch(path, options);
      if (!res.ok) throw new Error(await res.text());
      const type = res.headers.get("content-type") || "";
      return type.includes("application/json") ? res.json() : res.text();
    }

    function apiPath(path) {
      return path.startsWith("./") ? path : "./" + path.replace(/^\/+/, "");
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

    function t(key) {
      return translations[currentLanguage]?.[key] || translations.en[key] || key;
    }

    function applyTranslations() {
      document.documentElement.lang = currentLanguage;
      document.title = t("title");
      for (const node of document.querySelectorAll("[data-i18n]")) {
        node.textContent = t(node.dataset.i18n);
      }
      for (const node of document.querySelectorAll("[data-i18n-placeholder]")) {
        node.placeholder = t(node.dataset.i18nPlaceholder);
      }
      for (const node of document.querySelectorAll("[data-i18n-dynamic]")) {
        node.textContent = t(node.dataset.i18nDynamic);
      }
      for (const code of ["zh-CN", "en"]) {
        document.getElementById("lang-" + code)?.classList.toggle("active", code === currentLanguage);
      }
    }

    function preferredLanguage() {
      const stored = localStorage.getItem("qorvexus_lang");
      if (stored && translations[stored]) return stored;
      const browser = navigator.language || "en";
      if (browser.toLowerCase().startsWith("zh")) return "zh-CN";
      return "en";
    }

    function setLanguage(lang) {
      currentLanguage = translations[lang] ? lang : "en";
      localStorage.setItem("qorvexus_lang", currentLanguage);
      applyTranslations();
      syncDynamicLabels();
    }

    function syncDynamicLabels() {
      const currentTab = document.querySelector(".nav button.active")?.id?.replace("tab-button-", "") || "overview";
      setText("current-tab-label", t("tab_" + currentTab));
      const heroFocus = document.getElementById("hero-focus");
      if (heroFocus?.dataset.i18nDynamic) {
        heroFocus.textContent = t(heroFocus.dataset.i18nDynamic);
      }
    }

    function showTab(name) {
      for (const pane of document.querySelectorAll(".tab-pane")) {
        pane.classList.toggle("active", pane.id === "tab-" + name);
      }
      for (const button of document.querySelectorAll(".nav button")) {
        button.classList.toggle("active", button.id === "tab-button-" + name);
      }
      setText("current-tab-label", t("tab_" + name));
      if (name === "config") {
        loadConfig();
      }
    }

    function prefillOnboarding() {
      loadStatus().then(() => showTab("overview"));
    }

    async function loadConfig() {
      const field = document.getElementById("config-text");
      field.value = t("loading_runtime_config");
      try {
        const data = await api(apiPath("api/config"));
        field.value = data.config || "";
        if (!field.value.trim()) {
          field.value = t("config_empty");
        }
      } catch (err) {
        field.value = t("config_load_failed") + (err?.message || String(err));
      }
    }

    async function saveConfig() {
      await api(apiPath("api/config"), {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify({config:document.getElementById("config-text").value})
      });
      alert(t("config_saved"));
    }

    async function loadStatus() {
      const data = await api(apiPath("api/status"));
      setText("status-output", data);
      if (data.owner_onboarding_required) {
        if (!document.getElementById("run-session").value) {
          document.getElementById("run-session").value = data.owner_onboarding_session_id || "";
        }
        if (data.owner_onboarding_prompt) {
          setText("run-output", data.owner_onboarding_prompt);
          setText("hero-session", data.owner_onboarding_session_id || "owner-onboarding");
          document.getElementById("hero-focus").dataset.i18nDynamic = "focus_onboarding";
          setText("hero-focus", t("focus_onboarding"));
        }
      } else {
        document.getElementById("hero-focus").dataset.i18nDynamic = "focus_operations";
        setText("hero-focus", t("focus_operations"));
      }
      return data;
    }

    async function runPrompt() {
      const payload = {
        prompt: document.getElementById("run-prompt").value,
        model: document.getElementById("run-model").value,
        session_id: document.getElementById("run-session").value,
      };
      const data = await api(apiPath("api/run"), {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("run-output", data.output);
      loadSessions();
      loadAudit();
    }

    async function loadSessions() {
      const data = await api(apiPath("api/sessions"));
      document.getElementById("sessions").innerHTML = renderTable(data, ["id","model","updated_at"]);
    }

    async function loadQueue() {
      const data = await api(apiPath("api/queue"));
      document.getElementById("queue").innerHTML = renderTable(data, ["id","status","name","created_at"]);
    }

    async function loadMemory() {
      const q = encodeURIComponent(document.getElementById("memory-query").value);
      const data = await api(apiPath("api/memory?q=" + q));
      setText("memory-output", data);
    }

    async function loadSelf() {
      const data = await api(apiPath("api/self"));
      setText("self-output", data);
    }

    async function mineSelf() {
      const data = await api(apiPath("api/self/mine"));
      setText("self-mine-output", data);
    }

    async function updateSelfStatus() {
      const payload = {
        id: document.getElementById("self-id").value,
        status: document.getElementById("self-status").value,
      };
      const data = await api(apiPath("api/self/status"), {
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
      const data = await api(apiPath("api/self/capture"), {
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
      const data = await api(apiPath("api/social/recent"));
      setText("social-output", data);
    }

    async function loadConnectors() {
      const data = await api(apiPath("api/social/connectors"));
      setText("social-connectors-output", data);
    }

    async function loadCommitments() {
      const data = await api(apiPath("api/commitments"));
      setText("commitments-output", data);
    }

    async function loadCommitmentSummary() {
      const data = await api(apiPath("api/commitments/summary"));
      setText("commitments-summary-output", data);
    }

    async function scanCommitments() {
      const data = await api(apiPath("api/commitments/scan"), {method:"POST"});
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
      const data = await api(apiPath("api/social/inbound"), {
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
      const data = await api(apiPath("api/commitments/status"), {
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
      const data = await api(apiPath("api/audit"));
      setText("audit-output", data);
    }

    async function retryQueue() {
      const payload = { id: document.getElementById("queue-retry-id").value };
      const data = await api(apiPath("api/queue/retry"), {
        method:"POST",
        headers:{"Content-Type":"application/json"},
        body:JSON.stringify(payload)
      });
      setText("queue-action-output", data);
      setText("audit-output", data);
      loadQueue();
      loadAudit();
    }

    setLanguage(preferredLanguage());
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
