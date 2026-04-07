package model

import (
	"context"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

type CompletionRequest struct {
	Model       string
	Messages    []types.Message
	Tools       []types.ToolDefinition
	MaxTokens   int
	Temperature float64
}

type CompletionResponse struct {
	Message types.Message
	Usage   map[string]int
}

type EmbeddingRequest struct {
	Model  string
	Inputs []string
}

type EmbeddingResponse struct {
	Model   string
	Vectors [][]float64
	Usage   map[string]int
}

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

type EmbeddingClient interface {
	Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error)
}

type Registry struct {
	clients map[string]Client
	configs map[string]config.ModelConfig
}

func NewRegistry() *Registry {
	return &Registry{
		clients: map[string]Client{},
		configs: map[string]config.ModelConfig{},
	}
}

func (r *Registry) Register(name string, cfg config.ModelConfig, client Client) {
	r.clients[name] = client
	r.configs[name] = cfg
}

func (r *Registry) Get(name string) (Client, config.ModelConfig, bool) {
	client, ok := r.clients[name]
	if !ok {
		return nil, config.ModelConfig{}, false
	}
	return client, r.configs[name], true
}

func (r *Registry) Embed(ctx context.Context, name string, inputs []string) (*EmbeddingResponse, bool, error) {
	client, cfg, ok := r.Get(name)
	if !ok {
		return nil, false, nil
	}
	embedder, ok := client.(EmbeddingClient)
	if !ok {
		return nil, false, nil
	}
	resp, err := embedder.Embed(ctx, EmbeddingRequest{
		Model:  cfg.Model,
		Inputs: inputs,
	})
	if resp != nil && resp.Model == "" {
		resp.Model = cfg.Model
	}
	return resp, true, err
}
