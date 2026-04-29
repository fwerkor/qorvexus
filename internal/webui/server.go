package webui

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"os"
	"strconv"
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
	ListPlans(ctx context.Context, limit int, status string) (string, error)
	GetPlan(ctx context.Context, planID string) (string, error)
	AdvancePlan(ctx context.Context, planID string, limit int) (string, error)
	MineSelfImprovements(ctx context.Context, limit int) (string, error)
	CaptureSelfImprovement(ctx context.Context, title string, description string, kind string, promote bool, model string) (string, error)
	LoadConfigText() (string, error)
	SocialWebhookAdapters() []social.WebhookAdapter
	SaveConfigText(raw string) error
	HandleSocialEnvelope(ctx context.Context, env social.Envelope) (string, error)
	RetryQueueTask(ctx context.Context, id string) (string, error)
	UpdateCommitmentStatus(ctx context.Context, id string, status string) (string, error)
	UpdateSelfImprovementStatus(ctx context.Context, id string, status string) (string, error)
	RequestRuntimeRestart(ctx context.Context, reason string) (string, error)
	ApplySelfUpdate(ctx context.Context, runTests bool, reason string) (string, error)
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
	RuntimeMode              string    `json:"runtime_mode"`
	RuntimeApplyEnabled      bool      `json:"runtime_apply_enabled"`
	ExecutablePath           string    `json:"executable_path,omitempty"`
	SourceRoot               string    `json:"source_root,omitempty"`
	OwnerOnboardingRequired  bool      `json:"owner_onboarding_required"`
	OwnerOnboardingSessionID string    `json:"owner_onboarding_session_id,omitempty"`
	OwnerOnboardingPrompt    string    `json:"owner_onboarding_prompt,omitempty"`
}

type Server struct {
	app  App
	tmpl *template.Template
}

func NewServer(app App) (*Server, error) {
	tmpl, err := template.New("dashboard").Parse(controlPanelHTML)
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
	mux.HandleFunc("/api/plans", s.handlePlans)
	mux.HandleFunc("/api/plans/view", s.handlePlanView)
	mux.HandleFunc("/api/plans/advance", s.handlePlanAdvance)
	mux.HandleFunc("/api/self/mine", s.handleSelfMine)
	mux.HandleFunc("/api/self/capture", s.handleSelfCapture)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/social/inbound", s.handleSocialInbound)
	mux.HandleFunc("/api/queue/retry", s.handleQueueRetry)
	mux.HandleFunc("/api/commitments/status", s.handleCommitmentStatus)
	mux.HandleFunc("/api/self/status", s.handleSelfStatus)
	mux.HandleFunc("/api/runtime/control", s.handleRuntimeControl)
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

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	raw, err := s.app.ListPlans(ownerContext(r.Context()), limit, strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handlePlanView(w http.ResponseWriter, r *http.Request) {
	planID := strings.TrimSpace(r.URL.Query().Get("id"))
	if planID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	raw, err := s.app.GetPlan(ownerContext(r.Context()), planID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handlePlanAdvance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw, err := s.app.AdvancePlan(ownerContext(r.Context()), input.ID, input.Limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "channel": adapter.Name()})
	go func() {
		_, _ = s.app.HandleSocialEnvelope(context.Background(), env)
	}()
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

func (s *Server) handleRuntimeControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Action   string `json:"action"`
		RunTests bool   `json:"run_tests"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var (
		out string
		err error
	)
	switch strings.TrimSpace(strings.ToLower(input.Action)) {
	case "restart":
		out, err = s.app.RequestRuntimeRestart(ownerContext(r.Context()), input.Reason)
	case "apply", "build":
		out, err = s.app.ApplySelfUpdate(ownerContext(r.Context()), input.RunTests, input.Reason)
	default:
		http.Error(w, "unsupported runtime action", http.StatusBadRequest)
		return
	}
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

func LoadConfigText(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
