package cli

import (
	"os"
	"path/filepath"
	"testing"

	"qorvexus/internal/config"
)

func TestSampleConfigIsMinimalButRunnable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "qorvexus.yaml")
	if err := os.WriteFile(path, []byte(sampleConfig()), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load sample config: %v", err)
	}

	if cfg.Agent.DefaultModel != "primary" {
		t.Fatalf("expected primary default model, got %q", cfg.Agent.DefaultModel)
	}
	if !cfg.Web.Enabled {
		t.Fatal("expected web UI enabled in sample config")
	}
	if !cfg.Queue.Enabled || !cfg.Queue.WorkerEnabled {
		t.Fatal("expected queue worker enabled in sample config")
	}
	if !cfg.Social.Enabled {
		t.Fatal("expected social enabled in sample config")
	}
	if len(cfg.Social.AllowedChannels) != 1 || cfg.Social.AllowedChannels[0] != "telegram" {
		t.Fatalf("expected minimal social channels, got %#v", cfg.Social.AllowedChannels)
	}
	if len(cfg.Identity.OwnerAliases) != 1 || cfg.Identity.OwnerAliases[0] != "owner" {
		t.Fatalf("expected generic owner alias, got %#v", cfg.Identity.OwnerAliases)
	}
	primary := cfg.Models["primary"]
	if primary.Provider != "openai-compatible" || primary.BaseURL != "https://api.openai.com/v1" || primary.Model != "gpt-4.1" {
		t.Fatalf("expected loader to fill primary model defaults, got %+v", primary)
	}
}
