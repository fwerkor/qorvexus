package taskqueue

import (
	"context"
	"path/filepath"
	"testing"
)

type stubRunner struct{}

func (stubRunner) RunQueuedTask(context.Context, Task) (string, error) {
	return "done", nil
}

func TestQueueAddAndRunNext(t *testing.T) {
	queue := New(filepath.Join(t.TempDir(), "queue.json"), stubRunner{})
	task, err := queue.Add(Task{Name: "demo", Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusQueued {
		t.Fatalf("expected queued status, got %s", task.Status)
	}
	ran, err := queue.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("expected a task to run")
	}
	if got := queue.List()[0].Status; got != StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", got)
	}
}

func TestQueueHasActiveSession(t *testing.T) {
	queue := New(filepath.Join(t.TempDir(), "queue.json"), stubRunner{})
	if _, err := queue.Add(Task{Name: "demo", Prompt: "hello", SessionID: "cron-task-1"}); err != nil {
		t.Fatal(err)
	}
	if !queue.HasActiveSession("cron-task-1") {
		t.Fatal("expected queued task session to be active")
	}
	if _, err := queue.RunNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if queue.HasActiveSession("cron-task-1") {
		t.Fatal("expected succeeded task session to no longer be active")
	}
}
