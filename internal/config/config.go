package config

import (
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
	APIKey      string            `yaml:"api_key"`
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
	AllowCommandExecution    bool     `yaml:"allow_command_execution"`
	CommandShell             string   `yaml:"command_shell"`
	PlaywrightCommand        string   `yaml:"playwright_command"`
	PlaywrightBrowser        string   `yaml:"playwright_browser"`
	PlaywrightProfileDir     string   `yaml:"playwright_profile_dir"`
	PlaywrightStateDir       string   `yaml:"playwright_state_dir"`
	PlaywrightArtifactsDir   string   `yaml:"playwright_artifacts_dir"`
	PlaywrightRuntimeDir     string   `yaml:"playwright_runtime_dir"`
	PlaywrightInstallBrowser []string `yaml:"playwright_install_browsers"`
	PlaywrightAutoInstall    *bool    `yaml:"playwright_auto_install"`
	PlaywrightTimeoutSeconds int      `yaml:"playwright_timeout_seconds"`
	PlaywrightHeadless       *bool    `yaml:"playwright_headless"`
	MaxCommandBytes          int      `yaml:"max_command_bytes"`
	HTTPUserAgent            string   `yaml:"http_user_agent"`
	BlockedCommands          []string `yaml:"blocked_commands"`
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
	Enabled             bool   `yaml:"enabled"`
	File                string `yaml:"file"`
	EmbeddingModel      string `yaml:"embedding_model"`
	SummaryModel        string `yaml:"summary_model"`
	SemanticSearch      *bool  `yaml:"semantic_search"`
	CompactionThreshold int    `yaml:"compaction_threshold"`
	CompactionRetain    int    `yaml:"compaction_retain"`
	MaxSummarySources   int    `yaml:"max_summary_sources"`
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
	Enabled                       bool           `yaml:"enabled"`
	AllowedChannels               []string       `yaml:"allowed_channels"`
	InboxFile                     string         `yaml:"inbox_file"`
	CommitmentFile                string         `yaml:"commitment_file"`
	DraftFile                     string         `yaml:"draft_file"`
	FollowUpFile                  string         `yaml:"followup_file"`
	GraphFile                     string         `yaml:"graph_file"`
	CommitmentScanIntervalSeconds int            `yaml:"commitment_scan_interval_seconds"`
	ReminderCooldownHours         int            `yaml:"reminder_cooldown_hours"`
	AutoSendTrustedReplies        *bool          `yaml:"auto_send_trusted_replies"`
	AutoSendExternalReplies       *bool          `yaml:"auto_send_external_replies"`
	Discord                       DiscordConfig  `yaml:"discord"`
	QQBot                         QQBotConfig    `yaml:"qqbot"`
	Slack                         SlackConfig    `yaml:"slack"`
	Telegram                      TelegramConfig `yaml:"telegram"`
}

type DiscordConfig struct {
	BotToken         string `yaml:"bot_token"`
	APIBaseURL       string `yaml:"api_base_url"`
	DefaultChannelID string `yaml:"default_channel_id"`
}

type SlackConfig struct {
	BotToken         string `yaml:"bot_token"`
	APIBaseURL       string `yaml:"api_base_url"`
	DefaultChannelID string `yaml:"default_channel_id"`
}

type QQBotConfig struct {
	AppID         string `yaml:"app_id"`
	ClientSecret  string `yaml:"client_secret"`
	APIBaseURL    string `yaml:"api_base_url"`
	TokenBaseURL  string `yaml:"token_base_url"`
	DefaultTarget string `yaml:"default_target"`
}

