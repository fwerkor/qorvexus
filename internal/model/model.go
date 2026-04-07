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

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
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
