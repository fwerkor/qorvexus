package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "qorvexus.yaml")
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
}
