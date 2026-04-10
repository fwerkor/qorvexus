package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/memory"
	"qorvexus/internal/model"
	"qorvexus/internal/plan"
	"qorvexus/internal/session"
	"qorvexus/internal/skill"
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

type sequencedStubClient struct {
	replies  []types.Message
	requests []model.CompletionRequest
	calls    int
}

func (s *sequencedStubClient) Complete(_ context.Context, req model.CompletionRequest) (*model.CompletionResponse, error) {
	if s.calls >= len(s.replies) {
		return nil, context.DeadlineExceeded
	}
	s.requests = append(s.requests, req)
	msg := s.replies[s.calls]
	s.calls++
	return &model.CompletionResponse{Message: msg}, nil
}

type echoTool struct{}

func (echoTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "echo",
		Description: "Echo input.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
	}
}

func (echoTool) Invoke(_ context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return "echo:" + input.Text, nil
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

func TestRunnerInjectsSkillInstructionsIntoSystemPrompt(t *testing.T) {
	tempDir := t.TempDir()
	registry := model.NewRegistry()
	client := &stubClient{reply: "Applied."}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     1,
			},
		},
		Models:   registry,
		Sessions: session.NewStore(tempDir),
		Tools:    tool.NewRegistry(),
		Skills: []skill.Skill{
			{
				Name:         "self-improver",
				Description:  "Improve Qorvexus safely.",
				Instructions: "Use restart_runtime after config changes.\nUse apply_self_update after source changes.",
				Location:     "/tmp/skills/self-improver",
			},
		},
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
	}

	_, _, err := runner.Run(context.Background(), Request{
		SessionID: "sess-skills",
		Prompt:    "Update yourself.",
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
		if msg.Role != types.RoleSystem {
			continue
		}
		if strings.Contains(msg.Content, "Use restart_runtime after config changes.") &&
			strings.Contains(msg.Content, "Use apply_self_update after source changes.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected skill instructions to be injected into system prompt, got %#v", client.lastRequest.Messages)
	}
}

