package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"qorvexus/internal/types"
)

type ThinkTool struct{}

type Runtime interface {
	RunSubAgent(ctx context.Context, name string, prompt string, model string) (string, error)
	ConsultModels(ctx context.Context, prompt string, panel []string) (string, error)
	AddScheduledTask(ctx context.Context, name string, schedule string, prompt string, model string) (string, error)
	Remember(ctx context.Context, content string, tags []string, source string) (string, error)
	Recall(ctx context.Context, query string, limit int) (string, error)
	EnqueueTask(ctx context.Context, name string, prompt string, model string, sessionID string) (string, error)
	SendSocialMessage(ctx context.Context, channel string, threadID string, recipient string, text string) (string, error)
	ReadRuntimeConfig(ctx context.Context) (string, error)
	WriteRuntimeConfig(ctx context.Context, raw string) (string, error)
	UpsertSkill(ctx context.Context, name string, description string, body string) (string, error)
	AddSelfImprovement(ctx context.Context, title string, description string, kind string) (string, error)
	ListSelfImprovements(ctx context.Context, limit int) (string, error)
}

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

type RememberTool struct {
	rt Runtime
}

func NewRememberTool(rt Runtime) *RememberTool { return &RememberTool{rt: rt} }

func (t *RememberTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "remember",
		Description: "Store a durable memory for later retrieval.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string"},
				"source":  map[string]any{"type": "string"},
				"tags": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"required": []string{"content"},
		},
	}
}

func (t *RememberTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Content string   `json:"content"`
		Source  string   `json:"source"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.Remember(ctx, input.Content, input.Tags, input.Source)
}

type RecallTool struct {
	rt Runtime
}

func NewRecallTool(rt Runtime) *RecallTool { return &RecallTool{rt: rt} }

func (t *RecallTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "recall",
		Description: "Search durable memory for facts, preferences, and prior work.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			},
			"required": []string{"query"},
		},
	}
}

func (t *RecallTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.Recall(ctx, input.Query, input.Limit)
}

type EnqueueTaskTool struct {
	rt Runtime
}

func NewEnqueueTaskTool(rt Runtime) *EnqueueTaskTool { return &EnqueueTaskTool{rt: rt} }

func (t *EnqueueTaskTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "enqueue_task",
		Description: "Queue a background task for asynchronous execution.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":       map[string]any{"type": "string"},
				"prompt":     map[string]any{"type": "string"},
				"model":      map[string]any{"type": "string"},
				"session_id": map[string]any{"type": "string"},
			},
			"required": []string{"name", "prompt"},
		},
	}
}

func (t *EnqueueTaskTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Name      string `json:"name"`
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.EnqueueTask(ctx, input.Name, input.Prompt, input.Model, input.SessionID)
}

type SocialSendTool struct {
	rt Runtime
}

func NewSocialSendTool(rt Runtime) *SocialSendTool { return &SocialSendTool{rt: rt} }

func (t *SocialSendTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "send_social_message",
		Description: "Send or queue a message through a configured social connector. Useful when acting as the owner's digital representative.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel":   map[string]any{"type": "string"},
				"thread_id": map[string]any{"type": "string"},
				"recipient": map[string]any{"type": "string"},
				"text":      map[string]any{"type": "string"},
			},
			"required": []string{"channel", "text"},
		},
	}
}

func (t *SocialSendTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Channel   string `json:"channel"`
		ThreadID  string `json:"thread_id"`
		Recipient string `json:"recipient"`
		Text      string `json:"text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.SendSocialMessage(ctx, input.Channel, input.ThreadID, input.Recipient, input.Text)
}

type ReadConfigTool struct{ rt Runtime }

func NewReadConfigTool(rt Runtime) *ReadConfigTool { return &ReadConfigTool{rt: rt} }

func (t *ReadConfigTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "read_runtime_config",
		Description: "Read the current runtime configuration file for self-inspection and planning.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ReadConfigTool) Invoke(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.rt.ReadRuntimeConfig(ctx)
}

type WriteConfigTool struct{ rt Runtime }

func NewWriteConfigTool(rt Runtime) *WriteConfigTool { return &WriteConfigTool{rt: rt} }

func (t *WriteConfigTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "write_runtime_config",
		Description: "Write a full updated runtime configuration. Use carefully and only after deliberate reasoning.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"config": map[string]any{"type": "string"},
			},
			"required": []string{"config"},
		},
	}
}

func (t *WriteConfigTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.WriteRuntimeConfig(ctx, input.Config)
}

type UpsertSkillTool struct{ rt Runtime }

func NewUpsertSkillTool(rt Runtime) *UpsertSkillTool { return &UpsertSkillTool{rt: rt} }

func (t *UpsertSkillTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "upsert_skill",
		Description: "Create or update a SKILL.md file so the agent can extend its own behavior.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"body":        map[string]any{"type": "string"},
			},
			"required": []string{"name", "description", "body"},
		},
	}
}

func (t *UpsertSkillTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.UpsertSkill(ctx, input.Name, input.Description, input.Body)
}

type SelfBacklogAddTool struct{ rt Runtime }

func NewSelfBacklogAddTool(rt Runtime) *SelfBacklogAddTool { return &SelfBacklogAddTool{rt: rt} }

func (t *SelfBacklogAddTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "add_self_improvement",
		Description: "Record a self-improvement item for the agent to work on later.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"kind":        map[string]any{"type": "string"},
			},
			"required": []string{"title", "description"},
		},
	}
}

func (t *SelfBacklogAddTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.AddSelfImprovement(ctx, input.Title, input.Description, input.Kind)
}

type SelfBacklogListTool struct{ rt Runtime }

func NewSelfBacklogListTool(rt Runtime) *SelfBacklogListTool { return &SelfBacklogListTool{rt: rt} }

func (t *SelfBacklogListTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "list_self_improvements",
		Description: "List queued self-improvement items and recent ideas for evolving the agent.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{"type": "integer"},
			},
		},
	}
}

func (t *SelfBacklogListTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Limit int `json:"limit"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.ListSelfImprovements(ctx, input.Limit)
}
