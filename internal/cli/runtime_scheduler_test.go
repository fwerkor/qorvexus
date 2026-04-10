package cli

import (
	"context"
	"path/filepath"
	"testing"

	"qorvexus/internal/scheduler"
	"qorvexus/internal/taskqueue"
)

func TestRunScheduledSkipsDuplicateActiveCronTask(t *testing.T) {
	queue := taskqueue.New(filepath.Join(t.TempDir(), "queue.json"), stubQueueRunner{})
	app := &appRuntime{queue: queue}
	task := scheduler.Task{
		ID:       "task-1",
		Name:     "demo",
		Prompt:   "hello",
		Model:    "primary",
		Schedule: "0 */5 * * * *",
	}

	if err := app.RunScheduled(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := app.RunScheduled(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	items := queue.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 queued task, got %d", len(items))
	}
}

type stubQueueRunner struct{}

func (stubQueueRunner) RunQueuedTask(context.Context, taskqueue.Task) (string, error) {
	return "done", nil
}
