package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
models:
  primary:
    provider: openai-compatible
    base_url: https://api.openai.com/v1
    model: gpt-4.1
agent:
  default_model: primary
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir == "" {
		t.Fatal("expected data dir default")
	}
	if len(cfg.Skills.Dirs) == 0 {
		t.Fatal("expected default skills dirs")
	}
	if cfg.Agent.MaxTurns != 12 {
		t.Fatalf("expected default max turns, got %d", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.SystemPrompt == "" {
		t.Fatal("expected default system prompt")
	}
	if cfg.Agent.SummarizerModel != "" {
		t.Fatalf("expected summarizer model to remain optional by default, got %q", cfg.Agent.SummarizerModel)
	}
	if got := cfg.Models["primary"].Provider; got != "openai-compatible" {
		t.Fatalf("expected default provider, got %q", got)
	}
	if got := cfg.Models["primary"].BaseURL; got != "https://api.openai.com/v1" {
		t.Fatalf("expected default base URL, got %q", got)
	}
	if got := cfg.Models["primary"].Model; got != "gpt-4.1" {
		t.Fatalf("expected default model, got %q", got)
	}
	if len(cfg.Identity.OwnerAliases) != 1 || cfg.Identity.OwnerAliases[0] != "owner" {
		t.Fatalf("expected default owner alias, got %#v", cfg.Identity.OwnerAliases)
	}
	if cfg.Tools.PlaywrightCommand == "" {
		t.Fatal("expected default playwright command")
	}
	if got := cfg.Tools.PlaywrightBrowser; got != "chromium" {
		t.Fatalf("expected default playwright browser, got %q", got)
	}
	if cfg.Tools.PlaywrightProfileDir == "" || cfg.Tools.PlaywrightStateDir == "" || cfg.Tools.PlaywrightArtifactsDir == "" {
		t.Fatal("expected default playwright persistence directories")
	}
	if cfg.Tools.PlaywrightRuntimeDir == "" {
		t.Fatal("expected default playwright runtime dir")
	}
	if cfg.Tools.PlaywrightAutoInstall == nil || !*cfg.Tools.PlaywrightAutoInstall {
		t.Fatalf("expected default playwright auto install true, got %#v", cfg.Tools.PlaywrightAutoInstall)
	}
	if len(cfg.Tools.PlaywrightInstallBrowser) != 1 || cfg.Tools.PlaywrightInstallBrowser[0] != "chromium" {
		t.Fatalf("expected default playwright install browser chromium, got %#v", cfg.Tools.PlaywrightInstallBrowser)
	}
	if cfg.Tools.PlaywrightTimeoutSeconds != 120 {
		t.Fatalf("expected default playwright timeout, got %d", cfg.Tools.PlaywrightTimeoutSeconds)
	}
	if cfg.Tools.PlaywrightHeadless == nil || !*cfg.Tools.PlaywrightHeadless {
		t.Fatalf("expected default playwright headless true, got %#v", cfg.Tools.PlaywrightHeadless)
	}
}

func TestLoadAppliesTelegramDefaultsWhenSocialEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
models:
  primary:
    api_key: ""
social:
  enabled: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Social.Telegram.Mode; got != "polling" {
		t.Fatalf("expected default telegram mode polling, got %q", got)
	}
	if len(cfg.Social.AllowedChannels) != 1 || cfg.Social.AllowedChannels[0] != "telegram" {
		t.Fatalf("expected default telegram channel, got %#v", cfg.Social.AllowedChannels)
	}
}

func TestLoadAppliesQQBotDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
models:
  primary:
    api_key: ""
social:
  enabled: true
  allowed_channels:
    - qqbot
  qqbot:
    app_id: "123"
    client_secret: "secret"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Social.QQBot.APIBaseURL; got != "https://api.sgroup.qq.com" {
		t.Fatalf("expected default QQBot API base URL, got %q", got)
	}
	if got := cfg.Social.QQBot.TokenBaseURL; got != "https://bots.qq.com" {
		t.Fatalf("expected default QQBot token base URL, got %q", got)
	}
}

func TestParseRawRejectsInvalidYAML(t *testing.T) {
	_, err := ParseRaw("/tmp/config.yaml", []byte("models: ["))
	if err == nil {
		t.Fatal("expected parse error for invalid yaml")
	}
}

func TestParseRawRejectsUnknownDefaultModel(t *testing.T) {
	raw := []byte(`
models:
  primary:
    provider: openai-compatible
agent:
  default_model: missing
`)
	_, err := ParseRaw("/tmp/config.yaml", raw)
	if err == nil {
		t.Fatal("expected validation error for missing default model")
	}
}
