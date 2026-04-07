package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"qorvexus/internal/agent"
	"qorvexus/internal/config"
	"qorvexus/internal/skill"
	"qorvexus/internal/types"
)

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "run":
		return runCommand(ctx, args[1:])
	case "daemon":
		return daemonCommand(ctx, args[1:])
	case "web":
		return webCommand(ctx, args[1:])
	case "skills":
		return skillsCommand(args[1:])
	case "queue":
		return queueCommand(args[1:])
	case "init":
		return initCommand(args[1:])
	default:
		return usage()
	}
}

func runCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "examples/qorvexus.yaml", "config path")
	modelName := fs.String("model", "", "override model")
	sessionID := fs.String("session", "", "session id")
	var images stringSliceFlag
	fs.Var(&images, "image", "image URL for multimodal input; may be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("prompt is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	app, err := newRuntime(cfg, *configPath)
	if err != nil {
		return err
	}
	var parts []types.ContentPart
	for _, image := range images {
		parts = append(parts, types.ContentPart{Type: "image_url", ImageURL: image})
	}
	_, out, err := app.runner.Run(ctx, agent.Request{
		SessionID: *sessionID,
		Model:     *modelName,
		Prompt:    strings.Join(fs.Args(), " "),
		Parts:     parts,
	})
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func daemonCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	configPath := fs.String("config", "examples/qorvexus.yaml", "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	app, err := newRuntime(cfg, *configPath)
	if err != nil {
		return err
	}
	if cfg.Scheduler.Enabled {
		if err := app.scheduler.Start(); err != nil {
			return err
		}
	}
	if cfg.Social.Enabled {
		go func() {
			_ = app.RunCommitmentWatchdog(ctx)
		}()
	}
	if cfg.Queue.Enabled && cfg.Queue.WorkerEnabled {
		go func() {
			_ = app.worker.Run(ctx)
		}()
	}
	if cfg.Web.Enabled && app.webServer != nil {
		go func() {
			_ = app.webServer.ListenAndServe()
		}()
		fmt.Printf("web panel listening on http://%s\n", cfg.Web.Address)
	}
	fmt.Println("qorvexus daemon is running")
	<-ctx.Done()
	return nil
}

func webCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	configPath := fs.String("config", "examples/qorvexus.yaml", "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	cfg.Web.Enabled = true
	app, err := newRuntime(cfg, *configPath)
	if err != nil {
		return err
	}
	if app.webServer == nil {
		return errors.New("web server is not configured")
	}
	if cfg.Queue.Enabled && cfg.Queue.WorkerEnabled {
		go func() {
			_ = app.worker.Run(ctx)
		}()
	}
	if cfg.Social.Enabled {
		go func() {
			_ = app.RunCommitmentWatchdog(ctx)
		}()
	}
	fmt.Printf("qorvexus web panel listening on http://%s\n", cfg.Web.Address)
	return app.webServer.ListenAndServe()
}

func skillsCommand(args []string) error {
	fs := flag.NewFlagSet("skills", flag.ContinueOnError)
	configPath := fs.String("config", "examples/qorvexus.yaml", "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	skills, err := skill.NewLoader().LoadDirs(cfg.Skills.Dirs)
	if err != nil {
		return err
	}
	for _, sk := range skills {
		fmt.Printf("%s\t%s\t%s\n", sk.Name, sk.Description, sk.Location)
	}
	return nil
}

func queueCommand(args []string) error {
	fs := flag.NewFlagSet("queue", flag.ContinueOnError)
	configPath := fs.String("config", "examples/qorvexus.yaml", "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	app, err := newRuntime(cfg, *configPath)
	if err != nil {
		return err
	}
	for _, task := range app.queue.List() {
		fmt.Printf("%s\t%s\t%s\t%s\n", task.ID, task.Status, task.Name, task.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func initCommand(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("path", "examples/qorvexus.yaml", "write config to path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil {
		return fmt.Errorf("%s already exists", *path)
	}
	return os.WriteFile(*path, []byte(sampleConfig()), 0o644)
}

func usage() error {
	return errors.New("usage: qorvexus <run|daemon|web|skills|queue|init> [flags]")
}

func sampleConfig() string {
	return `data_dir: ./.qorvexus
skills:
  dirs:
    - ./skills
models:
  primary:
    provider: openai-compatible
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
    model: gpt-4.1
    max_tokens: 2000
    temperature: 0.2
  summarizer:
    provider: openai-compatible
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
    model: gpt-4.1-mini
    max_tokens: 800
    temperature: 0.1
agent:
  default_model: primary
  summarizer_model: summarizer
  vision_fallback_model: primary
  max_turns: 12
  context_window_chars: 24000
  compression_threshold: 0.75
  system_prompt: |
    You are Qorvexus, a capable autonomous agent. Use tools when they help. Prefer direct, verifiable action.
  discussion:
    default_panel: [primary, summarizer]
    synthesis_model: primary
    max_parallel_models: 4
tools:
  allow_command_execution: true
  command_shell: bash
  playwright_command: node ./scripts/playwright.js
  max_command_bytes: 65536
  http_user_agent: qorvexus/0.1
  blocked_commands:
    - rm -rf /
    - git reset --hard
    - shutdown
scheduler:
  enabled: true
  task_file: ./.qorvexus/tasks.json
memory:
  enabled: true
  file: ./.qorvexus/memory.jsonl
queue:
  enabled: true
  file: ./.qorvexus/queue.json
  worker_enabled: true
  poll_interval_seconds: 5
web:
  enabled: true
  address: 127.0.0.1:7788
identity:
  owner_ids:
    - owner
  owner_aliases:
    - fwerkor
  trusted_ids: []
social:
  enabled: true
  allowed_channels:
    - telegram
    - discord
    - slack
    - x
    - email
  inbox_file: ./.qorvexus/social_inbox.jsonl
  commitment_file: ./.qorvexus/social_commitments.jsonl
  commitment_scan_interval_seconds: 3600
  webhook_secret: change-me
self:
  enabled: true
  skills_dir: ./skills
  backlog_file: ./.qorvexus/self_backlog.jsonl
  allow_config_edits: true
  allow_skill_writes: true
audit:
  enabled: true
  file: ./.qorvexus/audit.jsonl
`
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}
