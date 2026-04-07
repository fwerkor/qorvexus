package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"qorvexus/internal/memory"
	"qorvexus/internal/plan"
	"qorvexus/internal/types"
)

type ThinkTool struct{}

type PlanStepUpdateInput struct {
	PlanID        string
	StepID        string
	Status        string
	Title         string
	Details       string
	Prompt        string
	Model         string
	ExecutionMode string
	DependsOn     []string
	Note          string
	Result        string
	Error         string
}

type Runtime interface {
	RunSubAgent(ctx context.Context, name string, prompt string, model string) (string, error)
	ConsultModels(ctx context.Context, prompt string, panel []string) (string, error)
	AddScheduledTask(ctx context.Context, name string, schedule string, prompt string, model string) (string, error)
	CreatePlan(ctx context.Context, plan plan.Plan) (string, error)
	GetPlan(ctx context.Context, planID string) (string, error)
	ListPlans(ctx context.Context, limit int, status string) (string, error)
	UpdatePlanStep(ctx context.Context, input PlanStepUpdateInput) (string, error)
	ExecutePlanStep(ctx context.Context, planID string, stepID string, mode string) (string, error)
	AdvancePlan(ctx context.Context, planID string, limit int) (string, error)
	Remember(ctx context.Context, entry memory.Entry) (string, error)
	Recall(ctx context.Context, query string, limit int) (string, error)
	EnqueueTask(ctx context.Context, name string, prompt string, model string, sessionID string) (string, error)
	SendSocialMessage(ctx context.Context, channel string, threadID string, recipient string, text string) (string, error)
	ListSocialConnectors(ctx context.Context) (string, error)
	ReadRuntimeConfig(ctx context.Context) (string, error)
	WriteRuntimeConfig(ctx context.Context, raw string) (string, error)
	UpsertSkill(ctx context.Context, name string, description string, body string) (string, error)
	AddSelfImprovement(ctx context.Context, title string, description string, kind string) (string, error)
	ListSelfImprovements(ctx context.Context, limit int) (string, error)
	PromoteSelfImprovement(ctx context.Context, title string, description string, model string) (string, error)
	MineSelfImprovements(ctx context.Context, limit int) (string, error)
	CaptureSelfImprovement(ctx context.Context, title string, description string, kind string, promote bool, model string) (string, error)
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

type CreatePlanTool struct {
	rt Runtime
}

func NewCreatePlanTool(rt Runtime) *CreatePlanTool { return &CreatePlanTool{rt: rt} }

func (t *CreatePlanTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "create_plan",
		Description: "Create a durable multi-step execution plan with dependencies that can be resumed and advanced later.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal":       map[string]any{"type": "string"},
				"summary":    map[string]any{"type": "string"},
				"session_id": map[string]any{"type": "string"},
				"steps": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":             map[string]any{"type": "string"},
							"title":          map[string]any{"type": "string"},
							"details":        map[string]any{"type": "string"},
							"prompt":         map[string]any{"type": "string"},
							"model":          map[string]any{"type": "string"},
							"depends_on":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"execution_mode": map[string]any{"type": "string", "enum": []string{"subagent", "queued"}},
						},
						"required": []string{"title"},
					},
				},
			},
			"required": []string{"goal", "steps"},
		},
	}
}

