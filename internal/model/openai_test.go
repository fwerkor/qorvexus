package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

func TestFromOpenAIMessageDropsReasoningParts(t *testing.T) {
	msg := fromOpenAIMessage(openAIResponseMessage{
		Role: "assistant",
		Content: []any{
			map[string]any{"type": "reasoning", "text": "hidden chain of thought"},
			map[string]any{"type": "text", "text": "Final answer."},
		},
	})
	if msg.Content != "Final answer." {
		t.Fatalf("expected only final answer content, got %+v", msg)
	}
	if len(msg.Parts) != 0 {
		t.Fatalf("expected assistant parts to be flattened away, got %+v", msg.Parts)
	}
}

func TestFromOpenAIMessageSanitizesThinkingTagsInStringContent(t *testing.T) {
	msg := fromOpenAIMessage(openAIResponseMessage{
		Role:    "assistant",
		Content: "<think>hidden</think>\nVisible answer.",
	})
	if msg.Content != "Visible answer." {
		t.Fatalf("expected thinking tags to be removed, got %+v", msg)
	}
}

func TestFromOpenAIMessageParsesFunctionCallContentParts(t *testing.T) {
	msg := fromOpenAIMessage(openAIResponseMessage{
		Role: "assistant",
		Content: []any{
			map[string]any{"type": "text", "text": "我来查一下。"},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_123",
				"name":      "list_sessions",
				"arguments": "{\"limit\":5}",
			},
		},
	})
	if msg.Content != "我来查一下。" {
		t.Fatalf("unexpected content: %+v", msg)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %+v", msg.ToolCalls)
	}
	if msg.ToolCalls[0].Name != "list_sessions" || msg.ToolCalls[0].Arguments != "{\"limit\":5}" {
		t.Fatalf("unexpected tool call: %+v", msg.ToolCalls[0])
	}
}

func TestFromOpenAIMessageParsesNestedFunctionToolCallParts(t *testing.T) {
	msg := fromOpenAIMessage(openAIResponseMessage{
		Role: "assistant",
		Content: []any{
			map[string]any{
				"type": "tool_call",
				"id":   "call_456",
				"function": map[string]any{
					"name":      "get_session",
					"arguments": "{\"session_id\":\"sess-1\"}",
				},
			},
		},
	})
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %+v", msg.ToolCalls)
	}
	if msg.ToolCalls[0].ID != "call_456" || msg.ToolCalls[0].Name != "get_session" {
		t.Fatalf("unexpected tool call: %+v", msg.ToolCalls[0])
	}
}

func TestCompleteAcceptsNestedUsageObjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [
				{
					"message": {
						"role": "assistant",
						"content": "你好"
					}
				}
			],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 2,
				"total_tokens": 12,
				"completion_tokens_details": {
					"reasoning_tokens": 0
				}
			}
		}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(config.ModelConfig{
		BaseURL: srv.URL,
		Model:   "demo",
	})
	resp, err := client.Complete(context.Background(), CompletionRequest{
		Model: "demo",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(resp.Message.Content) != "你好" {
		t.Fatalf("unexpected message content: %+v", resp.Message)
	}
	if !strings.Contains(resp.Raw, `"choices"`) {
		t.Fatalf("expected raw response body to be captured, got %q", resp.Raw)
	}
	if got := resp.Usage["prompt_tokens"]; got != 10 {
		t.Fatalf("expected prompt_tokens=10, got %d", got)
	}
	if got := resp.Usage["completion_tokens_details.reasoning_tokens"]; got != 0 {
		t.Fatalf("expected nested reasoning token count to flatten, got %d", got)
	}
}

func TestEmbedAcceptsNestedUsageObjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{"embedding":[0.1,0.2],"index":0}],
			"model":"embed-demo",
			"usage":{
				"prompt_tokens":3,
				"total_tokens":3,
				"prompt_tokens_details":{"cached_tokens":1}
			}
		}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(config.ModelConfig{
		BaseURL: srv.URL,
		Model:   "embed-demo",
	})
	resp, err := client.Embed(context.Background(), EmbeddingRequest{
		Model:  "embed-demo",
		Inputs: []string{"hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Vectors) != 1 || len(resp.Vectors[0]) != 2 {
		t.Fatalf("unexpected vectors: %#v", resp.Vectors)
	}
	if !strings.Contains(resp.Raw, `"embedding"`) {
		t.Fatalf("expected raw embedding response body to be captured, got %q", resp.Raw)
	}
	if got := resp.Usage["prompt_tokens_details.cached_tokens"]; got != 1 {
		t.Fatalf("expected cached token count to flatten, got %d", got)
	}
}
