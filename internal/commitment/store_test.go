package commitment

import (
	"path/filepath"
	"testing"
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