type TelegramConfig struct {
	BotToken            string `yaml:"bot_token"`
	Mode                string `yaml:"mode"`
	PollTimeoutSeconds  int    `yaml:"poll_timeout_seconds"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	WebhookPath         string `yaml:"webhook_path"`
	WebhookSecret       string `yaml:"webhook_secret"`
	PublicBaseURL       string `yaml:"public_base_url"`
}

type SelfConfig struct {
	Enabled           bool   `yaml:"enabled"`
	SkillsDir         string `yaml:"skills_dir"`
	BacklogFile       string `yaml:"backlog_file"`
	AllowConfigEdits  bool   `yaml:"allow_config_edits"`
	AllowSkillWrites  bool   `yaml:"allow_skill_writes"`
	AllowRuntimeApply *bool  `yaml:"allow_runtime_apply"`
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

	return ParseRaw(path, raw)
}

func ParseRaw(path string, raw []byte) (*Config, error) {
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
	if c.Models == nil {
		c.Models = map[string]ModelConfig{}
	}
	if len(c.Models) == 0 {
		c.Models["primary"] = ModelConfig{
			Provider:    "openai-compatible",
			BaseURL:     "https://api.openai.com/v1",
			APIKey:      "",
			Model:       "gpt-4.1",
			MaxTokens:   2000,
			Temperature: 0.2,
		}
	}
	if _, ok := c.Models["primary"]; !ok {
		c.Models["primary"] = ModelConfig{
			Provider:    "openai-compatible",
			BaseURL:     "https://api.openai.com/v1",
			APIKey:      "",
			Model:       "gpt-4.1",
			MaxTokens:   2000,
			Temperature: 0.2,
		}
	}
	for name, modelCfg := range c.Models {
		if strings.TrimSpace(modelCfg.Provider) == "" {
			modelCfg.Provider = "openai-compatible"
		}
		if strings.TrimSpace(modelCfg.BaseURL) == "" {
			modelCfg.BaseURL = "https://api.openai.com/v1"
		}
		if strings.TrimSpace(modelCfg.Model) == "" {
			if name == "primary" {
				modelCfg.Model = "gpt-4.1"
			} else {
				modelCfg.Model = "gpt-4.1-mini"
			}
		}
		if modelCfg.MaxTokens <= 0 {
			if name == "primary" {
				modelCfg.MaxTokens = 2000
			} else {
				modelCfg.MaxTokens = 800
			}
		}
		if modelCfg.Temperature == 0 {
			if name == "primary" {
				modelCfg.Temperature = 0.2
			} else {
				modelCfg.Temperature = 0.1
			}
		}
		c.Models[name] = modelCfg
	}
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
	if c.Agent.DefaultModel == "" {
		c.Agent.DefaultModel = "primary"
	}
	if c.Agent.VisionFallbackModel == "" {
		c.Agent.VisionFallbackModel = c.Agent.DefaultModel
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
	if strings.TrimSpace(c.Agent.SystemPrompt) == "" {
		c.Agent.SystemPrompt = defaultSystemPrompt()
	}
	if len(c.Agent.Discussion.DefaultPanel) == 0 {
		c.Agent.Discussion.DefaultPanel = []string{c.Agent.DefaultModel}
		if c.Agent.SummarizerModel != "" && c.Agent.SummarizerModel != c.Agent.DefaultModel {
			c.Agent.Discussion.DefaultPanel = append(c.Agent.Discussion.DefaultPanel, c.Agent.SummarizerModel)
		}
	}
	if c.Agent.Discussion.SynthesisModel == "" {
		c.Agent.Discussion.SynthesisModel = c.Agent.DefaultModel
	}
	if c.Agent.Discussion.MaxParallelModels <= 0 {
		c.Agent.Discussion.MaxParallelModels = 4
	}
	if c.Tools.CommandShell == "" {
		c.Tools.CommandShell = "bash"
	}
	if strings.TrimSpace(c.Tools.PlaywrightCommand) == "" {
		c.Tools.PlaywrightCommand = fmt.Sprintf("node %q", filepath.Join(base, "scripts", "playwright_runner.js"))
	}
	if strings.TrimSpace(c.Tools.PlaywrightBrowser) == "" {
		c.Tools.PlaywrightBrowser = "chromium"
	}
	if c.Tools.PlaywrightProfileDir == "" {
		c.Tools.PlaywrightProfileDir = filepath.Join(c.DataDir, "browser", "profiles")
	}
	if c.Tools.PlaywrightStateDir == "" {
		c.Tools.PlaywrightStateDir = filepath.Join(c.DataDir, "browser", "state")
	}
	if c.Tools.PlaywrightArtifactsDir == "" {
		c.Tools.PlaywrightArtifactsDir = filepath.Join(c.DataDir, "browser", "artifacts")
	}
	if c.Tools.PlaywrightRuntimeDir == "" {
		c.Tools.PlaywrightRuntimeDir = filepath.Join(c.DataDir, "browser", "runtime")
	}
	if len(c.Tools.PlaywrightInstallBrowser) == 0 {
		c.Tools.PlaywrightInstallBrowser = []string{c.Tools.PlaywrightBrowser}
	}
	if c.Tools.PlaywrightAutoInstall == nil {
		c.Tools.PlaywrightAutoInstall = boolPtr(true)
	}
	if c.Tools.PlaywrightTimeoutSeconds <= 0 {
		c.Tools.PlaywrightTimeoutSeconds = 120
	}
	if c.Tools.PlaywrightHeadless == nil {
		c.Tools.PlaywrightHeadless = boolPtr(true)
	}
	c.Tools.PlaywrightProfileDir = expandPath(base, c.Tools.PlaywrightProfileDir)
	c.Tools.PlaywrightStateDir = expandPath(base, c.Tools.PlaywrightStateDir)
	c.Tools.PlaywrightArtifactsDir = expandPath(base, c.Tools.PlaywrightArtifactsDir)
	c.Tools.PlaywrightRuntimeDir = expandPath(base, c.Tools.PlaywrightRuntimeDir)
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
	if c.Memory.SemanticSearch == nil {
		c.Memory.SemanticSearch = boolPtr(true)
	}
	if c.Memory.CompactionThreshold <= 0 {
		c.Memory.CompactionThreshold = 6
	}
	if c.Memory.CompactionRetain <= 0 {
		c.Memory.CompactionRetain = 3
	}
	if c.Memory.MaxSummarySources <= 0 {
		c.Memory.MaxSummarySources = 6
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
	if len(c.Identity.OwnerAliases) == 0 {
		c.Identity.OwnerAliases = []string{"owner"}
	}
	if c.Social.InboxFile == "" {
		c.Social.InboxFile = filepath.Join(c.DataDir, "social_inbox.jsonl")
	}
	if c.Social.CommitmentFile == "" {
		c.Social.CommitmentFile = filepath.Join(c.DataDir, "social_commitments.jsonl")
	}
	if c.Social.DraftFile == "" {
		c.Social.DraftFile = filepath.Join(c.DataDir, "social_drafts.json")
	}
	if c.Social.FollowUpFile == "" {
		c.Social.FollowUpFile = filepath.Join(c.DataDir, "social_followups.json")
	}
	if c.Social.GraphFile == "" {
		c.Social.GraphFile = filepath.Join(c.DataDir, "social_graph.json")
	}
	if c.Social.CommitmentScanIntervalSeconds <= 0 {
		c.Social.CommitmentScanIntervalSeconds = 3600
	}
	if c.Social.ReminderCooldownHours <= 0 {
		c.Social.ReminderCooldownHours = 48
	}
	if c.Social.AutoSendTrustedReplies == nil {
		c.Social.AutoSendTrustedReplies = boolPtr(true)
	}
	if c.Social.AutoSendExternalReplies == nil {
		c.Social.AutoSendExternalReplies = boolPtr(true)
	}
	if strings.TrimSpace(c.Social.Discord.APIBaseURL) == "" {
		c.Social.Discord.APIBaseURL = "https://discord.com/api/v10"
	}
	if strings.TrimSpace(c.Social.QQBot.APIBaseURL) == "" {
		c.Social.QQBot.APIBaseURL = "https://api.sgroup.qq.com"
	}
	if strings.TrimSpace(c.Social.QQBot.TokenBaseURL) == "" {
		c.Social.QQBot.TokenBaseURL = "https://bots.qq.com"
	}
	if strings.TrimSpace(c.Social.Slack.APIBaseURL) == "" {
		c.Social.Slack.APIBaseURL = "https://slack.com/api"
	}
	if strings.TrimSpace(c.Social.Telegram.Mode) == "" {
		c.Social.Telegram.Mode = "polling"
	}
	if len(c.Social.AllowedChannels) == 0 && c.Social.Enabled {
		c.Social.AllowedChannels = []string{"telegram"}
	}
	if c.Social.Telegram.PollTimeoutSeconds <= 0 {
		c.Social.Telegram.PollTimeoutSeconds = 30
	}
	if c.Social.Telegram.PollIntervalSeconds <= 0 {
		c.Social.Telegram.PollIntervalSeconds = 1
	}
	if c.Social.Telegram.WebhookPath == "" {
		c.Social.Telegram.WebhookPath = "/webhooks/telegram"
	}
	if c.Self.SkillsDir == "" {
		c.Self.SkillsDir = filepath.Join(base, "skills")
	}
	if c.Self.BacklogFile == "" {
		c.Self.BacklogFile = filepath.Join(c.DataDir, "self_backlog.jsonl")
	}
	if c.Self.AllowRuntimeApply == nil {
		c.Self.AllowRuntimeApply = boolPtr(true)
	}
	c.Self.SkillsDir = expandPath(base, c.Self.SkillsDir)
	if c.Audit.File == "" {
		c.Audit.File = filepath.Join(c.DataDir, "audit.jsonl")
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
	if c.Memory.EmbeddingModel != "" {
		if _, ok := c.Models[c.Memory.EmbeddingModel]; !ok {
			return fmt.Errorf("memory embedding model %q not found", c.Memory.EmbeddingModel)
		}
	}
	if c.Memory.SummaryModel != "" {
		if _, ok := c.Models[c.Memory.SummaryModel]; !ok {
			return fmt.Errorf("memory summary model %q not found", c.Memory.SummaryModel)
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

func defaultSystemPrompt() string {
	return strings.TrimSpace(`
You are Qorvexus, a capable autonomous agent with long-horizon memory and tool access.
Act directly when the next step is clear, stay careful with authority boundaries, and prefer verifiable action over vague advice.
Use tools, background tasks, scheduling, memory, and social channels when they help complete real work.
For complex work, create a durable execution plan, break it into concrete steps with dependencies, and keep the plan updated as you execute it.
Use subagents or queued plan steps when they help parallelize or isolate focused work, and preserve results back into the plan.
When interacting with the local device, prefer structured system, filesystem, and process tools before falling back to raw shell commands.
Use run_command for short, synchronous shell work only. For long-running or stateful commands such as apt update, package installs, servers, watchers, or builds that may exceed a short timeout, use manage_process with action=start so the job can keep running in the background.
When doing software engineering work, prefer repository indexing, structured repo search, apply_diff, change summaries, and test failure localization before improvising with raw shell output.
When browsing the web, prefer the structured browser workflow tool for common tasks, and use raw Playwright scripts only for flows that need custom logic.
When you need simple web fetches or APIs without login or JavaScript interaction, prefer http_request instead of browser automation.
When browsing the web through Playwright, prefer persistent browser profiles so logins, cookies, and session state can survive across runs.
When continuity matters, inspect saved sessions with list_sessions and get_session so you can recover relevant context from other threads, channels, or prior work before acting.
When preserving durable facts, preferences, project state, or relationship context would help future work, store them with remember and retrieve them with recall.
When the owner wants proactive outreach or cross-channel follow-up, inspect available connectors and use send_social_message or hold_social_message deliberately.
If an outbound social message is time-sensitive and ready, send it directly; if wording, authority, or timing is uncertain, prefer hold_social_message first.
When an authenticated owner wants to authorize a new chat identity, device, or channel route as owner, use grant_owner_identity.
When talking to external parties, act as an autonomous assistant with delegated authority: you may reply, defer internally, or stay silent when no response is needed.
When a social channel conversation does not need an outward reply, return [[NO_REPLY]] instead of forcing a response.
When improving yourself, make concrete, reversible progress and preserve auditability.
Before replacing runtime config, read the current config first; after config or skill changes, use restart_runtime so the running service reloads them.
When changing runtime config or SKILL.md files that should affect the live service, use restart_runtime after the write succeeds so the supervised runtime reloads them.
When changing Qorvexus source code and you need the running service to adopt it, use apply_self_update so a fresh binary is built and the supervisor can hand off to it.
Do not claim a self-update is live until restart_runtime or apply_self_update has succeeded; if supervised runtime apply is unavailable, say so plainly.
`)
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

func boolPtr(value bool) *bool {
	return &value
}
