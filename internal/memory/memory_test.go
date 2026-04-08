package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestExtractStructuredMemoriesFromSocialContactConversation(t *testing.T) {
	entries := ExtractStructuredMemories("social-1", "I'm with Northstar Studio and based in Berlin. Please keep replies concise.", "Happy to keep replies concise.", types.ConversationContext{
		Channel:    "telegram",
		SenderID:   "lead-1",
		SenderName: "Taylor",
		Trust:      types.TrustExternal,
	})
	if len(entries) == 0 {
		t.Fatal("expected extracted contact memories")
	}
	subject := ContactMemorySubject(types.ConversationContext{
		Channel:    "telegram",
		SenderID:   "lead-1",
		SenderName: "Taylor",
		Trust:      types.TrustExternal,
	})
	foundProfile := false
	foundInteraction := false
	for _, entry := range entries {
		if entry.Subject != subject {
			continue
		}
		if entry.Kind == "contact_profile" && strings.Contains(entry.Content, "Northstar Studio") {
			foundProfile = true
		}
		if entry.Kind == "interaction_note" && entry.HasTag("contact_subject:"+subject) {
			foundInteraction = true
		}
	}
	if !foundProfile {
		t.Fatalf("expected contact profile memory for %s, got %+v", subject, entries)
	}
	if !foundInteraction {
		t.Fatalf("expected tagged interaction note for %s, got %+v", subject, entries)
	}
}

func TestStoreArbitratesContactProfileEntry(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "memory.jsonl"))
	subject := ContactMemorySubject(types.ConversationContext{
		Channel:    "telegram",
		SenderID:   "lead-1",
		SenderName: "Taylor",
		Trust:      types.TrustExternal,
	})
	if err := store.Upsert(Entry{
		Key:        "person:" + subject + ":profile:organization",
		Layer:      "people",
		Area:       "contacts",
		Kind:       "contact_profile",
		Subject:    subject,
		Content:    "Contact organization or company: Northstar Studio.",
		Importance: 7,
		Confidence: 0.6,
		Source:     "social:conversation",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(Entry{
		Key:        "person:" + subject + ":profile:organization",
		Layer:      "people",
		Area:       "contacts",
		Kind:       "contact_profile",
		Subject:    subject,
		Content:    "Contact organization or company: Northstar Labs.",
		Importance: 8,
		Confidence: 0.9,
		Source:     "manual:social",
	}); err != nil {
		t.Fatal(err)
	}
	results, err := store.SearchWithOptions(SearchOptions{
		Layers:   []string{"people"},
		Areas:    []string{"contacts"},
		Subjects: []string{subject},
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected active contact profile result")
	}
	if results[0].Content != "Contact organization or company: Northstar Labs." {
		t.Fatalf("expected canonical contact profile to win arbitration, got %+v", results[0])
	}
}

func TestResolveContactIdentityMergesCrossPlatformByName(t *testing.T) {
	telegram := ResolveContactIdentity(types.ConversationContext{
		Channel:    "telegram",
		SenderID:   "lead-1",
		SenderName: "Taylor",
		Trust:      types.TrustExternal,
	}, "")
	slack := ResolveContactIdentity(types.ConversationContext{
		Channel:    "slack",
		SenderID:   "U123",
		SenderName: "Taylor",
		Trust:      types.TrustExternal,
	}, "")
	if telegram.CanonicalSubject == "" || slack.CanonicalSubject == "" {
		t.Fatalf("expected canonical subjects, got %+v and %+v", telegram, slack)
	}
	if telegram.CanonicalSubject != slack.CanonicalSubject {
		t.Fatalf("expected cross-platform contact merge by name, got %+v and %+v", telegram, slack)
	}
	if telegram.RouteKey == slack.RouteKey {
		t.Fatalf("expected per-platform route keys to remain distinct, got %+v and %+v", telegram, slack)
	}
}

func TestBuildContactCardSummarizesProfileAndPreferences(t *testing.T) {
	card := BuildContactCard([]Entry{
		{
			Subject: "person:taylor",
			Key:     "person:person:taylor:identity:display_name",
			Kind:    "contact_profile",
			Content: "Contact display name is Taylor.",
			Tags:    []string{"contact_route:telegram:lead-1", "channel:telegram"},
		},
		{
			Subject: "person:taylor",
			Key:     "person:person:taylor:profile:organization",
			Kind:    "contact_profile",
			Content: "Contact organization or company: Northstar Studio.",
		},
		{
			Subject: "person:taylor",
			Key:     "person:person:taylor:preference:concise",
			Kind:    "contact_preference",
			Content: "Please keep replies concise.",
		},
		{
			Subject:   "person:taylor",
			Kind:      "interaction_note",
			Content:   "Interaction with Taylor. Latest inbound: Can we discuss the proposal next week?",
			UpdatedAt: mustParseTime(t, "2026-04-08T12:00:00Z"),
		},
	})
	if card.DisplayName != "Taylor" || card.Organization != "Northstar Studio" {
		t.Fatalf("unexpected contact card core fields: %+v", card)
	}
	if len(card.Preferences) != 1 || card.Preferences[0] != "Please keep replies concise." {
		t.Fatalf("unexpected contact card preferences: %+v", card)
	}
	if len(card.Aliases) != 1 || card.Aliases[0] != "telegram:lead-1" {
		t.Fatalf("unexpected contact card aliases: %+v", card)
	}
	if !strings.Contains(FormatContactCard(card), "Northstar Studio") {
		t.Fatalf("expected formatted contact card to mention organization, got %q", FormatContactCard(card))
	}
}

func TestStoreRefreshContactCardPersistsSummaryEntry(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "memory.jsonl"))
	subject := "person:taylor"
	for _, entry := range []Entry{
		{
			Key:        "person:person:taylor:identity:display_name",
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_profile",
			Subject:    subject,
			Content:    "Contact display name is Taylor.",
			Importance: 8,
		},
		{
			Key:        "person:person:taylor:profile:organization",
			Layer:      "people",
			Area:       "contacts",
			Kind:       "contact_profile",
			Subject:    subject,
			Content:    "Contact organization or company: Northstar Studio.",
			Importance: 8,
		},
	} {
		if err := store.Upsert(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RefreshContactCard(subject); err != nil {
		t.Fatal(err)
	}
	results, err := store.SearchWithOptions(SearchOptions{
		Layers:           []string{"people"},
		Areas:            []string{"contacts"},
		Subjects:         []string{subject},
		Kinds:            []string{"contact_summary"},
		Limit:            5,
		IncludeSummaries: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one contact summary entry, got %d", len(results))
	}
	if !strings.Contains(results[0].Content, "Northstar Studio") {
		t.Fatalf("expected contact summary to mention organization, got %+v", results[0])
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
