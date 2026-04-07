package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"qorvexus/internal/types"
)

type ThinkTool struct{}

func (t *ThinkTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "think",
		Description: "Write down private reasoning notes or plans before acting.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"note": map[string]any{"type": "string"},
			},
			"required": []string{"note"},
		},
	}
}

func (t *ThinkTool) Invoke(_ context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return "thought recorded: " + strings.TrimSpace(input.Note), nil
}

type SubAgentTool struct {
	rt Runtime
}

func NewSubAgentTool(rt Runtime) *SubAgentTool { return &SubAgentTool{rt: rt} }

func (t *SubAgentTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "spawn_subagent",
		Description: "Delegate a focused task to a child agent and receive its result.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"prompt": map[string]any{"type": "string"},
				"model":  map[string]any{"type": "string"},
			},
			"required": []string{"name", "prompt"},
		},
	}
}

func (t *SubAgentTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.RunSubAgent(ctx, input.Name, input.Prompt, input.Model)
}

type DiscussTool struct {
	rt Runtime
}

func NewDiscussTool(rt Runtime) *DiscussTool { return &DiscussTool{rt: rt} }

func (t *DiscussTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "consult_models",
		Description: "Ask a panel of models to debate or offer alternative views, then return the combined result.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string"},
				"models": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"required": []string{"prompt", "models"},
		},
	}
}

func (t *DiscussTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Prompt string   `json:"prompt"`
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if len(input.Models) == 0 {
		return "", fmt.Errorf("models list cannot be empty")
	}
	return t.rt.ConsultModels(ctx, input.Prompt, input.Models)
}

type ScheduleTool struct {
	rt Runtime
}

func NewScheduleTool(rt Runtime) *ScheduleTool { return &ScheduleTool{rt: rt} }

func (t *ScheduleTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "schedule_task",
		Description: "Create a cron-style recurring task that runs the agent later.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":     map[string]any{"type": "string"},
				"schedule": map[string]any{"type": "string"},
				"prompt":   map[string]any{"type": "string"},
				"model":    map[string]any{"type": "string"},
			},
			"required": []string{"name", "schedule", "prompt"},
		},
	}
}

func (t *ScheduleTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.AddScheduledTask(ctx, input.Name, input.Schedule, input.Prompt, input.Model)
}
