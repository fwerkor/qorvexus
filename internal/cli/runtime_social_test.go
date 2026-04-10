package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"qorvexus/internal/agent"
	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/model"
	"qorvexus/internal/session"
	"qorvexus/internal/social"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
)

func TestHandleEnvelopeSendsFailureNoticeAfterPrefaceError(t *testing.T) {
	tempDir := t.TempDir()
	autoSend := true
	cfg := &config.Config{
		DataDir: tempDir,
		Agent: config.AgentConfig{
			DefaultModel: "primary",
			MaxTurns:     3,
		},
		Social: config.SocialConfig{
			AutoSendExternalReplies: &autoSend,
		},
	}
	registry := model.NewRegistry()
	registry.Register("primary", config.ModelConfig{Model: "stub"}, &failingAfterPrefaceClient{})
	connectors := social.NewRegistry()
	conn := &capturingConnector{}
	connectors.Register(conn)
	tools := tool.NewRegistry()
	tools.Register(echoTool{})
	app := &appRuntime{
		cfg:         cfg,
		runner:      &agent.Runner{Config: cfg, Models: registry, Sessions: session.NewStore(tempDir), Tools: tools, Compressor: &contextx.Compressor{MaxChars: 1_000_000, Threshold: 0.9}},
		connectors:  connectors,
		socialGraph: social.NewGraphStore(filepath.Join(tempDir, "graph.json")),
		socialTurns: map[string]*socialTurnState{},
	}

	_, err := app.HandleEnvelope(context.Background(), social.Envelope{
		Channel:    "telegram",
		ThreadID:   "thread-1",
		SenderID:   "user-1",
		SenderName: "Ada",
		Text:       "hello",
		Context: types.ConversationContext{
			Channel:      "telegram",
			ThreadID:     "thread-1",
			SenderID:     "user-1",
			SenderName:   "Ada",
			Trust:        types.TrustExternal,
			ReplyAsAgent: true,
		},
	})
	if err == nil {
		t.Fatal("expected handle envelope to return error")
	}
	if len(conn.messages) != 2 {
		t.Fatalf("expected preface and failure notice, got %#v", conn.messages)
	}
	if conn.messages[0].Text != "Checking now." {
		t.Fatalf("unexpected preface message: %#v", conn.messages[0])
	}
	if conn.messages[1].Text == "" || !containsString(conn.messages[1].Text, "I ran into an error while continuing that task") {
		t.Fatalf("unexpected failure notice: %#v", conn.messages[1])
	}
}

type failingAfterPrefaceClient struct {
	calls int
}

func (c *failingAfterPrefaceClient) Complete(_ context.Context, _ model.CompletionRequest) (*model.CompletionResponse, error) {
	if c.calls == 0 {
		c.calls++
		return &model.CompletionResponse{
			Message: types.Message{
				Role:    types.RoleAssistant,
				Content: "Checking now.",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "echo", Arguments: `{"text":"demo"}`},
				},
			},
		}, nil
	}
	return nil, errors.New("max turns exceeded")
}

type capturingConnector struct {
	messages []social.OutboundMessage
}

func (c *capturingConnector) Name() string { return "telegram" }

func (c *capturingConnector) Send(_ context.Context, msg social.OutboundMessage) (string, error) {
	c.messages = append(c.messages, msg)
	return "ok", nil
}

type echoTool struct{}

func (echoTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name: "echo",
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

func containsString(value string, needle string) bool {
	return strings.Contains(value, needle)
}
