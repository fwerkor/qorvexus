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

type countingSummaryClient struct {
	calls int
}

func (c *countingSummaryClient) Complete(context.Context, model.CompletionRequest) (*model.CompletionResponse, error) {
	c.calls++
	return &model.CompletionResponse{
		Message: types.Message{Role: types.RoleAssistant, Content: "New summary."},
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

func TestMaybeCompressSkipsRecentSummaryUntilWindowReallyExceeded(t *testing.T) {
	registry := model.NewRegistry()
	client := &countingSummaryClient{}
	registry.Register("primary", config.ModelConfig{Model: "stub"}, client)
	compressor := &Compressor{
		Registry:  registry,
		MaxChars:  200,
		Threshold: 0.5,
	}
	messages := []types.Message{
		{Role: types.RoleSystem, Content: "Base rules."},
		{Role: types.RoleUser, Content: "Compressed conversation summary:\nEarlier work."},
		{Role: types.RoleUser, Content: strings.Repeat("a", 95)},
		{Role: types.RoleAssistant, Content: "ok"},
	}

	out, err := compressor.MaybeCompress(context.Background(), "primary", messages)
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 0 {
		t.Fatalf("expected recent summary guard to skip compression, got %d calls", client.calls)
	}
	if len(out) != len(messages) {
		t.Fatalf("expected messages unchanged, got %#v", out)
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
		{Role: types.RoleUser, Content: "latest raw request"},
		{Role: types.RoleAssistant, Content: strings.Repeat("c", 20)},
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
	foundLatest := false
	for _, msg := range out {
		if msg.Content == "latest raw request" {
			foundLatest = true
		}
	}
	if !foundLatest {
		t.Fatalf("expected latest user message to remain raw, got %#v", out)
	}
}

func TestMaybeCompressPreservesLatestOriginalUserMessage(t *testing.T) {
	registry := model.NewRegistry()
	registry.Register("primary", config.ModelConfig{Model: "stub"}, summaryClient{})
	compressor := &Compressor{
		Registry:  registry,
		MaxChars:  10,
		Threshold: 0.5,
	}
	messages := []types.Message{
		{Role: types.RoleSystem, Content: "Base rules."},
		{Role: types.RoleUser, Content: "请把论坛登录保持住，不要每次重开浏览器。"},
		{Role: types.RoleAssistant, Content: "我会处理。"},
		{Role: types.RoleUser, Content: "压缩后记得保留用户原始输入。"},
		{Role: types.RoleAssistant, Content: "收到。"},
	}

	out, err := compressor.MaybeCompress(context.Background(), "primary", messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2 {
		t.Fatalf("expected compressed output, got %#v", out)
	}
	summary := out[1].Content
	for _, needle := range []string{
		"Latest original user message:",
		"压缩后记得保留用户原始输入。",
	} {
		if !strings.Contains(summary, needle) {
			t.Fatalf("expected compressed summary to preserve %q, got %q", needle, summary)
		}
	}
	for _, msg := range out {
		if msg.Role == types.RoleUser && msg.Content == "压缩后记得保留用户原始输入。" {
			return
		}
	}
	t.Fatalf("expected latest user message to remain as its own raw message, got %#v", out)
}
