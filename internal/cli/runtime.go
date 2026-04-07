package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qorvexus/internal/agent"
	"qorvexus/internal/audit"
	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/memory"
	"qorvexus/internal/model"
	"qorvexus/internal/orchestrator"
	"qorvexus/internal/policy"
	"qorvexus/internal/scheduler"
	"qorvexus/internal/self"
	"qorvexus/internal/session"
	"qorvexus/internal/skill"
	"qorvexus/internal/social"
	"qorvexus/internal/taskqueue"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
	"qorvexus/internal/webui"
)

type appRuntime struct {
	cfg        *config.Config
	configPath string
	runner     *agent.Runner
	discussion *orchestrator.Discussion
	scheduler  *scheduler.Manager
	sessions   *session.Store
	memory     *memory.Store
	queue      *taskqueue.Queue
	worker     *taskqueue.Worker
	webServer  *http.Server
	startedAt  time.Time
	social     *social.Gateway
	connectors *social.Registry
	self       *self.Manager
	audit      *audit.Logger
}

func newRuntime(cfg *config.Config, configPath string) (*appRuntime, error) {
	registry := model.NewRegistry()
	recorder := model.NewRecorder(filepath.Join(cfg.DataDir, "traces", "model_calls.jsonl"))
	for name, modelCfg := range cfg.Models {
		var client model.Client
		switch strings.ToLower(modelCfg.Provider) {
		case "openai", "openai-compatible", "":
			client = model.NewOpenAIClient(modelCfg)
		default:
			return nil, fmt.Errorf("unsupported provider %q for model %s", modelCfg.Provider, name)
		}
		registry.Register(name, modelCfg, recorder.Wrap(client))
	}

	skills, err := skill.NewLoader().LoadDirs(cfg.Skills.Dirs)
	if err != nil {
		return nil, err
	}

	store := session.NewStore(cfg.DataDir)
	discussion := &orchestrator.Discussion{Registry: registry}
	policyEngine := policy.NewEngine(cfg.Tools)
	app := &appRuntime{
		cfg:        cfg,
		configPath: configPath,
		discussion: discussion,
		sessions:   store,
		memory:     memory.NewStore(cfg.Memory.File),
		startedAt:  time.Now().UTC(),
		connectors: social.NewRegistry(),
		self:       self.NewManager(cfg.Self.SkillsDir, cfg.Self.BacklogFile),
		audit:      audit.New(cfg.Audit.File),
	}

	toolRegistry := tool.NewRegistry()
	toolRegistry.Register(&tool.ThinkTool{})
	toolRegistry.Register(tool.NewCommandTool(cfg.Tools, policyEngine))
	toolRegistry.Register(tool.NewHTTPTool(cfg.Tools))
	toolRegistry.Register(tool.NewPlaywrightTool(cfg.Tools))
	toolRegistry.Register(tool.NewSubAgentTool(app))
	toolRegistry.Register(tool.NewDiscussTool(app))
	toolRegistry.Register(tool.NewScheduleTool(app))
	toolRegistry.Register(tool.NewRememberTool(app))
	toolRegistry.Register(tool.NewRecallTool(app))
	toolRegistry.Register(tool.NewEnqueueTaskTool(app))
	toolRegistry.Register(tool.NewSocialSendTool(app))
	toolRegistry.Register(tool.NewSocialListTool(app))
	toolRegistry.Register(tool.NewReadConfigTool(app))
	toolRegistry.Register(tool.NewWriteConfigTool(app))
	toolRegistry.Register(tool.NewUpsertSkillTool(app))
	toolRegistry.Register(tool.NewSelfBacklogAddTool(app))
	toolRegistry.Register(tool.NewSelfBacklogListTool(app))
	toolRegistry.Register(tool.NewPromoteSelfImprovementTool(app))
	toolRegistry.Register(tool.NewMineSelfImprovementsTool(app))
	toolRegistry.Register(tool.NewCaptureSelfImprovementTool(app))

	app.runner = &agent.Runner{
		Config:   cfg,
		Models:   registry,
		Sessions: store,
		Tools:    toolRegistry,
		Skills:   skills,
		Compressor: &contextx.Compressor{
			Registry:        registry,
			SummarizerModel: cfg.Agent.SummarizerModel,
			MaxChars:        cfg.Agent.ContextWindowChars,
			Threshold:       cfg.Agent.CompressionThreshold,
		},
	}

	app.scheduler = scheduler.NewManager(cfg.Scheduler.TaskFile, app)
	_ = app.scheduler.Load()

	app.queue = taskqueue.New(cfg.Queue.File, app)
	_ = app.queue.Load()
	app.worker = &taskqueue.Worker{
		Queue:        app.queue,
		PollInterval: time.Duration(cfg.Queue.PollInterval) * time.Second,
	}
	app.social = social.NewGateway(cfg.Social, cfg.Identity, app)
	for _, channel := range cfg.Social.AllowedChannels {
		app.connectors.Register(social.NewFileConnector(channel, filepath.Join(cfg.DataDir, "social_outbox_"+channel+".jsonl")))
	}

	if cfg.Web.Enabled {
		panel, err := webui.NewServer(app)
		if err != nil {
			return nil, err
		}
		app.webServer = &http.Server{
			Addr:    cfg.Web.Address,
			Handler: panel.Handler(),
		}
	}

	return app, nil
}

