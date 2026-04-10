package tool

import (
	"strings"
	"testing"

	"qorvexus/internal/config"
)

func TestCommandToolPromptMentionsProcessRouting(t *testing.T) {
	def := NewCommandTool(config.ToolsConfig{}, nil).Definition()
	if !strings.Contains(def.Description, "manage_process") {
		t.Fatalf("expected run_command description to mention manage_process, got %q", def.Description)
	}
	props := def.Parameters.(map[string]any)["properties"].(map[string]any)
	commandProp := props["command"].(map[string]any)
	if strings.TrimSpace(commandProp["description"].(string)) == "" {
		t.Fatal("expected command property description")
	}
}

func TestCreatePlanToolStepSchemaHasPromptGuidance(t *testing.T) {
	def := NewCreatePlanTool(nil).Definition()
	props := def.Parameters.(map[string]any)["properties"].(map[string]any)
	steps := props["steps"].(map[string]any)
	stepItems := steps["items"].(map[string]any)
	stepProps := stepItems["properties"].(map[string]any)

	for _, key := range []string{"prompt", "review_prompt", "verify_prompt", "rollback_prompt", "degrade_prompt"} {
		prop := stepProps[key].(map[string]any)
		if strings.TrimSpace(prop["description"].(string)) == "" {
			t.Fatalf("expected %s to include a description", key)
		}
	}
}

func TestSocialSendToolPromptMentionsHoldAlternative(t *testing.T) {
	def := NewSocialSendTool(nil).Definition()
	if !strings.Contains(def.Description, "hold_social_message") {
		t.Fatalf("expected send_social_message description to mention hold_social_message, got %q", def.Description)
	}
}
