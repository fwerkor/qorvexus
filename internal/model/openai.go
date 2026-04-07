package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

type OpenAIClient struct {
	cfg        config.ModelConfig
	httpClient *http.Client
}

func NewOpenAIClient(cfg config.ModelConfig) *OpenAIClient {
	return &OpenAIClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string            `json:"type"`
	Function openAIFunctionDef `json:"function"`
}

type openAIFunctionDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type openAIToolCall struct {
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function openAIFunctionCallRef `json:"function"`
}

type openAIFunctionCallRef struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIResponseMessage `json:"message"`
	} `json:"choices"`
	Usage map[string]int `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type openAIResponseMessage struct {
	Role      string           `json:"role"`
	Content   any              `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string         `json:"model"`
	Usage map[string]int `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenAIClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	payload := openAIRequest{
		Model:       c.pick(req.Model, c.cfg.Model),
		Messages:    make([]openAIMessage, 0, len(req.Messages)),
		MaxTokens:   c.pickInt(req.MaxTokens, c.cfg.MaxTokens),
		Temperature: c.pickFloat(req.Temperature, c.cfg.Temperature),
	}
	for _, msg := range req.Messages {
		payload.Messages = append(payload.Messages, mapMessage(msg))
	}
	for _, tool := range req.Tools {
		payload.Tools = append(payload.Tools, openAITool{
			Type: "function",
			Function: openAIFunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	key := strings.TrimSpace(c.cfg.APIKey)
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	parsed := &openAIResponse{}
	if err := json.Unmarshal(raw, parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("model error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("empty model response")
	}

	msg := parsed.Choices[0].Message
	return &CompletionResponse{
		Message: fromOpenAIMessage(msg),
		Usage:   parsed.Usage,
	}, nil
}

func (c *OpenAIClient) Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	inputs := make([]string, 0, len(req.Inputs))
	for _, item := range req.Inputs {
		if strings.TrimSpace(item) != "" {
			inputs = append(inputs, item)
		}
	}
	if len(inputs) == 0 {
		return &EmbeddingResponse{}, nil
	}
	payload := openAIEmbeddingRequest{
		Model: c.pick(req.Model, c.cfg.Model),
		Input: inputs,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/embeddings"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	key := strings.TrimSpace(c.cfg.APIKey)
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send embedding request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding model returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	parsed := &openAIEmbeddingResponse{}
	if err := json.Unmarshal(raw, parsed); err != nil {
		return nil, fmt.Errorf("parse embedding response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("embedding model error: %s", parsed.Error.Message)
	}
	out := &EmbeddingResponse{
		Model: parsed.Model,
		Usage: parsed.Usage,
	}
	for _, item := range parsed.Data {
		out.Vectors = append(out.Vectors, item.Embedding)
	}
	return out, nil
}

func mapMessage(msg types.Message) openAIMessage {
	out := openAIMessage{
		Role:       string(msg.Role),
		Name:       msg.Name,
		ToolCallID: msg.ToolCallID,
	}
	if len(msg.Parts) > 0 {
		parts := make([]map[string]any, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			switch part.Type {
			case "image_url":
				parts = append(parts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": part.ImageURL,
					},
				})
			default:
				parts = append(parts, map[string]any{
					"type": "text",
					"text": part.Text,
				})
			}
		}
		out.Content = parts
	} else {
		out.Content = msg.Content
	}
	for _, call := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, openAIToolCall{
			ID:   call.ID,
			Type: "function",
			Function: openAIFunctionCallRef{
				Name:      call.Name,
				Arguments: call.Arguments,
			},
		})
	}
	return out
}

func fromOpenAIMessage(msg openAIResponseMessage) types.Message {
	out := types.Message{Role: types.Role(msg.Role)}
	switch v := msg.Content.(type) {
	case string:
		out.Content = v
	case []any:
		for _, item := range v {
			partMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := partMap["type"].(string)
			switch partType {
			case "text":
				out.Parts = append(out.Parts, types.ContentPart{Type: "text", Text: toString(partMap["text"])})
			}
		}
	}
	for _, call := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, types.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	return out
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (c *OpenAIClient) pick(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func (c *OpenAIClient) pickInt(primary, fallback int) int {
	if primary > 0 {
		return primary
	}
	return fallback
}

func (c *OpenAIClient) pickFloat(primary, fallback float64) float64 {
	if primary != 0 {
		return primary
	}
	return fallback
}
