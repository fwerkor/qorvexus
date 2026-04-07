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
	case "skills":
		return skillsCommand(args[1:])
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
	app, err := newRuntime(cfg)
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
	app, err := newRuntime(cfg)
	if err != nil {
		return err
	}
	if cfg.Scheduler.Enabled {
		if err := app.scheduler.Start(); err != nil {
			return err
		}
	}
	fmt.Println("qorvexus daemon is running")
	<-ctx.Done()
	return nil
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

func initCommand(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("path", "examples/qorvexus.yaml", "write config to path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil {
		return fmt.Errorf("%s already exists", *path)
	}
	content := sampleConfig()
	return os.WriteFile(*path, []byte(content), 0o644)
}

func usage() error {
	return errors.New("usage: qorvexus <run|daemon|skills|init> [flags]")
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
scheduler:
  enabled: true
  task_file: ./.qorvexus/tasks.json
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