func (t *CreatePlanTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Goal      string `json:"goal"`
		Summary   string `json:"summary"`
		SessionID string `json:"session_id"`
		Steps     []struct {
			ID            string   `json:"id"`
			Title         string   `json:"title"`
			Details       string   `json:"details"`
			Prompt        string   `json:"prompt"`
			Model         string   `json:"model"`
			DependsOn     []string `json:"depends_on"`
			ExecutionMode string   `json:"execution_mode"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	steps := make([]plan.Step, 0, len(input.Steps))
	for _, item := range input.Steps {
		steps = append(steps, plan.Step{
			ID:            item.ID,
			Title:         item.Title,
			Details:       item.Details,
			Prompt:        item.Prompt,
			Model:         item.Model,
			DependsOn:     item.DependsOn,
			ExecutionMode: plan.ExecutionMode(item.ExecutionMode),
		})
	}
	return t.rt.CreatePlan(ctx, plan.Plan{
		Goal:      input.Goal,
		Summary:   input.Summary,
		SessionID: input.SessionID,
		Steps:     steps,
	})
}

type GetPlanTool struct {
	rt Runtime
}

func NewGetPlanTool(rt Runtime) *GetPlanTool { return &GetPlanTool{rt: rt} }

func (t *GetPlanTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "get_plan",
		Description: "Inspect a saved execution plan and its current step state.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string"},
			},
			"required": []string{"plan_id"},
		},
	}
}

func (t *GetPlanTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.GetPlan(ctx, input.PlanID)
}

type ListPlansTool struct {
	rt Runtime
}

func NewListPlansTool(rt Runtime) *ListPlansTool { return &ListPlansTool{rt: rt} }

func (t *ListPlansTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "list_plans",
		Description: "List durable execution plans, optionally filtered by overall plan status.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit":  map[string]any{"type": "integer"},
				"status": map[string]any{"type": "string"},
			},
		},
	}
}

func (t *ListPlansTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Limit  int    `json:"limit"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.ListPlans(ctx, input.Limit, input.Status)
}

type UpdatePlanStepTool struct {
	rt Runtime
}

func NewUpdatePlanStepTool(rt Runtime) *UpdatePlanStepTool { return &UpdatePlanStepTool{rt: rt} }

func (t *UpdatePlanStepTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "update_plan_step",
		Description: "Revise a plan step, record a note, or update its status and result.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id":        map[string]any{"type": "string"},
				"step_id":        map[string]any{"type": "string"},
				"status":         map[string]any{"type": "string"},
				"title":          map[string]any{"type": "string"},
				"details":        map[string]any{"type": "string"},
				"prompt":         map[string]any{"type": "string"},
				"model":          map[string]any{"type": "string"},
				"execution_mode": map[string]any{"type": "string", "enum": []string{"subagent", "queued"}},
				"depends_on":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"note":           map[string]any{"type": "string"},
				"result":         map[string]any{"type": "string"},
				"error":          map[string]any{"type": "string"},
			},
			"required": []string{"plan_id", "step_id"},
		},
	}
}

func (t *UpdatePlanStepTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		PlanID        string   `json:"plan_id"`
		StepID        string   `json:"step_id"`
		Status        string   `json:"status"`
		Title         string   `json:"title"`
		Details       string   `json:"details"`
		Prompt        string   `json:"prompt"`
		Model         string   `json:"model"`
		ExecutionMode string   `json:"execution_mode"`
		DependsOn     []string `json:"depends_on"`
		Note          string   `json:"note"`
		Result        string   `json:"result"`
		Error         string   `json:"error"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.UpdatePlanStep(ctx, PlanStepUpdateInput{
		PlanID:        input.PlanID,
		StepID:        input.StepID,
		Status:        input.Status,
		Title:         input.Title,
		Details:       input.Details,
		Prompt:        input.Prompt,
		Model:         input.Model,
		ExecutionMode: input.ExecutionMode,
		DependsOn:     input.DependsOn,
		Note:          input.Note,
		Result:        input.Result,
		Error:         input.Error,
	})
}

type ExecutePlanStepTool struct {
	rt Runtime
}

func NewExecutePlanStepTool(rt Runtime) *ExecutePlanStepTool { return &ExecutePlanStepTool{rt: rt} }

func (t *ExecutePlanStepTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "execute_plan_step",
		Description: "Execute one specific step from a saved plan via a subagent or background queue.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string"},
				"step_id": map[string]any{"type": "string"},
				"mode":    map[string]any{"type": "string", "enum": []string{"subagent", "queued"}},
			},
			"required": []string{"plan_id", "step_id"},
		},
	}
}

func (t *ExecutePlanStepTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		PlanID string `json:"plan_id"`
		StepID string `json:"step_id"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.ExecutePlanStep(ctx, input.PlanID, input.StepID, input.Mode)
}