func (a *appRuntime) RunSubAgent(ctx context.Context, name string, prompt string, model string) (string, error) {
	_, out, err := a.runner.Run(ctx, agent.Request{
		SessionID: fmt.Sprintf("subagent-%s-%d", sanitize(name), os.Getpid()),
		Model:     model,
		Prompt:    prompt,
	})
	return out, err
}

func (a *appRuntime) ConsultModels(ctx context.Context, prompt string, panel []string) (string, error) {
	return a.discussion.Run(ctx, prompt, panel, a.cfg.Agent.Discussion.SynthesisModel)
}

func (a *appRuntime) AddScheduledTask(_ context.Context, name string, scheduleExpr string, prompt string, model string) (string, error) {
	task := scheduler.Task{
		Name:     name,
		Schedule: scheduleExpr,
		Prompt:   prompt,
		Model:    model,
	}
	if err := a.scheduler.Add(task); err != nil {
		return "", err
	}
	a.logAudit(context.Background(), "schedule_task", "ok", name, map[string]any{"schedule": scheduleExpr, "model": model})
	return fmt.Sprintf("scheduled task %q with cron %q", name, scheduleExpr), nil
}

func (a *appRuntime) RunScheduled(_ context.Context, task scheduler.Task) error {
	_, err := a.queue.Add(taskqueue.Task{
		Name:      task.Name,
		Prompt:    task.Prompt,
		Model:     task.Model,
		SessionID: "cron-" + task.ID,
	})
	return err
}

func (a *appRuntime) Remember(_ context.Context, content string, tags []string, source string) (string, error) {
	if !a.cfg.Memory.Enabled {
		return "", fmt.Errorf("memory is disabled")
	}
	if err := a.memory.Append(memory.Entry{Content: content, Tags: tags, Source: source}); err != nil {
		return "", err
	}
	return "memory stored", nil
}

