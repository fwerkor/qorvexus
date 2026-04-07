package scheduler

import (
	"context"
	"path/filepath"
	"testing"
)

type stubRunner struct{}

func (stubRunner) RunScheduled(context.Context, Task) error { return nil }

func TestManagerAddPersistsTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	manager := NewManager(path, stubRunner{})
	if err := manager.Add(Task{
		Name:     "demo",
		Schedule: "*/30 * * * * *",
		Prompt:   "hello",
	}); err != nil {
		t.Fatal(err)
	}
	loaded := NewManager(path, stubRunner{})
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if len(loaded.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(loaded.tasks))
	}
}
