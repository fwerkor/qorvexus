package model

import (
	"context"
	"strings"
	"sync"

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
	Raw     string
}

type EmbeddingRequest struct {
	Model  string
	Inputs []string
}

type EmbeddingResponse struct {
	Model   string
	Vectors [][]float64
	Usage   map[string]int
	Raw     string
}

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

type EmbeddingClient interface {
	Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error)
}

type QueuedClient struct {
	inner Client
	mu    *sync.Mutex
}

func NewQueuedClient(inner Client, mu *sync.Mutex) Client {
	if inner == nil {
		return nil
	}
	if mu == nil {
		mu = &sync.Mutex{}
	}
	return &QueuedClient{inner: inner, mu: mu}
}

func (c *QueuedClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.Complete(ctx, req)
}

func (c *QueuedClient) Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	embedder, ok := c.inner.(EmbeddingClient)
	if !ok {
		return nil, nil
	}
	return embedder.Embed(ctx, req)
}

type AliasMappedClient struct {
	inner Client
	cfg   config.ModelConfig
}

func NewAliasMappedClient(cfg config.ModelConfig, inner Client) Client {
	if inner == nil {
		return nil
	}
	return &AliasMappedClient{inner: inner, cfg: cfg}
}

func (c *AliasMappedClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	req.Model = providerModel(c.cfg, req.Model)
	return c.inner.Complete(ctx, req)
}

func (c *AliasMappedClient) Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	embedder, ok := c.inner.(EmbeddingClient)
	if !ok {
		return nil, nil
	}
	req.Model = providerModel(c.cfg, req.Model)
	return embedder.Embed(ctx, req)
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

func providerModel(cfg config.ModelConfig, requested string) string {
	configured := strings.TrimSpace(cfg.Model)
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return configured
	}
	if configured == "" {
		return requested
	}
	if strings.EqualFold(requested, strings.TrimSpace(cfg.RuntimeName)) {
		return configured
	}
	if strings.EqualFold(requested, "primary") && !strings.EqualFold(configured, "primary") {
		return configured
	}
	return requested
}
