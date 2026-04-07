package policy

import (
	"testing"

	"qorvexus/internal/config"
)

func TestPolicyBlocksDangerousCommand(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	result := engine.EvaluateCommand("rm -rf /")
	if result.Verdict != VerdictDeny {
		t.Fatalf("expected deny, got %s", result.Verdict)
	}
}

func TestPolicyAllowsSafeCommand(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	result := engine.EvaluateCommand("ls -la")
	if result.Verdict != VerdictAllow {
		t.Fatalf("expected allow, got %s", result.Verdict)
	}
}
