package session

import (
	"path/filepath"
	"testing"

	"qorvexus/internal/types"
)

func TestLoadReturnsDetachedCopy(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.Save(&State{
		ID:    "sess-1",
		Model: "primary",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}

	first, err := store.Load("sess-1")
	if err != nil {
		t.Fatalf("load first session: %v", err)
	}
	first.Messages[0].Content = "mutated"

	second, err := store.Load("sess-1")
	if err != nil {
		t.Fatalf("load second session: %v", err)
	}
	if second.Messages[0].Content != "hello" {
		t.Fatalf("expected cached session to stay unchanged, got %+v", second.Messages)
	}
}

func TestSaveCachesDetachedCopy(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "root"))
	state := &State{
		ID:    "sess-2",
		Model: "primary",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "before"},
		},
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("save session: %v", err)
	}
	state.Messages[0].Content = "after"

	loaded, err := store.Load("sess-2")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.Messages[0].Content != "before" {
		t.Fatalf("expected saved snapshot to remain immutable, got %+v", loaded.Messages)
	}
}