func (a *appRuntime) Recall(_ context.Context, query string, limit int) (string, error) {
	if !a.cfg.Memory.Enabled {
		return "", fmt.Errorf("memory is disabled")
	}
	results, err := a.memory.Search(query, limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) EnqueueTask(_ context.Context, name string, prompt string, model string, sessionID string) (string, error) {
	if !a.cfg.Queue.Enabled {
		return "", fmt.Errorf("queue is disabled")
	}
	task, err := a.queue.Add(taskqueue.Task{
		Name:      name,
		Prompt:    prompt,
		Model:     model,
		SessionID: sessionID,
	})
	if err != nil {
		return "", err
	}
	a.logAudit(context.Background(), "enqueue_task", "ok", task.ID, map[string]any{"name": name, "model": model})
	return fmt.Sprintf("queued task %q as %s", name, task.ID), nil
}

func (a *appRuntime) RunQueuedTask(ctx context.Context, task taskqueue.Task) (string, error) {
	sessionID := task.SessionID
	if sessionID == "" {
		sessionID = "queue-" + task.ID
	}
	_, out, err := a.runner.Run(ctx, agent.Request{
		SessionID: sessionID,
		Model:     task.Model,
		Prompt:    task.Prompt,
	})
	return out, err
}

func (a *appRuntime) Status() webui.Status {
	return webui.Status{
		StartedAt:        a.startedAt,
		DefaultModel:     a.cfg.Agent.DefaultModel,
		SchedulerEnabled: a.cfg.Scheduler.Enabled,
		QueueEnabled:     a.cfg.Queue.Enabled,
		MemoryEnabled:    a.cfg.Memory.Enabled,
		SelfEnabled:      a.cfg.Self.Enabled,
		SocialEnabled:    a.cfg.Social.Enabled,
		WebAddress:       a.cfg.Web.Address,
	}
}

func (a *appRuntime) RunPrompt(ctx context.Context, prompt string, model string, sessionID string) (string, error) {
	_, out, err := a.runner.Run(ctx, agent.Request{
		SessionID: sessionID,
		Model:     model,
		Prompt:    prompt,
		Context: &types.ConversationContext{
			Channel:  "web",
			SenderID: "owner",
			Trust:    types.TrustOwner,
			IsOwner:  true,
		},
	})
	return out, err
}

func (a *appRuntime) ListSessions() ([]session.State, error) {
	return a.sessions.List()
}

func (a *appRuntime) ListQueue() []taskqueue.Task {
	if a.queue == nil {
		return nil
	}
	return a.queue.List()
}

func (a *appRuntime) SearchMemory(query string, limit int) (string, error) {
	return a.Recall(context.Background(), query, limit)
}

func (a *appRuntime) LoadConfigText() (string, error) {
	return webui.LoadConfigText(a.configPath)
}

func (a *appRuntime) SaveConfigText(raw string) error {
	return webui.SaveConfigText(a.configPath, raw)
}

func (a *appRuntime) ReadRuntimeConfig(_ context.Context) (string, error) {
	return webui.LoadConfigText(a.configPath)
}

func (a *appRuntime) WriteRuntimeConfig(ctx context.Context, raw string) (string, error) {
	if !a.cfg.Self.Enabled || !a.cfg.Self.AllowConfigEdits {
		return "", fmt.Errorf("self config edits are disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("config edits require owner context")
	}
	if err := webui.SaveConfigText(a.configPath, raw); err != nil {
		return "", err
	}
	a.logAudit(ctx, "write_runtime_config", "ok", a.configPath, nil)
	return "runtime config updated", nil
}

func (a *appRuntime) UpsertSkill(ctx context.Context, name string, description string, body string) (string, error) {
	if !a.cfg.Self.Enabled || !a.cfg.Self.AllowSkillWrites {
		return "", fmt.Errorf("self skill writes are disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("skill writes require owner context")
	}
	path, err := a.self.UpsertSkill(name, description, body)
	if err != nil {
		return "", err
	}
	a.logAudit(ctx, "upsert_skill", "ok", path, map[string]any{"skill": name})
	return path, nil
}

func (a *appRuntime) AddSelfImprovement(ctx context.Context, title string, description string, kind string) (string, error) {
	if !a.cfg.Self.Enabled {
		return "", fmt.Errorf("self improvement is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("self improvement backlog writes require owner context")
	}
	if kind == "" {
		kind = "general"
	}
	if err := a.self.AppendBacklog(self.BacklogEntry{
		Title:       title,
		Description: description,
		Kind:        kind,
	}); err != nil {
		return "", err
	}
	a.logAudit(ctx, "add_self_improvement", "ok", title, map[string]any{"kind": kind})
	return "self-improvement item recorded", nil
}

func (a *appRuntime) ListSelfImprovements(_ context.Context, limit int) (string, error) {
	items, err := a.self.ListBacklog(limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) ListRecentSocial(_ context.Context, limit int) (string, error) {
	if a.social == nil {
		return "[]", nil
	}
	items, err := a.social.Recent(limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) PromoteSelfImprovement(ctx context.Context, title string, description string, modelName string) (string, error) {
	if !a.cfg.Self.Enabled {
		return "", fmt.Errorf("self improvement is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("promoting self improvement requires owner context")
	}
	prompt := "Work on this self-improvement task for Qorvexus.\nTitle: " + title + "\nDescription: " + description + "\nMake concrete progress and use tools if needed."
	out, err := a.EnqueueTask(ctx, "self-improvement: "+title, prompt, modelName, "")
	if err == nil {
		a.logAudit(ctx, "promote_self_improvement", "ok", title, map[string]any{"model": modelName})
	}
	return out, err
}

func (a *appRuntime) MineSelfImprovements(ctx context.Context, limit int) (string, error) {
	if !a.cfg.Self.Enabled {
		return "", fmt.Errorf("self improvement is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("mining self improvements requires owner context")
	}
	if limit <= 0 {
		limit = 20
	}
	if !a.cfg.Audit.Enabled {
		return "[]", nil
	}
	entries, err := a.audit.Recent(limit)
	if err != nil {
		return "", err
	}
	type candidate struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		Source      string `json:"source"`
	}
	var out []candidate
	for _, entry := range entries {
		if entry.Status != "ok" && entry.Status != "" {
			out = append(out, candidate{
				Title:       "Investigate failed action: " + entry.Action,
				Description: "A recent audited action did not complete cleanly and may need a workflow, prompt, or tool improvement.",
				Kind:        "reliability",
				Source:      entry.Action,
			})
			continue
		}
		switch entry.Action {
		case "retry_queue_task":
			out = append(out, candidate{
				Title:       "Reduce queue retries",
				Description: "Recent manual queue retries suggest the task execution flow should be more reliable or recoverable.",
				Kind:        "reliability",
				Source:      entry.Action,
			})
		case "write_runtime_config", "upsert_skill":
			out = append(out, candidate{
				Title:       "Review self-modification ergonomics",
				Description: "Frequent self-modification actions suggest a chance to make configuration and skill evolution safer or more structured.",
				Kind:        "self-optimization",
				Source:      entry.Action,
			})
		}
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) CaptureSelfImprovement(ctx context.Context, title string, description string, kind string, promote bool, modelName string) (string, error) {
	out, err := a.AddSelfImprovement(ctx, title, description, kind)
	if err != nil {
		return "", err
	}
	if !promote {
		return out, nil
	}
	promoted, err := a.PromoteSelfImprovement(ctx, title, description, modelName)
	if err != nil {
		return out + "\n" + err.Error(), nil
	}
	return out + "\n" + promoted, nil
}

func (a *appRuntime) HandleSocialEnvelope(ctx context.Context, env social.Envelope) (string, error) {
	if a.social == nil {
		return a.HandleEnvelope(ctx, env)
	}
	return a.social.Receive(ctx, env)
}

func (a *appRuntime) SendSocialMessage(ctx context.Context, channel string, threadID string, recipient string, text string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("outbound social messages require owner context")
	}
	out, err := a.connectors.Send(ctx, channel, social.OutboundMessage{
		Channel:   channel,
		ThreadID:  threadID,
		Recipient: recipient,
		Text:      text,
		Context: types.ConversationContext{
			Channel:      channel,
			Trust:        types.TrustOwner,
			IsOwner:      true,
			ReplyAsAgent: true,
		},
	})
	if err == nil {
		a.logAudit(ctx, "send_social_message", "ok", channel, map[string]any{"thread_id": threadID, "recipient": recipient})
	}
	return out, err
}

func (a *appRuntime) ListSocialConnectors(_ context.Context) (string, error) {
	raw, err := json.MarshalIndent(a.connectors.List(), "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) RetryQueueTask(ctx context.Context, id string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("retrying queue tasks requires owner context")
	}
	if err := a.queue.Retry(id); err != nil {
		return "", err
	}
	a.logAudit(ctx, "retry_queue_task", "ok", id, nil)
	return "queue task retried", nil
}

func (a *appRuntime) UpdateSelfImprovementStatus(ctx context.Context, id string, status string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("updating self improvement status requires owner context")
	}
	if err := a.self.UpdateStatus(id, status); err != nil {
		return "", err
	}
	a.logAudit(ctx, "update_self_improvement_status", "ok", id, map[string]any{"status": status})
	return "self improvement status updated", nil
}

func (a *appRuntime) ListAudit(_ context.Context, limit int) (string, error) {
	if !a.cfg.Audit.Enabled {
		return "[]", nil
	}
	items, err := a.audit.Recent(limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) HandleEnvelope(ctx context.Context, env social.Envelope) (string, error) {
	sessionID := env.Channel + "-" + env.ThreadID
	if sessionID == "-" || sessionID == "" {
		sessionID = env.Channel + "-" + env.SenderID
	}
	var parts []types.ContentPart
	for _, image := range env.Images {
		parts = append(parts, types.ContentPart{Type: "image_url", ImageURL: image})
	}
	_, out, err := a.runner.Run(ctx, agent.Request{
		SessionID: sessionID,
		Prompt:    env.Text,
		Parts:     parts,
		Context:   &env.Context,
	})
	return out, err
}

func sanitize(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func ownerAllowedFromContext(ctx context.Context) bool {
	convo, ok := tool.ConversationContextFrom(ctx)
	if !ok {
		return false
	}
	return convo.IsOwner || convo.Trust == types.TrustOwner
}

func (a *appRuntime) logAudit(ctx context.Context, action string, status string, target string, metadata map[string]any) {
	if !a.cfg.Audit.Enabled {
		return
	}
	var actor, channel, trust string
	if convo, ok := tool.ConversationContextFrom(ctx); ok {
		actor = convo.SenderID
		channel = convo.Channel
		trust = string(convo.Trust)
	}
	_ = a.audit.Append(audit.Entry{
		Action:   action,
		Actor:    actor,
		Channel:  channel,
		Trust:    trust,
		Status:   status,
		Target:   target,
		Metadata: metadata,
	})
}
