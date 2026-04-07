package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/memory"
	"qorvexus/internal/model"
	"qorvexus/internal/plan"
	"qorvexus/internal/session"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
)

type stubClient struct {
	reply       string
	lastRequest model.CompletionRequest
}

func (s *stubClient) Complete(_ context.Context, req model.CompletionRequest) (*model.CompletionResponse, error) {
	s.lastRequest = req
	return &model.CompletionResponse{
		Message: types.Message{
			Role:    types.RoleAssistant,
			Content: s.reply,
		},
	}, nil
}

func TestPickModelUsesVisionFallback(t *testing.T) {
	registry := model.NewRegistry()
	registry.Register("text", config.ModelConfig{Model: "text-only", Vision: false}, nil)
	registry.Register("vision", config.ModelConfig{Model: "vision", Vision: true}, nil)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel:        "text",
				VisionFallbackModel: "vision",
			},
		},
		Models: registry,
	}
	got := runner.pickModel(Request{
		Parts: []types.ContentPart{{Type: "image_url", ImageURL: "https://example.com/demo.png"}},
	})
	if got != "vision" {
		t.Fatalf("expected vision model, got %s", got)
	}
}

func TestRunnerInjectsRelevantMemoryIntoPrompt(t *testing.T) {
	tempDir := t.TempDir()
	mem := memory.NewStore(filepath.Join(tempDir, "memory.jsonl"))
	if err := mem.Upsert(memory.Entry{
		Key:        "owner:rule:no-outbound",
		Area:       "owner_rules",
		Kind:       "rule",
		Content:    "Never send external messages without explicit owner approval.",
		Importance: 10,
	}); err != nil {
		t.Fatal(err)
	}

	registry := model.NewRegistry()
	client := &stubClient{reply: "Understood."}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     1,
			},
		},
		Models:     registry,
		Sessions:   session.NewStore(tempDir),
		Tools:      tool.NewRegistry(),
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
		Memory:     mem,
	}

	_, _, err := runner.Run(context.Background(), Request{
		SessionID: "sess-1",
		Prompt:    "Help me draft a reply",
		Context: &types.ConversationContext{
			IsOwner: true,
			Trust:   types.TrustOwner,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, msg := range client.lastRequest.Messages {
		if msg.Role == types.RoleSystem && strings.Contains(msg.Content, "Never send external messages without explicit owner approval.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected relevant memory to be injected into system prompt, got %#v", client.lastRequest.Messages)
	}
}

func TestRunnerCapturesOwnerMemoriesAutomatically(t *testing.T) {
	tempDir := t.TempDir()
	mem := memory.NewStore(filepath.Join(tempDir, "memory.jsonl"))
	registry := model.NewRegistry()
	client := &stubClient{reply: "I will remember that."}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)

	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     1,
			},
		},
		Models:     registry,
		Sessions:   session.NewStore(tempDir),
		Tools:      tool.NewRegistry(),
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
		Memory:     mem,
	}

	_, _, err := runner.Run(context.Background(), Request{
		SessionID: "owner-onboarding",
		Prompt:    "Call me Alex. My timezone is Asia/Shanghai. Please answer in Chinese. Never publish anything without my approval.",
		Context: &types.ConversationContext{
			IsOwner: true,
			Trust:   types.TrustOwner,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := mem.SearchWithOptions(memory.SearchOptions{
		Areas: []string{"owner_profile", "owner_preferences", "owner_rules"},
		Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 4 {
		t.Fatalf("expected captured owner memories, got %d", len(results))
	}
}

func TestRunnerInjectsActivePlanIntoPrompt(t *testing.T) {
	tempDir := t.TempDir()
	plans := plan.NewStore(filepath.Join(tempDir, "plans.json"))
	_, err := plans.Create(plan.Plan{
		Goal:      "Ship planner support",
		SessionID: "sess-plan",
		Steps: []plan.Step{
			{ID: "inspect", Title: "Inspect code", Status: plan.StepStatusSucceeded, Result: "Mapped the current runtime and queue integration points."},
			{ID: "implement", Title: "Implement planner store"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	registry := model.NewRegistry()
	client := &stubClient{reply: "On it."}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     1,
			},
		},
		Models:     registry,
		Sessions:   session.NewStore(tempDir),
		Tools:      tool.NewRegistry(),
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
		Plans:      plans,
	}

	_, _, err = runner.Run(context.Background(), Request{
		SessionID: "sess-plan",
		Prompt:    "Keep going",
		Context: &types.ConversationContext{
			IsOwner: true,
			Trust:   types.TrustOwner,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, msg := range client.lastRequest.Messages {
		if msg.Role == types.RoleSystem && strings.Contains(msg.Content, "Active execution plans:") && strings.Contains(msg.Content, "Implement planner store") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected active plan prompt injection, got %#v", client.lastRequest.Messages)
	}
}
