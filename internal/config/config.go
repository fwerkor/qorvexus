package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DataDir   string                 `yaml:"data_dir"`
	Skills    SkillsConfig           `yaml:"skills"`
	Models    map[string]ModelConfig `yaml:"models"`
	Agent     AgentConfig            `yaml:"agent"`
	Identity  IdentityConfig         `yaml:"identity"`
	Tools     ToolsConfig            `yaml:"tools"`
	Scheduler SchedulerConfig        `yaml:"scheduler"`
	Memory    MemoryConfig           `yaml:"memory"`
	Queue     QueueConfig            `yaml:"queue"`
	Web       WebConfig              `yaml:"web"`
	Social    SocialConfig           `yaml:"social"`
	Self      SelfConfig             `yaml:"self"`
	Audit     AuditConfig            `yaml:"audit"`
}

type SkillsConfig struct {
	Dirs []string `yaml:"dirs"`
}

type ModelConfig struct {
	Provider    string            `yaml:"provider"`
	BaseURL     string            `yaml:"base_url"`
	APIKeyEnv   string            `yaml:"api_key_env"`
	Model       string            `yaml:"model"`
	MaxTokens   int               `yaml:"max_tokens"`
	Temperature float64           `yaml:"temperature"`
	Headers     map[string]string `yaml:"headers"`
	Vision      bool              `yaml:"vision"`
}

type AgentConfig struct {
	DefaultModel         string           `yaml:"default_model"`
	SummarizerModel      string           `yaml:"summarizer_model"`
	VisionFallbackModel  string           `yaml:"vision_fallback_model"`
	MaxTurns             int              `yaml:"max_turns"`
	ContextWindowChars   int              `yaml:"context_window_chars"`
	CompressionThreshold float64          `yaml:"compression_threshold"`
	SystemPrompt         string           `yaml:"system_prompt"`
	Discussion           DiscussionConfig `yaml:"discussion"`
}

type DiscussionConfig struct {
	DefaultPanel      []string `yaml:"default_panel"`
	SynthesisModel    string   `yaml:"synthesis_model"`
	MaxParallelModels int      `yaml:"max_parallel_models"`
}

type ToolsConfig struct {
	AllowCommandExecution bool     `yaml:"allow_command_execution"`
	CommandShell          string   `yaml:"command_shell"`
	PlaywrightCommand     string   `yaml:"playwright_command"`
	MaxCommandBytes       int      `yaml:"max_command_bytes"`
	HTTPUserAgent         string   `yaml:"http_user_agent"`
	BlockedCommands       []string `yaml:"blocked_commands"`
}

type IdentityConfig struct {
	OwnerIDs     []string `yaml:"owner_ids"`
	OwnerAliases []string `yaml:"owner_aliases"`
	TrustedIDs   []string `yaml:"trusted_ids"`
}

type SchedulerConfig struct {
	Enabled  bool   `yaml:"enabled"`
	TaskFile string `yaml:"task_file"`
}

type MemoryConfig struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

type QueueConfig struct {
	Enabled       bool   `yaml:"enabled"`
	File          string `yaml:"file"`
	WorkerEnabled bool   `yaml:"worker_enabled"`
	PollInterval  int    `yaml:"poll_interval_seconds"`
}

type WebConfig struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
}

type SocialConfig struct {
	Enabled         bool     `yaml:"enabled"`
	AllowedChannels []string `yaml:"allowed_channels"`
	InboxFile       string   `yaml:"inbox_file"`
	WebhookSecret   string   `yaml:"webhook_secret"`
}

type SelfConfig struct {
	Enabled          bool   `yaml:"enabled"`
	SkillsDir        string `yaml:"skills_dir"`
	BacklogFile      string `yaml:"backlog_file"`
	AllowConfigEdits bool   `yaml:"allow_config_edits"`
	AllowSkillWrites bool   `yaml:"allow_skill_writes"`
}

type AuditConfig struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.setDefaults(path); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) setDefaults(path string) error {
	base := filepath.Dir(path)
	if c.DataDir == "" {
		c.DataDir = filepath.Join(base, ".qorvexus")
	}
	if len(c.Skills.Dirs) == 0 {
		c.Skills.Dirs = []string{
			filepath.Join(base, "skills"),
			filepath.Join(base, ".agents", "skills"),
		}
	}
	for i, dir := range c.Skills.Dirs {
		c.Skills.Dirs[i] = expandPath(base, dir)
	}
	if c.Agent.MaxTurns <= 0 {
		c.Agent.MaxTurns = 12
	}
	if c.Agent.ContextWindowChars <= 0 {
		c.Agent.ContextWindowChars = 24000
	}
	if c.Agent.CompressionThreshold <= 0 || c.Agent.CompressionThreshold >= 1 {
		c.Agent.CompressionThreshold = 0.75
	}
	if c.Tools.CommandShell == "" {
		c.Tools.CommandShell = "bash"
	}
	if c.Tools.MaxCommandBytes <= 0 {
		c.Tools.MaxCommandBytes = 64 * 1024
	}
	if c.Tools.HTTPUserAgent == "" {
		c.Tools.HTTPUserAgent = "qorvexus/0.1"
	}
	if c.Scheduler.TaskFile == "" {
		c.Scheduler.TaskFile = filepath.Join(c.DataDir, "tasks.json")
	}
	if c.Memory.File == "" {
		c.Memory.File = filepath.Join(c.DataDir, "memory.jsonl")
	}
	if c.Queue.File == "" {
		c.Queue.File = filepath.Join(c.DataDir, "queue.json")
	}
	if c.Queue.PollInterval <= 0 {
		c.Queue.PollInterval = 5
	}
	if c.Web.Address == "" {
		c.Web.Address = "127.0.0.1:7788"
	}
	if c.Social.InboxFile == "" {
		c.Social.InboxFile = filepath.Join(c.DataDir, "social_inbox.jsonl")
	}
	if c.Self.SkillsDir == "" {
		c.Self.SkillsDir = filepath.Join(base, "skills")
	}
	if c.Self.BacklogFile == "" {
		c.Self.BacklogFile = filepath.Join(c.DataDir, "self_backlog.jsonl")
	}
	c.Self.SkillsDir = expandPath(base, c.Self.SkillsDir)
	if c.Audit.File == "" {
		c.Audit.File = filepath.Join(c.DataDir, "audit.jsonl")
	}
	if c.Agent.DefaultModel == "" {
		return errors.New("agent.default_model is required")
	}
	if _, ok := c.Models[c.Agent.DefaultModel]; !ok {
		return fmt.Errorf("default model %q not found", c.Agent.DefaultModel)
	}
	if c.Agent.SummarizerModel != "" {
		if _, ok := c.Models[c.Agent.SummarizerModel]; !ok {
			return fmt.Errorf("summarizer model %q not found", c.Agent.SummarizerModel)
		}
	}
	if c.Agent.VisionFallbackModel != "" {
		if _, ok := c.Models[c.Agent.VisionFallbackModel]; !ok {
			return fmt.Errorf("vision fallback model %q not found", c.Agent.VisionFallbackModel)
		}
	}
	if c.Agent.Discussion.SynthesisModel != "" {
		if _, ok := c.Models[c.Agent.Discussion.SynthesisModel]; !ok {
			return fmt.Errorf("synthesis model %q not found", c.Agent.Discussion.SynthesisModel)
		}
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	return nil
}

func expandPath(base string, value string) string {
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, value[2:])
		}
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(base, value)
}
