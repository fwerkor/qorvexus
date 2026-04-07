package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"qorvexus/internal/agent"
	"qorvexus/internal/config"
	"qorvexus/internal/skill"
	"qorvexus/internal/types"
)

const defaultConfigPath = "qorvexus.yaml"

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return startCommand(ctx, nil)
	}
	switch args[0] {
	case "start":
		return startCommand(ctx, args[1:])
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
	configPath := fs.String("config", defaultConfigPath, "config path")
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
	if err := ensureConfigExists(*configPath); err != nil {
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
	configPath := fs.String("config", defaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runService(ctx, *configPath, false)
}

func startCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	configPath := fs.String("config", defaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runService(ctx, *configPath, false)
}

func runService(ctx context.Context, configPath string, forceWeb bool) error {
	if err := ensureConfigExists(configPath); err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if forceWeb {
		cfg.Web.Enabled = true
	}
	app, err := newRuntime(cfg, configPath)
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
		go func() {
			_ = app.RunSocialBackground(ctx)
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
	fmt.Println("qorvexus service is running")
	<-ctx.Done()
	return nil
}

func webCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	configPath := fs.String("config", defaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := ensureConfigExists(*configPath); err != nil {
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
		go func() {
			_ = app.RunSocialBackground(ctx)
		}()
	}
	fmt.Printf("qorvexus web panel listening on http://%s\n", cfg.Web.Address)
	return app.webServer.ListenAndServe()
}

func skillsCommand(args []string) error {
	fs := flag.NewFlagSet("skills", flag.ContinueOnError)
	configPath := fs.String("config", defaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := ensureConfigExists(*configPath); err != nil {
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
	configPath := fs.String("config", defaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := ensureConfigExists(*configPath); err != nil {
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
	path := fs.String("path", defaultConfigPath, "write config to path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil {
		return fmt.Errorf("%s already exists", *path)
	}
	return os.WriteFile(*path, []byte(sampleConfig()), 0o644)
}

func usage() error {
	return errors.New("usage: qorvexus [start|run|daemon|web|skills|queue|init] [flags]")
}

func ensureConfigExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(sampleConfig()), 0o644)
}

func sampleConfig() string {
	return `models:
  primary:
    provider: openai-compatible
    base_url: https://api.openai.com/v1
    api_key: ""
    model: gpt-4.1
scheduler:
  enabled: true
memory:
  enabled: true
queue:
  enabled: true
  worker_enabled: true
web:
  enabled: true
  address: 127.0.0.1:7788
social:
  enabled: true
  allowed_channels:
    - telegram
  telegram:
    bot_token: ""
tools:
  allow_command_execution: true
self:
  enabled: true
  allow_config_edits: true
  allow_skill_writes: true
audit:
  enabled: true
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
