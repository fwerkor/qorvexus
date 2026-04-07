package plan

import (
	"path/filepath"
	"testing"
)

func TestStoreCreateUpdateAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	store := NewStore(path)

	created, err := store.Create(Plan{
		Goal:      "Ship planner support",
		SessionID: "sess-1",
		Steps: []Step{
			{ID: "inspect", Title: "Inspect code"},
			{ID: "implement", Title: "Implement planner", DependsOn: []string{"inspect"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusActive {
		t.Fatalf("expected active plan, got %s", created.Status)
	}

	runnable := RunnableSteps(created)
	if len(runnable) != 1 || runnable[0].ID != "inspect" {
		t.Fatalf("expected inspect to be runnable, got %#v", runnable)
	}

	updated, err := store.UpdateStep(created.ID, "inspect", func(item *Plan, step *Step) error {
		step.Status = StepStatusSucceeded
		step.Result = "context gathered"
		step.Notes = append(step.Notes, "done")
		item.Notes = append(item.Notes, "first pass complete")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusActive {
		t.Fatalf("expected plan to remain active, got %s", updated.Status)
	}

	runnable = RunnableSteps(updated)
	if len(runnable) != 1 || runnable[0].ID != "implement" {
		t.Fatalf("expected implement to be runnable after dependency, got %#v", runnable)
	}

	reloaded := NewStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	step, ok := FindStep(got, "inspect")
	if !ok {
		t.Fatal("expected inspect step to exist after reload")
	}
	if step.Status != StepStatusSucceeded {
		t.Fatalf("expected succeeded step after reload, got %s", step.Status)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "first pass complete" {
		t.Fatalf("expected plan notes to persist, got %#v", got.Notes)
	}
}

func TestActiveForSessionIgnoresCompletedPlans(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "plans.json"))
	completed, err := store.Create(Plan{
		Goal:      "Done",
		SessionID: "sess-1",
		Steps: []Step{
			{ID: "done", Title: "Done", Status: StepStatusSucceeded},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted {
		t.Fatalf("expected completed plan, got %s", completed.Status)
	}
	if _, err := store.Create(Plan{
		Goal:      "Open",
		SessionID: "sess-1",
		Steps: []Step{
			{ID: "next", Title: "Next"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	active := store.ActiveForSession("sess-1", 10)
	if len(active) != 1 {
		t.Fatalf("expected one open plan, got %d", len(active))
	}
	if active[0].Goal != "Open" {
		t.Fatalf("expected open plan, got %#v", active[0])
	}
}
