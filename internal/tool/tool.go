package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"qorvexus/internal/types"
)

type Tool interface {
	Definition() types.ToolDefinition
	Invoke(ctx context.Context, raw json.RawMessage) (string, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Definition().Name] = tool
}

func (r *Registry) Definitions() []types.ToolDefinition {
	out := make([]types.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		out = append(out, tool.Definition())
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, call types.ToolCall) types.ToolResult {
	tool, ok := r.tools[call.Name]
	if !ok {
		return types.ToolResult{Name: call.Name, CallID: call.ID, Error: true, Content: fmt.Sprintf("unknown tool %q", call.Name)}
	}
	out, err := tool.Invoke(ctx, json.RawMessage(call.Arguments))
	if err != nil {
		return types.ToolResult{Name: call.Name, CallID: call.ID, Error: true, Content: err.Error()}
	}
	return types.ToolResult{Name: call.Name, CallID: call.ID, Content: out}
}
