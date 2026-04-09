package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderReadsSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: demo
description: Demo skill
---
Follow these instructions.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skills, err := NewLoader().LoadDirs([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "demo" {
		t.Fatalf("unexpected skill name: %s", skills[0].Name)
	}
}

func TestPromptIncludesSkillInstructions(t *testing.T) {
	prompt := Prompt([]Skill{{
		Name:         "self-improver",
		Description:  "Improve Qorvexus safely.",
		Instructions: "Use restart_runtime after config changes.\nUse apply_self_update after source changes.",
		Location:     "/tmp/skills/self-improver",
	}})
	for _, needle := range []string{
		"Skill: self-improver",
		"Description: Improve Qorvexus safely.",
		"Use restart_runtime after config changes.",
		"Use apply_self_update after source changes.",
		"Location: /tmp/skills/self-improver",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected prompt to include %q, got %q", needle, prompt)
		}
	}
}
