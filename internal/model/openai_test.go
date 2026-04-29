package model

import (
	"context"
	"encoding/json"
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

func TestCompleteMapsRuntimeAliasToProviderModel(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(config.ModelConfig{
		RuntimeName: "primary",
		BaseURL:     srv.URL,
		Model:       "provider-model-id",
	})
	_, err := client.Complete(context.Background(), CompletionRequest{
		Model: "primary",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := payload["model"]; got != "provider-model-id" {
		t.Fatalf("expected provider model id, got %#v", got)
	}
}

func TestOpenAIToolFormatPreservesMessageShape(t *testing.T) {
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(config.ModelConfig{
		BaseURL:    srv.URL,
		Model:      "demo",
		ToolFormat: "openai",
	})
	_, err := client.Complete(context.Background(), CompletionRequest{
		Model: "demo",
		Messages: []types.Message{
			{Role: types.RoleSystem, Content: "system"},
			{Role: types.RoleUser, Content: "first"},
			{Role: types.RoleUser, Content: "second"},
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "demo_tool", Arguments: "{}"},
				},
			},
			{Role: types.RoleTool, Name: "demo_tool", ToolCallID: "call-1", Content: "done"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		t.Fatalf("expected payload messages, got %#v", payload["messages"])
	}
	if len(messages) != 5 {
		t.Fatalf("expected OpenAI mode to preserve all messages, got %#v", messages)
	}
	toolMessage, ok := messages[4].(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool message shape: %#v", messages[4])
	}
	if toolMessage["role"] != "tool" || toolMessage["tool_call_id"] != "call-1" {
		t.Fatalf("expected OpenAI mode to preserve tool message fields, got %#v", toolMessage)
	}
}

func TestCompleteRetriesWithLegacyFunctionsOnToolTemplateFailure(t *testing.T) {
	var payloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		if len(payloads) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"While executing CallExpression at line 85 in chat template"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
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
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "demo_tool", Arguments: `{"value":"one"}`},
				},
			},
			{
				Role:       types.RoleTool,
				Name:       "demo_tool",
				ToolCallID: "call-1",
				Content:    "tool output",
			},
		},
		Tools: []types.ToolDefinition{
			{
				Name:        "demo_tool",
				Description: "demo",
				Parameters:  map[string]any{"type": "object"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(resp.Message.Content) != "ok" {
		t.Fatalf("unexpected content: %q", resp.Message.Content)
	}
	if len(payloads) != 2 {
		t.Fatalf("expected retry with legacy functions, got %d requests", len(payloads))
	}
	if _, ok := payloads[0]["tools"]; !ok {
		t.Fatalf("expected first request to include tools")
	}
	if _, ok := payloads[1]["tools"]; ok {
		t.Fatalf("expected retry request to omit OpenAI tools, got %#v", payloads[1]["tools"])
	}
	if _, ok := payloads[1]["functions"]; !ok {
		t.Fatalf("expected retry request to include legacy functions")
	}
}

func TestCompleteRetriesWithoutToolsWhenLegacyFunctionsAlsoFail(t *testing.T) {
	var payloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		if len(payloads) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"While executing CallExpression at line 85 in chat template"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
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
		Tools: []types.ToolDefinition{
			{
				Name:        "demo_tool",
				Description: "demo",
				Parameters:  map[string]any{"type": "object"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(resp.Message.Content) != "ok" {
		t.Fatalf("unexpected content: %q", resp.Message.Content)
	}
	if len(payloads) != 3 {
		t.Fatalf("expected OpenAI tools, legacy functions, then no-tools retry, got %d requests", len(payloads))
	}
	if _, ok := payloads[2]["tools"]; ok {
		t.Fatalf("expected final retry request to omit tools, got %#v", payloads[2]["tools"])
	}
	if _, ok := payloads[2]["functions"]; ok {
		t.Fatalf("expected final retry request to omit functions, got %#v", payloads[2]["functions"])
	}
	finalMessages, ok := payloads[2]["messages"].([]any)
	if !ok {
		t.Fatalf("expected final payload messages, got %#v", payloads[2]["messages"])
	}
	for _, raw := range finalMessages {
		message, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("unexpected final message shape: %#v", raw)
		}
		if message["role"] == "tool" {
			t.Fatalf("expected final retry to convert tool-role messages to text-compatible messages: %#v", finalMessages)
		}
		if _, ok := message["tool_calls"]; ok {
			t.Fatalf("expected final retry to omit historical tool_calls: %#v", finalMessages)
		}
		if _, ok := message["tool_call_id"]; ok {
			t.Fatalf("expected final retry to omit historical tool_call_id: %#v", finalMessages)
		}
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
