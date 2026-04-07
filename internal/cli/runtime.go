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
	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/memory"
	"qorvexus/internal/model"
	"qorvexus/internal/orchestrator"
	"qorvexus/internal/policy"
	"qorvexus/internal/scheduler"
	"qorvexus/internal/session"
	"qorvexus/internal/skill"
	"qorvexus/internal/taskqueue"
	"qorvexus/internal/tool"
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
		WebAddress:       a.cfg.Web.Address,
	}
}

func (a *appRuntime) RunPrompt(ctx context.Context, prompt string, model string, sessionID string) (string, error) {
	_, out, err := a.runner.Run(ctx, agent.Request{
		SessionID: sessionID,
		Model:     model,
		Prompt:    prompt,
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

func sanitize(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, " ", "-")
	return value
}
