package memory

import (
	"path/filepath"
	"testing"

	"qorvexus/internal/types"
)

func TestStoreUpsertAndAreaSearch(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "memory.jsonl"))
	if err := store.Upsert(Entry{
		Key:        "owner:identity:name",
		Area:       "owner_profile",
		Kind:       "identity",
		Content:    "Owner's name is Alex.",
		Importance: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(Entry{
		Key:        "owner:identity:name",
		Area:       "owner_profile",
		Kind:       "identity",
		Content:    "Owner's name is Alex Chen.",
		Importance: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(Entry{
		Key:        "owner:rule:1",
		Area:       "owner_rules",
		Kind:       "rule",
		Content:    "Never send external messages without explicit approval.",
		Importance: 10,
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchWithOptions(SearchOptions{
		Query: "Alex",
		Areas: []string{"owner_profile"},
		Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 owner_profile result, got %d", len(results))
	}
	if results[0].Content != "Owner's name is Alex Chen." {
		t.Fatalf("expected upserted content, got %q", results[0].Content)
	}
}

func TestExtractStructuredMemoriesFromOwnerConversation(t *testing.T) {
	entries := ExtractStructuredMemories("owner-onboarding", "Call me Alex. My timezone is Asia/Shanghai. Please answer in Chinese. Never deploy on Friday.", "", types.ConversationContext{
		IsOwner: true,
		Trust:   types.TrustOwner,
	})
	if len(entries) < 4 {
		t.Fatalf("expected multiple extracted memories, got %d", len(entries))
	}
	areas := map[string]bool{}
	for _, entry := range entries {
		areas[entry.Area] = true
	}
	if !areas["owner_profile"] || !areas["owner_preferences"] || !areas["owner_rules"] {
		t.Fatalf("expected owner profile, preferences, and rules areas, got %+v", areas)
	}
}
