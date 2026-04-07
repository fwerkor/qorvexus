package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/model"
	"qorvexus/internal/types"
)

type embeddingStub struct{}

func (s *embeddingStub) Complete(_ context.Context, req model.CompletionRequest) (*model.CompletionResponse, error) {
	return &model.CompletionResponse{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: req.Messages[0].Content,
		},
	}, nil
}

func (s *embeddingStub) Embed(_ context.Context, req model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	resp := &model.EmbeddingResponse{Model: req.Model}
	for _, input := range req.Inputs {
		lower := strings.ToLower(input)
		switch {
		case strings.Contains(lower, "locale") || strings.Contains(lower, "timezone") || strings.Contains(lower, "utc offset"):
			resp.Vectors = append(resp.Vectors, []float64{1, 0, 0})
		case strings.Contains(lower, "proposal") || strings.Contains(lower, "quote"):
			resp.Vectors = append(resp.Vectors, []float64{0, 1, 0})
		default:
			resp.Vectors = append(resp.Vectors, []float64{0, 0, 1})
		}
	}
	return resp, nil
}

func TestStoreUpsertArbitratesCanonicalEntry(t *testing.T) {
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
		Confidence: 0.9,
		Source:     "manual:owner",
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
		Areas: []string{"owner_profile"},
		Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 active owner_profile result, got %d", len(results))
	}
	if results[0].Content != "Owner's name is Alex Chen." {
		t.Fatalf("expected canonical content, got %q", results[0].Content)
	}

	all, err := store.SearchWithOptions(SearchOptions{
		Areas:             []string{"owner_profile"},
		Limit:             10,
		IncludeArchived:   true,
		IncludeSuperseded: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected canonical and superseded entry, got %d", len(all))
	}
}

func TestStoreUsesSemanticEmbeddingsForRecall(t *testing.T) {
	registry := model.NewRegistry()
	registry.Register("memory-embed", config.ModelConfig{Model: "memory-embed"}, &embeddingStub{})
	store := NewStoreWithOptions(Options{
		Path:           filepath.Join(t.TempDir(), "memory.jsonl"),
		Models:         registry,
		EmbeddingModel: "memory-embed",
		SemanticSearch: true,
	})
	if err := store.Upsert(Entry{
		Key:        "owner:identity:timezone",
		Area:       "owner_profile",
		Kind:       "identity",
		Content:    "Owner locale is Asia/Shanghai.",
		Importance: 8,
		Confidence: 0.9,
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchWithOptions(SearchOptions{
		Query: "What UTC offset is the owner in?",
		Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected semantic search result")
	}
	if !strings.Contains(results[0].Content, "Asia/Shanghai") {
		t.Fatalf("expected timezone memory, got %#v", results[0])
	}
}

func TestStoreCompactsWorkflowMemoriesIntoSummary(t *testing.T) {
	store := NewStoreWithOptions(Options{
		Path:                filepath.Join(t.TempDir(), "memory.jsonl"),
		SemanticSearch:      true,
		CompactionThreshold: 4,
		CompactionRetain:    2,
		MaxSummarySources:   4,
	})
	for i := 0; i < 6; i++ {
		if err := store.Append(Entry{
			Layer:      "workflow",
			Area:       "workflow",
			Kind:       "conversation_outcome",
			Subject:    "sess-1",
			Content:    "Workflow note number " + string(rune('A'+i)),
			Importance: 4,
			Confidence: 0.6,
		}); err != nil {
			t.Fatal(err)
		}
	}

	all, err := store.SearchWithOptions(SearchOptions{
		Layers:            []string{"workflow"},
		Limit:             20,
		IncludeArchived:   true,
		IncludeSummaries:  true,
		IncludeSuperseded: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	hasSummary := false
	compacted := 0
	for _, entry := range all {
		if entry.Kind == "summary" {
			hasSummary = true
		}
		if entry.State == EntryStateCompacted {
			compacted++
		}
	}
	if !hasSummary {
		t.Fatal("expected workflow summary entry after compaction")
	}
	if compacted == 0 {
		t.Fatal("expected some workflow entries to be compacted")
	}
}

func TestExtractStructuredMemoriesFromOwnerConversation(t *testing.T) {
	entries := ExtractStructuredMemories("sess-owner", "Call me Alex. My timezone is Asia/Shanghai. Please answer in Chinese. Never deploy on Friday. I am working on project Atlas.", "I will help with Atlas.", types.ConversationContext{
		IsOwner: true,
		Trust:   types.TrustOwner,
	})
	if len(entries) < 5 {
		t.Fatalf("expected multiple extracted memories, got %d", len(entries))
	}
	areas := map[string]bool{}
	layers := map[string]bool{}
	for _, entry := range entries {
		areas[entry.Area] = true
		layers[entry.Layer] = true
	}
	if !areas["owner_profile"] || !areas["owner_preferences"] || !areas["owner_rules"] {
		t.Fatalf("expected owner profile, preferences, and rules areas, got %+v", areas)
	}
	if !layers["owner"] || !layers["projects"] || !layers["workflow"] {
		t.Fatalf("expected owner, projects, and workflow layers, got %+v", layers)
	}
}
