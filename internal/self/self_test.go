package self

import (
	"path/filepath"
	"testing"
)

func TestManagerWritesSkillAndBacklog(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "skills"), filepath.Join(dir, "backlog.jsonl"))
	path, err := mgr.UpsertSkill("demo", "Demo skill", "Use the demo flow.")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected skill path")
	}
	if err := mgr.AppendBacklog(BacklogEntry{Title: "Improve demo", Description: "Add more steps"}); err != nil {
		t.Fatal(err)
	}
	items, err := mgr.ListBacklog(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 backlog item, got %d", len(items))
	}
}
