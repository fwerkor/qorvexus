package cli

import (
	"testing"

	"qorvexus/internal/config"
)

func TestSubAgentMaxTurnsInheritsUnlimitedDefault(t *testing.T) {
	app := &appRuntime{
		cfg: &config.Config{
			Agent: config.AgentConfig{},
		},
	}

	if got := app.subAgentMaxTurns(); got != 0 {
		t.Fatalf("expected unlimited sub-agent max turns by default, got %d", got)
	}
}

func TestSubAgentMaxTurnsCapsPositiveConfig(t *testing.T) {
	app := &appRuntime{
		cfg: &config.Config{
			Agent: config.AgentConfig{
				MaxTurns: 12,
			},
		},
	}

	if got := app.subAgentMaxTurns(); got != 4 {
		t.Fatalf("expected positive sub-agent max turns to remain capped, got %d", got)
	}
}
