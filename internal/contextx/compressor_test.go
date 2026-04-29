package contextx

import (
	"context"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/model"
	"qorvexus/internal/types"
)

type summaryClient struct{}

func (summaryClient) Complete(context.Context, model.CompletionRequest) (*model.CompletionResponse, error) {
	return &model.CompletionResponse{
		Message: types.Message{Role: types.RoleAssistant, Content: "Earlier work was summarized."},
	}, nil
}

func TestMaybeCompressPreservesSystemPrompt(t *testing.T) {
	registry := model.NewRegistry()
	registry.Register("primary", config.ModelConfig{Model: "stub"}, summaryClient{})
	compressor := &Compressor{
		Registry:  registry,
		MaxChars:  10,
		Threshold: 0.5,
	}

	messages := []types.Message{
		{Role: types.RoleSystem, Content: "Base identity and safety rules."},
		{Role: types.RoleUser, Content: "one long message"},
		{Role: types.RoleAssistant, Content: "two long message"},
		{Role: types.RoleUser, Content: "three long message"},
		{Role: types.RoleAssistant, Content: "four long message"},
		{Role: types.RoleUser, Content: "five long message"},
		{Role: types.RoleAssistant, Content: "six long message"},
	}

	out, err := compressor.MaybeCompress(context.Background(), "primary", messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || out[0].Role != types.RoleSystem {
		t.Fatalf("expected preserved system prompt first, got %#v", out)
	}
	if !strings.Contains(out[0].Content, "Base identity and safety rules.") {
		t.Fatalf("expected original system prompt to be preserved, got %q", out[0].Content)
	}
	if len(out) < 2 || out[1].Role == types.RoleSystem || !strings.Contains(out[1].Content, "Compressed conversation summary:") {
		t.Fatalf("expected compressed summary as non-system context after system prompt, got %#v", out)
	}
	for i, msg := range out[1:] {
		if msg.Role == types.RoleSystem {
			t.Fatalf("expected no system message after the first, found one at %d in %#v", i+1, out)
		}
	}
}

func TestMaybeCompressUsesSizeWithoutTurnCountGate(t *testing.T) {
	registry := model.NewRegistry()
	registry.Register("primary", config.ModelConfig{Model: "stub"}, summaryClient{})
	compressor := &Compressor{
		Registry:  registry,
		MaxChars:  10,
		Threshold: 0.5,
	}
	messages := []types.Message{
		{Role: types.RoleSystem, Content: "Base rules."},
		{Role: types.RoleUser, Content: strings.Repeat("a", 20)},
		{Role: types.RoleAssistant, Content: strings.Repeat("b", 20)},
	}

	out, err := compressor.MaybeCompress(context.Background(), "primary", messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2 || !strings.Contains(out[1].Content, "Compressed conversation summary:") {
		t.Fatalf("expected compressed summary, got %#v", out)
	}
	for _, msg := range out {
		if msg.Content == strings.Repeat("a", 20) {
			t.Fatalf("expected oldest oversized message to be summarized away, got %#v", out)
		}
	}
}
