package memory

import (
	"path/filepath"
	"testing"
)

func TestStoreAppendAndSearch(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "memory.jsonl"))
	if err := store.Append(Entry{Content: "User prefers Chinese updates", Tags: []string{"preference"}}); err != nil {
		t.Fatal(err)
	}
	results, err := store.Search("Chinese", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}
