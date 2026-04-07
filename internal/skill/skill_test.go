package skill

import (
	"os"
	"path/filepath"
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
