package commitment

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAppendAndUpdateStatus(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "commitments.jsonl"))

	entry, err := store.Append(Entry{
		Channel:      "telegram",
		Counterparty: "Prospect",
		Summary:      "Send a proposal draft",
		DueHint:      "next week",
		Trust:        "external",
	})
	if err != nil {
		t.Fatalf("append commitment: %v", err)
	}
	if entry.Status != StatusOpen {
		t.Fatalf("expected open status, got %s", entry.Status)
	}

	if err := store.AttachTask(entry.ID, "queue-1"); err != nil {
		t.Fatalf("attach task: %v", err)
	}
	if err := store.UpdateStatus(entry.ID, StatusCompleted); err != nil {
		t.Fatalf("update status: %v", err)
	}

	items, err := store.List(10)
	if err != nil {
		t.Fatalf("list commitments: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 commitment, got %d", len(items))
	}
	if items[0].RelatedTaskID != "queue-1" || items[0].Status != StatusCompleted {
		t.Fatalf("unexpected stored commitment: %+v", items[0])
	}
}

func TestStoreSummary(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "commitments.jsonl"))

	_, _ = store.Append(Entry{Channel: "telegram", Summary: "Send quote", DueHint: "tomorrow", Trust: "external"})
	_, _ = store.Append(Entry{Channel: "slack", Summary: "Share update", Trust: "trusted", Status: StatusCompleted})
	_, _ = store.Append(Entry{Channel: "telegram", Summary: "Check back", Trust: "external", Status: StatusOverdue})

	summary, err := store.Summary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Total != 3 || summary.Open != 1 || summary.Completed != 1 || summary.Overdue != 1 {
		t.Fatalf("unexpected summary counts: %+v", summary)
	}
	if summary.WithDueHint != 1 || summary.ByChannel["telegram"] != 2 || summary.ByTrust["external"] != 2 {
		t.Fatalf("unexpected summary breakdown: %+v", summary)
	}
}

func TestStoreNoteReviewQueued(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "commitments.jsonl"))
	entry, err := store.Append(Entry{
		Channel: "telegram",
		Summary: "Send update",
	})
	if err != nil {
		t.Fatalf("append commitment: %v", err)
	}
	at := time.Now().UTC()
	if err := store.NoteReviewQueued(entry.ID, "queue-2", 2, at); err != nil {
		t.Fatalf("note review queued: %v", err)
	}
	got, err := store.Get(entry.ID)
	if err != nil {
		t.Fatalf("get commitment: %v", err)
	}
	if got.LastReviewTaskID != "queue-2" || got.RelatedTaskID != "queue-2" || got.EscalationLevel != 2 {
		t.Fatalf("unexpected queued review state: %+v", got)
	}
	if got.LastReviewAt.IsZero() {
		t.Fatalf("expected last review timestamp to be set")
	}
}
