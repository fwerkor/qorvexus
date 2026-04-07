package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"qorvexus/internal/agent"
	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/model"
	"qorvexus/internal/orchestrator"
	"qorvexus/internal/scheduler"
	"qorvexus/internal/session"
	"qorvexus/internal/skill"
	"qorvexus/internal/tool"
)

type appRuntime struct {
	cfg        *config.Config
	runner     *agent.Runner
	discussion *orchestrator.Discussion
	scheduler  *scheduler.Manager
}

func newRuntime(cfg *config.Config) (*appRuntime, error) {
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
	app := &appRuntime{
		cfg:        cfg,
		discussion: discussion,
	}
	toolRegistry := tool.NewRegistry()
	toolRegistry.Register(&tool.ThinkTool{})
	toolRegistry.Register(tool.NewCommandTool(cfg.Tools))
	toolRegistry.Register(tool.NewHTTPTool(cfg.Tools))
	toolRegistry.Register(tool.NewPlaywrightTool(cfg.Tools))
	toolRegistry.Register(tool.NewSubAgentTool(app))
	toolRegistry.Register(tool.NewDiscussTool(app))
	toolRegistry.Register(tool.NewScheduleTool(app))

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

func (a *appRuntime) RunScheduled(ctx context.Context, task scheduler.Task) error {
	_, _, err := a.runner.Run(ctx, agent.Request{
		SessionID: "cron-" + task.ID,
		Model:     task.Model,
		Prompt:    task.Prompt,
	})
	return err
}

func sanitize(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, " ", "-")
	return value
}