func TestRunnerInjectsCurrentContactMemoryIntoPrompt(t *testing.T) {
	tempDir := t.TempDir()
	mem := memory.NewStore(filepath.Join(tempDir, "memory.jsonl"))
	subject := memory.ContactMemorySubject(types.ConversationContext{
		Channel:    "telegram",
		SenderID:   "lead-1",
		SenderName: "Taylor",
		Trust:      types.TrustExternal,
	})
	if err := mem.Upsert(memory.Entry{
		Key:        "person:" + subject + ":profile:organization",
		Layer:      "people",
		Area:       "contacts",
		Kind:       "contact_profile",
		Subject:    subject,
		Content:    "Contact organization or company: Northstar Studio.",
		Importance: 8,
		Confidence: 0.9,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Upsert(memory.Entry{
		Key:        "person:" + subject + ":preference:concise",
		Layer:      "people",
		Area:       "contacts",
		Kind:       "contact_preference",
		Subject:    subject,
		Content:    "Please keep replies concise.",
		Importance: 7,
		Confidence: 0.8,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Upsert(memory.Entry{
		Key:        "person:telegram:other-1:profile:organization",
		Layer:      "people",
		Area:       "contacts",
		Kind:       "contact_profile",
		Subject:    "telegram:other-1",
		Content:    "Contact organization or company: Other Corp.",
		Importance: 8,
		Confidence: 0.9,
	}); err != nil {
		t.Fatal(err)
	}

	registry := model.NewRegistry()
	client := &stubClient{reply: "Will do."}
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
		SessionID: "sess-contact",
		Prompt:    "Reply politely.",
		Context: &types.ConversationContext{
			Channel:      "telegram",
			ThreadID:     "thread-1",
			SenderID:     "lead-1",
			SenderName:   "Taylor",
			Trust:        types.TrustExternal,
			ReplyAsAgent: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	foundContact := false
	for _, msg := range client.lastRequest.Messages {
		if msg.Role != types.RoleSystem {
			continue
		}
		if strings.Contains(msg.Content, "Current contact memory:") &&
			strings.Contains(msg.Content, "Northstar Studio") &&
			strings.Contains(msg.Content, "Please keep replies concise.") {
			foundContact = true
			break
		}
	}
	if !foundContact {
		t.Fatalf("expected current contact memory to be injected into system prompt, got %#v", client.lastRequest.Messages)
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

func TestRunnerStripsThinkingBlocksBeforeSavingSession(t *testing.T) {
	tempDir := t.TempDir()
	registry := model.NewRegistry()
	client := &stubClient{reply: "<think>private chain of thought</think>\nFinal answer."}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)

	store := session.NewStore(tempDir)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     1,
			},
		},
		Models:     registry,
		Sessions:   store,
		Tools:      tool.NewRegistry(),
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
	}

	state, out, err := runner.Run(context.Background(), Request{
		SessionID: "sess-think-strip",
		Prompt:    "Say hello",
		Context: &types.ConversationContext{
			IsOwner: true,
			Trust:   types.TrustOwner,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Final answer." {
		t.Fatalf("expected sanitized assistant output, got %q", out)
	}
	if got := state.Messages[len(state.Messages)-1].Content; got != "Final answer." {
		t.Fatalf("expected saved assistant message to be sanitized, got %q", got)
	}
	loaded, err := store.Load("sess-think-strip")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "Final answer." {
		t.Fatalf("expected persisted assistant message to be sanitized, got %q", got)
	}
}

func TestRunnerStripsControlTokensBeforeSavingSession(t *testing.T) {
	tempDir := t.TempDir()
	registry := model.NewRegistry()
	client := &stubClient{reply: "<|channel|>\nFinal answer."}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)

	store := session.NewStore(tempDir)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     1,
			},
		},
		Models:     registry,
		Sessions:   store,
		Tools:      tool.NewRegistry(),
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
	}

	state, out, err := runner.Run(context.Background(), Request{
		SessionID: "sess-control-token-strip",
		Prompt:    "Say hello",
		Context: &types.ConversationContext{
			IsOwner: true,
			Trust:   types.TrustOwner,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Final answer." {
		t.Fatalf("expected sanitized assistant output, got %q", out)
	}
	if got := state.Messages[len(state.Messages)-1].Content; got != "Final answer." {
		t.Fatalf("expected saved assistant message to be sanitized, got %q", got)
	}
	loaded, err := store.Load("sess-control-token-strip")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "Final answer." {
		t.Fatalf("expected persisted assistant message to be sanitized, got %q", got)
	}
}

func TestRunnerSanitizesThinkingFromLoadedHistory(t *testing.T) {
	tempDir := t.TempDir()
	store := session.NewStore(tempDir)
	if err := store.Save(&session.State{
		ID:    "sess-history-strip",
		Model: "primary",
		Messages: []types.Message{
			{Role: types.RoleSystem, Content: "You are helpful."},
			{Role: types.RoleAssistant, Content: "<thinking>hidden</thinking>\nVisible context."},
		},
	}); err != nil {
		t.Fatal(err)
	}

	registry := model.NewRegistry()
	client := &stubClient{reply: "Next answer."}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     1,
			},
		},
		Models:     registry,
		Sessions:   store,
		Tools:      tool.NewRegistry(),
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
	}

	_, _, err := runner.Run(context.Background(), Request{
		SessionID: "sess-history-strip",
		Prompt:    "Continue",
		Context: &types.ConversationContext{
			IsOwner: true,
			Trust:   types.TrustOwner,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	foundVisible := false
	for _, msg := range client.lastRequest.Messages {
		if msg.Role != types.RoleAssistant {
			continue
		}
		if strings.Contains(msg.Content, "<thinking>") || strings.Contains(msg.Content, "<think>") {
			t.Fatalf("expected reasoning blocks to be removed from replayed history, got %#v", client.lastRequest.Messages)
		}
		if strings.Contains(msg.Content, "Visible context.") {
			foundVisible = true
		}
	}
	if !foundVisible {
		t.Fatalf("expected sanitized visible assistant history to remain, got %#v", client.lastRequest.Messages)
	}
}

func TestRunnerEmitsAssistantPrefaceBeforeToolExecution(t *testing.T) {
	tempDir := t.TempDir()
	registry := model.NewRegistry()
	client := &sequencedStubClient{
		replies: []types.Message{
			{
				Role:    types.RoleAssistant,
				Content: "I will check that first.",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "echo", Arguments: `{"text":"demo"}`},
				},
			},
			{
				Role:    types.RoleAssistant,
				Content: "Done checking.",
			},
		},
	}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	tools := tool.NewRegistry()
	tools.Register(echoTool{})
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     3,
			},
		},
		Models:     registry,
		Sessions:   session.NewStore(tempDir),
		Tools:      tools,
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
	}

	var emitted []string
	state, out, err := runner.Run(context.Background(), Request{
		SessionID: "sess-preface",
		Prompt:    "Check now",
		Context: &types.ConversationContext{
			Channel:      "telegram",
			ThreadID:     "thread-1",
			SenderID:     "lead-1",
			SenderName:   "Taylor",
			Trust:        types.TrustExternal,
			ReplyAsAgent: true,
		},
		OnAssistantMessage: func(_ context.Context, msg types.Message) error {
			emitted = append(emitted, msg.Content)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 1 || emitted[0] != "I will check that first." {
		t.Fatalf("expected tool-preface callback once, got %#v", emitted)
	}
	if out != "Done checking." {
		t.Fatalf("expected final output, got %q", out)
	}
	if len(state.Messages) < 4 {
		t.Fatalf("expected conversation with tool steps, got %#v", state.Messages)
	}
	foundTool := false
	for _, msg := range state.Messages {
		if msg.Role == types.RoleTool && strings.Contains(msg.Content, "echo:demo") {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("expected tool result in session, got %#v", state.Messages)
	}
}

func TestRunnerDrainsPendingUserMessagesAfterToolExecution(t *testing.T) {
	tempDir := t.TempDir()
	registry := model.NewRegistry()
	client := &sequencedStubClient{
		replies: []types.Message{
			{
				Role:    types.RoleAssistant,
				Content: "Checking now.",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "echo", Arguments: `{"text":"demo"}`},
				},
			},
			{
				Role:    types.RoleAssistant,
				Content: "Saw the follow-up too.",
			},
		},
	}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	tools := tool.NewRegistry()
	tools.Register(echoTool{})
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     3,
			},
		},
		Models:     registry,
		Sessions:   session.NewStore(tempDir),
		Tools:      tools,
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
	}

	drained := false
	_, out, err := runner.Run(context.Background(), Request{
		SessionID: "sess-pending",
		Prompt:    "First question",
		Context: &types.ConversationContext{
			Channel:      "telegram",
			ThreadID:     "thread-1",
			SenderID:     "lead-1",
			SenderName:   "Taylor",
			Trust:        types.TrustExternal,
			ReplyAsAgent: true,
		},
		DrainPendingUserMessages: func(_ context.Context, st *session.State) ([]types.Message, error) {
			if drained {
				return nil, nil
			}
			drained = true
			st.Context.SenderName = "Taylor"
			return []types.Message{{Role: types.RoleUser, Content: "Second question"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Saw the follow-up too." {
		t.Fatalf("expected final output, got %q", out)
	}
	if client.calls != 2 || len(client.requests) != 2 {
		t.Fatalf("expected two model calls, got %d", client.calls)
	}
	foundPending := false
	for _, msg := range client.requests[1].Messages {
		if msg.Role == types.RoleUser && strings.Contains(msg.Content, "Second question") {
			foundPending = true
			break
		}
	}
	if !foundPending {
		t.Fatalf("expected second model call to include drained user message, got %#v", client.requests[1].Messages)
	}
}

func TestRunnerPersistsToolProgressBeforeLaterFailure(t *testing.T) {
	tempDir := t.TempDir()
	registry := model.NewRegistry()
	client := &failingAfterToolClient{}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	tools := tool.NewRegistry()
	tools.Register(echoTool{})
	store := session.NewStore(tempDir)
	runner := &Runner{
		Config: &config.Config{
			Agent: config.AgentConfig{
				DefaultModel: "primary",
				MaxTurns:     3,
			},
		},
		Models:     registry,
		Sessions:   store,
		Tools:      tools,
		Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9},
	}

	_, _, err := runner.Run(context.Background(), Request{
		SessionID: "sess-error-after-tool",
		Prompt:    "Check now",
	})
	if err == nil {
		t.Fatal("expected runner to fail after tool execution")
	}

	state, loadErr := store.Load("sess-error-after-tool")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	foundAssistantToolCall := false
	foundToolResult := false
	for _, msg := range state.Messages {
		if msg.Role == types.RoleAssistant && len(msg.ToolCalls) == 1 && msg.ToolCalls[0].Name == "echo" {
			foundAssistantToolCall = true
		}
		if msg.Role == types.RoleTool && strings.Contains(msg.Content, "echo:demo") {
			foundToolResult = true
		}
	}
	if !foundAssistantToolCall {
		t.Fatalf("expected persisted assistant tool call, got %#v", state.Messages)
	}
	if !foundToolResult {
		t.Fatalf("expected persisted tool result, got %#v", state.Messages)
	}
}

type failingAfterToolClient struct {
	calls int
}

func (c *failingAfterToolClient) Complete(_ context.Context, _ model.CompletionRequest) (*model.CompletionResponse, error) {
	if c.calls == 0 {
		c.calls++
		return &model.CompletionResponse{
			Message: types.Message{
				Role:    types.RoleAssistant,
				Content: "I will check that.",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "echo", Arguments: `{"text":"demo"}`},
				},
			},
		}, nil
	}
	return nil, errors.New("model backend unavailable")
}