type AdvancePlanTool struct {
	rt Runtime
}

func NewAdvancePlanTool(rt Runtime) *AdvancePlanTool { return &AdvancePlanTool{rt: rt} }

func (t *AdvancePlanTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "advance_plan",
		Description: "Advance a saved plan by executing or queueing runnable steps in dependency order.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan_id": map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer"},
			},
			"required": []string{"plan_id"},
		},
	}
}

func (t *AdvancePlanTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		PlanID string `json:"plan_id"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.AdvancePlan(ctx, input.PlanID, input.Limit)
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
				"key":     map[string]any{"type": "string"},
				"area":    map[string]any{"type": "string"},
				"kind":    map[string]any{"type": "string"},
				"subject": map[string]any{"type": "string"},
				"summary": map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
				"source":  map[string]any{"type": "string"},
				"importance": map[string]any{
					"type": "integer",
				},
				"confidence": map[string]any{
					"type": "number",
				},
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
		Key        string   `json:"key"`
		Area       string   `json:"area"`
		Kind       string   `json:"kind"`
		Subject    string   `json:"subject"`
		Summary    string   `json:"summary"`
		Content    string   `json:"content"`
		Source     string   `json:"source"`
		Tags       []string `json:"tags"`
		Importance int      `json:"importance"`
		Confidence float64  `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.Remember(ctx, memory.Entry{
		Key:        input.Key,
		Area:       input.Area,
		Kind:       input.Kind,
		Subject:    input.Subject,
		Summary:    input.Summary,
		Content:    input.Content,
		Source:     input.Source,
		Tags:       input.Tags,
		Importance: input.Importance,
		Confidence: input.Confidence,
	})
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

type SocialListTool struct{ rt Runtime }

func NewSocialListTool(rt Runtime) *SocialListTool { return &SocialListTool{rt: rt} }

func (t *SocialListTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "list_social_connectors",
		Description: "List configured social connectors/channels currently available for outbound communication.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *SocialListTool) Invoke(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.rt.ListSocialConnectors(ctx)
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

type PromoteSelfImprovementTool struct{ rt Runtime }

func NewPromoteSelfImprovementTool(rt Runtime) *PromoteSelfImprovementTool {
	return &PromoteSelfImprovementTool{rt: rt}
}

func (t *PromoteSelfImprovementTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "promote_self_improvement",
		Description: "Turn a self-improvement idea into an asynchronous execution task so the agent can work on it later.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"model":       map[string]any{"type": "string"},
			},
			"required": []string{"title", "description"},
		},
	}
}

func (t *PromoteSelfImprovementTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.PromoteSelfImprovement(ctx, input.Title, input.Description, input.Model)
}

type MineSelfImprovementsTool struct{ rt Runtime }

func NewMineSelfImprovementsTool(rt Runtime) *MineSelfImprovementsTool {
	return &MineSelfImprovementsTool{rt: rt}
}

func (t *MineSelfImprovementsTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "mine_self_improvements",
		Description: "Generate self-improvement candidates from recent audit history and failures.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{"type": "integer"},
			},
		},
	}
}

func (t *MineSelfImprovementsTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Limit int `json:"limit"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.MineSelfImprovements(ctx, input.Limit)
}

type CaptureSelfImprovementTool struct{ rt Runtime }

func NewCaptureSelfImprovementTool(rt Runtime) *CaptureSelfImprovementTool {
	return &CaptureSelfImprovementTool{rt: rt}
}

func (t *CaptureSelfImprovementTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "capture_self_improvement",
		Description: "Capture a mined improvement into the backlog and optionally promote it into an execution task.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"kind":        map[string]any{"type": "string"},
				"promote":     map[string]any{"type": "boolean"},
				"model":       map[string]any{"type": "string"},
			},
			"required": []string{"title", "description"},
		},
	}
}

func (t *CaptureSelfImprovementTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		Promote     bool   `json:"promote"`
		Model       string `json:"model"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.CaptureSelfImprovement(ctx, input.Title, input.Description, input.Kind, input.Promote, input.Model)
}
