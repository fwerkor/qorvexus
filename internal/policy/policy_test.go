package policy

import (
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

func TestPolicyBlocksDangerousCommand(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	result := engine.EvaluateCommand("rm -rf /")
	if result.Verdict != VerdictDeny {
		t.Fatalf("expected deny, got %s", result.Verdict)
	}
}

func TestPolicyAllowsDeletingOrdinaryDirectories(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	commands := []string{
		"rm -rf /tmp/qorvexus-build",
		"rm -rf ./dist",
		"rm -rf project/",
		"rm -rf /root/qorvexus/.qorvexus/browser/runtime",
	}
	for _, command := range commands {
		result := engine.EvaluateCommand(command)
		if result.Verdict != VerdictAllow {
			t.Fatalf("expected allow for %q, got %s: %s", command, result.Verdict, result.Reason)
		}
	}
}

func TestPolicyBlocksRootDeletionWithWrappersAndSeparators(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	commands := []string{
		"sudo rm -rf /",
		"cd /tmp && rm -fr /",
		"env FOO=bar rm -r -f /",
		"rm -rf ///",
		"rm -rf / /tmp/keep",
	}
	for _, command := range commands {
		result := engine.EvaluateCommand(command)
		if result.Verdict != VerdictDeny {
			t.Fatalf("expected deny for %q, got %s", command, result.Verdict)
		}
	}
}

func TestPolicyAllowsSafeCommand(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	result := engine.EvaluateCommand("ls -la")
	if result.Verdict != VerdictAllow {
		t.Fatalf("expected allow, got %s", result.Verdict)
	}
}

func TestPolicyBlocksHighRiskCommandForNonOwner(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	result := engine.EvaluateCommandForContext("systemctl restart ssh", types.ConversationContext{Trust: types.TrustExternal})
	if result.Verdict != VerdictDeny {
		t.Fatalf("expected deny for non-owner high-risk command, got %s", result.Verdict)
	}
}

func TestPolicyAllowsSudoForOwner(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	result := engine.EvaluateCommandForContext("sudo apt update", types.ConversationContext{Trust: types.TrustOwner, IsOwner: true})
	if result.Verdict != VerdictAllow {
		t.Fatalf("expected allow for owner sudo command, got %s", result.Verdict)
	}
}

func TestPolicyStillBlocksSelfDestructiveCommandsWithSudo(t *testing.T) {
	engine := NewEngine(config.ToolsConfig{})
	result := engine.EvaluateCommandForContext("sudo rm -rf /", types.ConversationContext{Trust: types.TrustOwner, IsOwner: true})
	if result.Verdict != VerdictDeny {
		t.Fatalf("expected deny for destructive sudo command, got %s", result.Verdict)
	}
}
