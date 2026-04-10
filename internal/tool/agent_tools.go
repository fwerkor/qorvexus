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
	PlanID              string
	StepID              string
	Status              string
	Title               string
	Details             string
	Prompt              string
	Model               string
	ExecutionMode       string
	DependsOn           []string
	MaxAttempts         *int
	RetryBackoffSeconds *int
	ReviewRequired      *bool
	ReviewPrompt        string
	ReviewModel         string
	VerifyRequired      *bool
	VerifyPrompt        string
	VerifyModel         string
	FailureStrategy     string
	RollbackPrompt      string
	RollbackModel       string
	DegradePrompt       string
	DegradeModel        string
	Note                string
	Result              string
	Error               string
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
	ListSavedSessions(ctx context.Context, limit int, channel string, senderID string) (string, error)
	GetSessionView(ctx context.Context, sessionID string, limitMessages int, includeSystem bool, includeTool bool) (string, error)
	EnqueueTask(ctx context.Context, name string, prompt string, model string, sessionID string) (string, error)
	GrantOwnerIdentity(ctx context.Context, channel string, senderID string, senderName string) (string, error)
	SendSocialMessage(ctx context.Context, channel string, threadID string, recipient string, text string) (string, error)
	HoldSocialMessage(ctx context.Context, channel string, threadID string, recipient string, text string, reason string) (string, error)
	ListSocialOutbox(ctx context.Context, limit int, status string) (string, error)
	ManageSocialOutbox(ctx context.Context, outboxID string, action string, editedText string) (string, error)
	ListSocialConnectors(ctx context.Context) (string, error)
	SocialContactGraph(ctx context.Context) (string, error)
	ListSocialFollowUps(ctx context.Context, limit int, status string) (string, error)
	ReadRuntimeConfig(ctx context.Context) (string, error)
	WriteRuntimeConfig(ctx context.Context, raw string) (string, error)
	UpsertSkill(ctx context.Context, name string, description string, body string) (string, error)
	AddSelfImprovement(ctx context.Context, title string, description string, kind string) (string, error)
	ListSelfImprovements(ctx context.Context, limit int) (string, error)
	PromoteSelfImprovement(ctx context.Context, title string, description string, model string) (string, error)
	MineSelfImprovements(ctx context.Context, limit int) (string, error)
	CaptureSelfImprovement(ctx context.Context, title string, description string, kind string, promote bool, model string) (string, error)
	RequestRuntimeRestart(ctx context.Context, reason string) (string, error)
	ApplySelfUpdate(ctx context.Context, runTests bool, reason string) (string, error)
}

func (t *ThinkTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "think",
		Description: "Write down a short private reasoning note or plan fragment before acting. Use this sparingly for intermediate planning, not as a substitute for taking real tool actions.",
		Parameters: schemaObject(map[string]any{
			"note": schemaString("Private reasoning note to record."),
		}, "note"),
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
		Description: "Delegate a focused task to a child agent and receive its result. Use this when a subtask can be isolated cleanly. Give the child a concrete objective and enough context to finish without further hand-holding.",
		Parameters: schemaObject(map[string]any{
			"name":   schemaString("Short label for the delegated task."),
			"prompt": schemaString("Full task instructions for the child agent, including the expected deliverable."),
			"model":  schemaString("Optional model override for the child agent."),
		}, "name", "prompt"),
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
		Description: "Ask a panel of models to debate or offer alternative views, then return the combined result. Use this when the task benefits from contrasting approaches or extra scrutiny, not for routine single-path execution.",
		Parameters: schemaObject(map[string]any{
			"prompt": schemaString("Question or task for the model panel to consider."),
			"models": schemaArray("List of model names that should participate in the consultation.", schemaString("Registered model name.")),
		}, "prompt", "models"),
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
		Description: "Create a cron-style recurring task that runs the agent later. Use this for repeatable ongoing work rather than immediate execution.",
		Parameters: schemaObject(map[string]any{
			"name":     schemaString("Human-readable name for the recurring task."),
			"schedule": schemaString("Cron-style schedule expression."),
			"prompt":   schemaString("Task prompt that should run each time the schedule fires."),
			"model":    schemaString("Optional model override for scheduled runs."),
		}, "name", "schedule", "prompt"),
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
	stepsProperties := map[string]any{
		"id":                    schemaString("Optional stable step id. If omitted, one will be generated."),
		"title":                 schemaString("Short action-oriented step title."),
		"details":               schemaString("Optional extra step context or constraints."),
		"prompt":                schemaString("Specific instruction the executing agent should follow for this step."),
		"model":                 schemaString("Optional model override for this step."),
		"depends_on":            schemaArray("Step ids that must succeed before this step may run.", schemaString("Dependency step id.")),
		"execution_mode":        schemaStringEnum("Whether the step should run immediately in a subagent or be queued.", "subagent", "queued"),
		"max_attempts":          schemaInteger("Maximum attempts before the step is considered failed."),
		"retry_backoff_seconds": schemaInteger("Delay between retries in seconds."),
		"review_required":       schemaBoolean("Whether this step should go through a review stage after execution."),
		"review_prompt":         schemaString("Review-specific instructions describing what quality concerns to check."),
		"review_model":          schemaString("Optional model override for the review stage."),
		"verify_required":       schemaBoolean("Whether this step should go through a verification stage after execution."),
		"verify_prompt":         schemaString("Verification-specific instructions describing what evidence of success is required."),
		"verify_model":          schemaString("Optional model override for the verification stage."),
		"failure_strategy":      schemaStringEnum("How to recover if execution ultimately fails.", "fail", "rollback", "degrade", "rollback_then_degrade"),
		"rollback_prompt":       schemaString("Instructions for undoing or cleaning up the step when rollback is needed."),
		"rollback_model":        schemaString("Optional model override for rollback."),
		"degrade_prompt":        schemaString("Instructions for producing a safe reduced fallback when full execution fails."),
		"degrade_model":         schemaString("Optional model override for degraded fallback."),
	}
	return types.ToolDefinition{
		Name:        "create_plan",
		Description: "Create a durable multi-step execution plan with dependencies, retries, reviewer and verifier gates, and recovery strategies. Use this for multi-step work that should survive across turns or needs explicit state, not for one trivial action.",
		Parameters: schemaObject(map[string]any{
			"goal":                 schemaString("Overall objective the plan should accomplish."),
			"summary":              schemaString("Optional high-level summary or strategy for the plan."),
			"session_id":           schemaString("Optional session id to associate with the plan."),
			"max_parallel":         schemaInteger("Maximum number of runnable steps that may execute in parallel."),
			"default_max_attempts": schemaInteger("Default retry budget for steps that do not set max_attempts explicitly."),
			"auto_review":          schemaBoolean("Whether steps should default into a review stage after execution."),
			"auto_verify":          schemaBoolean("Whether steps should default into a verification stage after execution."),
			"review_model":         schemaString("Default model for review stages."),
			"verify_model":         schemaString("Default model for verification stages."),
			"steps":                schemaArray("Ordered plan steps. Each step should be concrete, executable, and scoped tightly enough to finish or fail clearly.", schemaObject(stepsProperties, "title")),
		}, "goal", "steps"),
	}
}

func (t *CreatePlanTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Goal               string `json:"goal"`
		Summary            string `json:"summary"`
		SessionID          string `json:"session_id"`
		MaxParallel        int    `json:"max_parallel"`
		DefaultMaxAttempts int    `json:"default_max_attempts"`
		AutoReview         bool   `json:"auto_review"`
		AutoVerify         bool   `json:"auto_verify"`
		ReviewModel        string `json:"review_model"`
		VerifyModel        string `json:"verify_model"`
		Steps              []struct {
			ID                  string   `json:"id"`
			Title               string   `json:"title"`
			Details             string   `json:"details"`
			Prompt              string   `json:"prompt"`
			Model               string   `json:"model"`
			DependsOn           []string `json:"depends_on"`
			ExecutionMode       string   `json:"execution_mode"`
			MaxAttempts         int      `json:"max_attempts"`
			RetryBackoffSeconds int      `json:"retry_backoff_seconds"`
			ReviewRequired      bool     `json:"review_required"`
			ReviewPrompt        string   `json:"review_prompt"`
			ReviewModel         string   `json:"review_model"`
			VerifyRequired      bool     `json:"verify_required"`
			VerifyPrompt        string   `json:"verify_prompt"`
			VerifyModel         string   `json:"verify_model"`
			FailureStrategy     string   `json:"failure_strategy"`
			RollbackPrompt      string   `json:"rollback_prompt"`
			RollbackModel       string   `json:"rollback_model"`
			DegradePrompt       string   `json:"degrade_prompt"`
			DegradeModel        string   `json:"degrade_model"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	steps := make([]plan.Step, 0, len(input.Steps))
	for _, item := range input.Steps {
		steps = append(steps, plan.Step{
			ID:                  item.ID,
			Title:               item.Title,
			Details:             item.Details,
			Prompt:              item.Prompt,
			Model:               item.Model,
			DependsOn:           item.DependsOn,
			ExecutionMode:       plan.ExecutionMode(item.ExecutionMode),
			MaxAttempts:         item.MaxAttempts,
			RetryBackoffSeconds: item.RetryBackoffSeconds,
			ReviewRequired:      item.ReviewRequired,
			ReviewPrompt:        item.ReviewPrompt,
			ReviewModel:         item.ReviewModel,
			VerifyRequired:      item.VerifyRequired,
			VerifyPrompt:        item.VerifyPrompt,
			VerifyModel:         item.VerifyModel,
			FailureStrategy:     plan.FailureStrategy(item.FailureStrategy),
			RollbackPrompt:      item.RollbackPrompt,
			RollbackModel:       item.RollbackModel,
			DegradePrompt:       item.DegradePrompt,
			DegradeModel:        item.DegradeModel,
		})
	}
	return t.rt.CreatePlan(ctx, plan.Plan{
		Goal:               input.Goal,
		Summary:            input.Summary,
		SessionID:          input.SessionID,
		MaxParallel:        input.MaxParallel,
		DefaultMaxAttempts: input.DefaultMaxAttempts,
		AutoReview:         input.AutoReview,
		AutoVerify:         input.AutoVerify,
		ReviewModel:        input.ReviewModel,
		VerifyModel:        input.VerifyModel,
		Steps:              steps,
	})
}

type GetPlanTool struct {
	rt Runtime
}

func NewGetPlanTool(rt Runtime) *GetPlanTool { return &GetPlanTool{rt: rt} }

func (t *GetPlanTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "get_plan",
		Description: "Inspect a saved execution plan and its current step state. Use this before editing or advancing a plan when you need exact current status.",
		Parameters: schemaObject(map[string]any{
			"plan_id": schemaString("Identifier of the plan to inspect."),
		}, "plan_id"),
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
		Description: "List durable execution plans, optionally filtered by overall plan status. Useful for discovering whether relevant work is already in progress before creating a new plan.",
		Parameters: schemaObject(map[string]any{
			"limit":  schemaInteger("Maximum number of plans to return."),
			"status": schemaString("Optional status filter such as active, failed, or completed."),
		}),
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
		Description: "Revise a plan step, including retry policy, reviewer and verifier gates, recovery behavior, notes, status, and result. Use this when the plan needs to adapt after new information arrives.",
		Parameters: schemaObject(map[string]any{
			"plan_id":               schemaString("Identifier of the parent plan."),
			"step_id":               schemaString("Identifier of the step to revise."),
			"status":                schemaString("Optional new step status."),
			"title":                 schemaString("Optional replacement step title."),
			"details":               schemaString("Optional replacement step details."),
			"prompt":                schemaString("Optional replacement execution prompt for the step."),
			"model":                 schemaString("Optional replacement step model."),
			"execution_mode":        schemaStringEnum("Optional replacement execution mode.", "subagent", "queued"),
			"depends_on":            schemaArray("Optional replacement dependency list.", schemaString("Dependency step id.")),
			"max_attempts":          schemaInteger("Optional replacement retry budget."),
			"retry_backoff_seconds": schemaInteger("Optional replacement retry delay."),
			"review_required":       schemaBoolean("Whether the step should require review."),
			"review_prompt":         schemaString("Instructions for the review stage."),
			"review_model":          schemaString("Optional review model override."),
			"verify_required":       schemaBoolean("Whether the step should require verification."),
			"verify_prompt":         schemaString("Instructions for the verification stage."),
			"verify_model":          schemaString("Optional verification model override."),
			"failure_strategy":      schemaStringEnum("Recovery strategy if the step fails.", "fail", "rollback", "degrade", "rollback_then_degrade"),
			"rollback_prompt":       schemaString("Rollback instructions for failure recovery."),
			"rollback_model":        schemaString("Optional rollback model override."),
			"degrade_prompt":        schemaString("Fallback instructions for degraded completion."),
			"degrade_model":         schemaString("Optional degraded-fallback model override."),
			"note":                  schemaString("Additional note to append to the step history."),
			"result":                schemaString("Recorded result text for the step."),
			"error":                 schemaString("Recorded failure text for the step."),
		}, "plan_id", "step_id"),
	}
}

func (t *UpdatePlanStepTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		PlanID              string   `json:"plan_id"`
		StepID              string   `json:"step_id"`
		Status              string   `json:"status"`
		Title               string   `json:"title"`
		Details             string   `json:"details"`
		Prompt              string   `json:"prompt"`
		Model               string   `json:"model"`
		ExecutionMode       string   `json:"execution_mode"`
		DependsOn           []string `json:"depends_on"`
		MaxAttempts         *int     `json:"max_attempts"`
		RetryBackoffSeconds *int     `json:"retry_backoff_seconds"`
		ReviewRequired      *bool    `json:"review_required"`
		ReviewPrompt        string   `json:"review_prompt"`
		ReviewModel         string   `json:"review_model"`
		VerifyRequired      *bool    `json:"verify_required"`
		VerifyPrompt        string   `json:"verify_prompt"`
		VerifyModel         string   `json:"verify_model"`
		FailureStrategy     string   `json:"failure_strategy"`
		RollbackPrompt      string   `json:"rollback_prompt"`
		RollbackModel       string   `json:"rollback_model"`
		DegradePrompt       string   `json:"degrade_prompt"`
		DegradeModel        string   `json:"degrade_model"`
		Note                string   `json:"note"`
		Result              string   `json:"result"`
		Error               string   `json:"error"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.UpdatePlanStep(ctx, PlanStepUpdateInput{
		PlanID:              input.PlanID,
		StepID:              input.StepID,
		Status:              input.Status,
		Title:               input.Title,
		Details:             input.Details,
		Prompt:              input.Prompt,
		Model:               input.Model,
		ExecutionMode:       input.ExecutionMode,
		DependsOn:           input.DependsOn,
		MaxAttempts:         input.MaxAttempts,
		RetryBackoffSeconds: input.RetryBackoffSeconds,
		ReviewRequired:      input.ReviewRequired,
		ReviewPrompt:        input.ReviewPrompt,
		ReviewModel:         input.ReviewModel,
		VerifyRequired:      input.VerifyRequired,
		VerifyPrompt:        input.VerifyPrompt,
		VerifyModel:         input.VerifyModel,
		FailureStrategy:     input.FailureStrategy,
		RollbackPrompt:      input.RollbackPrompt,
		RollbackModel:       input.RollbackModel,
		DegradePrompt:       input.DegradePrompt,
		DegradeModel:        input.DegradeModel,
		Note:                input.Note,
		Result:              input.Result,
		Error:               input.Error,
	})
}

type ExecutePlanStepTool struct {
	rt Runtime
}

func NewExecutePlanStepTool(rt Runtime) *ExecutePlanStepTool { return &ExecutePlanStepTool{rt: rt} }

func (t *ExecutePlanStepTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "execute_plan_step",
		Description: "Execute one specific step from a saved plan, including retries plus any verifier, reviewer, and recovery logic attached to that step. Use this when you want to drive one step deliberately instead of advancing the whole plan.",
		Parameters: schemaObject(map[string]any{
			"plan_id": schemaString("Identifier of the parent plan."),
			"step_id": schemaString("Identifier of the step to execute."),
			"mode":    schemaStringEnum("Optional execution-mode override for this run.", "subagent", "queued"),
		}, "plan_id", "step_id"),
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
		Description: "Advance a saved plan by executing or queueing runnable steps in dependency order, with parallel waves, retries, review, verification, and recovery. Prefer this when the plan should make broad forward progress automatically.",
		Parameters: schemaObject(map[string]any{
			"plan_id": schemaString("Identifier of the plan to advance."),
			"limit":   schemaInteger("Maximum number of runnable steps to advance in this call."),
		}, "plan_id"),
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
		Description: "Store a durable memory for later semantic retrieval and layered recall. Use this for stable facts, preferences, constraints, contact knowledge, or project state that should survive the current turn.",
		Parameters: schemaObject(map[string]any{
			"key":        schemaString("Optional stable key for upsert-like memory updates."),
			"layer":      schemaString("Optional memory layer such as owner, people, project, or workflow."),
			"area":       schemaString("Optional sub-area within the layer."),
			"kind":       schemaString("Optional memory kind such as rule, preference, fact, or contact_profile."),
			"subject":    schemaString("Optional subject identifier the memory is about."),
			"summary":    schemaString("Optional short summary of the memory."),
			"content":    schemaString("The durable memory content to store."),
			"source":     schemaString("Optional source describing where the memory came from."),
			"importance": schemaInteger("Optional relative importance score."),
			"confidence": schemaNumber("Optional confidence score for the memory."),
			"tags":       schemaArray("Optional tags for retrieval and filtering.", schemaString("Memory tag.")),
		}, "content"),
	}
}

func (t *RememberTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Key        string   `json:"key"`
		Layer      string   `json:"layer"`
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
		Layer:      input.Layer,
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
		Description: "Search durable memory semantically for facts, preferences, people, projects, and prior workflow. Prefer this when continuity matters and you suspect the agent may already know relevant history.",
		Parameters: schemaObject(map[string]any{
			"query": schemaString("Natural-language query describing what memory to retrieve."),
			"limit": schemaInteger("Maximum number of memory hits to return."),
		}, "query"),
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

type SessionListTool struct {
	rt Runtime
}

func NewSessionListTool(rt Runtime) *SessionListTool { return &SessionListTool{rt: rt} }

func (t *SessionListTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "list_sessions",
		Description: "List saved sessions across channels so Qorvexus can recover context from other conversations, workstreams, or prior threads. Use this before get_session when you do not already know the right session id.",
		Parameters: schemaObject(map[string]any{
			"limit":     schemaInteger("Maximum number of sessions to return."),
			"channel":   schemaString("Optional channel filter such as telegram or slack."),
			"sender_id": schemaString("Optional sender identity filter."),
		}),
	}
}

func (t *SessionListTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Limit    int    `json:"limit"`
		Channel  string `json:"channel"`
		SenderID string `json:"sender_id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.ListSavedSessions(ctx, input.Limit, input.Channel, input.SenderID)
}

type SessionViewTool struct {
	rt Runtime
}

func NewSessionViewTool(rt Runtime) *SessionViewTool { return &SessionViewTool{rt: rt} }

func (t *SessionViewTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "get_session",
		Description: "Read another saved session by id, including recent messages and metadata, when Qorvexus needs cross-session continuity. Use it after list_sessions or when a known session id should be reopened for context.",
		Parameters: schemaObject(map[string]any{
			"session_id":     schemaString("Identifier of the session to inspect."),
			"limit_messages": schemaInteger("Maximum number of recent messages to include."),
			"include_system": schemaBoolean("Whether to include system prompts in the returned view."),
			"include_tool":   schemaBoolean("Whether to include tool messages in the returned view."),
		}, "session_id"),
	}
}

func (t *SessionViewTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		SessionID     string `json:"session_id"`
		LimitMessages int    `json:"limit_messages"`
		IncludeSystem bool   `json:"include_system"`
		IncludeTool   bool   `json:"include_tool"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.GetSessionView(ctx, input.SessionID, input.LimitMessages, input.IncludeSystem, input.IncludeTool)
}

type EnqueueTaskTool struct {
	rt Runtime
}

func NewEnqueueTaskTool(rt Runtime) *EnqueueTaskTool { return &EnqueueTaskTool{rt: rt} }

func (t *EnqueueTaskTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "enqueue_task",
		Description: "Queue a background task for asynchronous execution. Use this when the work should continue later without blocking the current turn.",
		Parameters: schemaObject(map[string]any{
			"name":       schemaString("Human-readable task name."),
			"prompt":     schemaString("Task prompt that the background worker should execute."),
			"model":      schemaString("Optional model override for the queued task."),
			"session_id": schemaString("Optional session id whose context the queued task should continue."),
		}, "name", "prompt"),
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
		Description: "Send a message immediately through a configured social connector when acting as Qorvexus, the owner's autonomous assistant. Use this only when an outbound message should go out now. If timing, wording, or authority is uncertain, prefer hold_social_message first.",
		Parameters: schemaObject(map[string]any{
			"channel":   schemaString("Configured connector or channel name."),
			"thread_id": schemaString("Optional thread or conversation id to reply within."),
			"recipient": schemaString("Optional direct recipient or channel-specific destination."),
			"text":      schemaString("Outbound message text to send."),
		}, "channel", "text"),
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
		Description: "List configured social connectors and channels currently available for outbound communication. Use this before sending when the available routes are unknown.",
		Parameters:  schemaObject(map[string]any{}),
	}
}

func (t *SocialListTool) Invoke(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.rt.ListSocialConnectors(ctx)
}

type SocialHoldTool struct {
	rt Runtime
}

func NewSocialHoldTool(rt Runtime) *SocialHoldTool { return &SocialHoldTool{rt: rt} }

func (t *SocialHoldTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "hold_social_message",
		Description: "Save an outbound message into the assistant outbox instead of sending it immediately. Prefer this when Qorvexus should prepare, revise, wait for approval, or avoid premature outreach.",
		Parameters: schemaObject(map[string]any{
			"channel":   schemaString("Configured connector or channel name."),
			"thread_id": schemaString("Optional thread or conversation id."),
			"recipient": schemaString("Optional direct recipient or channel-specific destination."),
			"text":      schemaString("Outbound draft message text to hold."),
			"reason":    schemaString("Why the message is being held instead of sent immediately."),
		}, "channel", "text"),
	}
}

func (t *SocialHoldTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Channel   string `json:"channel"`
		ThreadID  string `json:"thread_id"`
		Recipient string `json:"recipient"`
		Text      string `json:"text"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.HoldSocialMessage(ctx, input.Channel, input.ThreadID, input.Recipient, input.Text, input.Reason)
}

type GrantOwnerIdentityTool struct {
	rt Runtime
}

func NewGrantOwnerIdentityTool(rt Runtime) *GrantOwnerIdentityTool {
	return &GrantOwnerIdentityTool{rt: rt}
}

func (t *GrantOwnerIdentityTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "grant_owner_identity",
		Description: "Authorize a channel identity as owner when an already-authenticated owner wants to add a new account, device, or chat route. This is a high-trust operation and should be used deliberately.",
		Parameters: schemaObject(map[string]any{
			"channel":     schemaString("Channel where the new owner identity exists."),
			"sender_id":   schemaString("Exact sender id that should be granted owner status."),
			"sender_name": schemaString("Optional human-readable sender name."),
		}),
	}
}

func (t *GrantOwnerIdentityTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Channel    string `json:"channel"`
		SenderID   string `json:"sender_id"`
		SenderName string `json:"sender_name"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.GrantOwnerIdentity(ctx, input.Channel, input.SenderID, input.SenderName)
}

type SocialOutboxListTool struct {
	rt Runtime
}

func NewSocialOutboxListTool(rt Runtime) *SocialOutboxListTool { return &SocialOutboxListTool{rt: rt} }

func (t *SocialOutboxListTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "list_social_outbox",
		Description: "List held or pending outbound social messages in the assistant outbox. Use this to review drafts before deciding whether to send, keep, or discard them.",
		Parameters: schemaObject(map[string]any{
			"limit":  schemaInteger("Maximum number of outbox items to return."),
			"status": schemaString("Optional status filter such as held or pending."),
		}),
	}
}

func (t *SocialOutboxListTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Limit  int    `json:"limit"`
		Status string `json:"status"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.ListSocialOutbox(ctx, input.Limit, input.Status)
}

type SocialOutboxManageTool struct {
	rt Runtime
}

func NewSocialOutboxManageTool(rt Runtime) *SocialOutboxManageTool {
	return &SocialOutboxManageTool{rt: rt}
}

func (t *SocialOutboxManageTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "manage_social_outbox",
		Description: "Manage an assistant outbox entry by sending it, keeping it held, or discarding it. Use edited_text when the draft should be revised before the chosen action.",
		Parameters: schemaObject(map[string]any{
			"outbox_id":   schemaString("Identifier of the outbox entry to manage."),
			"action":      schemaString("Desired action such as send, hold, or discard."),
			"edited_text": schemaString("Optional replacement text to apply before managing the outbox entry."),
		}, "outbox_id"),
	}
}

func (t *SocialOutboxManageTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		OutboxID   string `json:"outbox_id"`
		Action     string `json:"action"`
		EditedText string `json:"edited_text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	return t.rt.ManageSocialOutbox(ctx, input.OutboxID, input.Action, input.EditedText)
}

type SocialGraphTool struct {
	rt Runtime
}

func NewSocialGraphTool(rt Runtime) *SocialGraphTool { return &SocialGraphTool{rt: rt} }

func (t *SocialGraphTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "get_social_contact_graph",
		Description: "Read the structured relationship graph for social contacts, including interaction counts, autonomy boundaries, commitments, and follow-up load. Use this before proactive outreach or relationship-sensitive decisions.",
		Parameters:  schemaObject(map[string]any{}),
	}
}

func (t *SocialGraphTool) Invoke(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.rt.SocialContactGraph(ctx)
}

type SocialFollowUpListTool struct {
	rt Runtime
}

func NewSocialFollowUpListTool(rt Runtime) *SocialFollowUpListTool {
	return &SocialFollowUpListTool{rt: rt}
}

func (t *SocialFollowUpListTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "list_social_followups",
		Description: "List active social follow-up strategies and pending relationship work tracked by Qorvexus. Useful when the assistant needs to continue relationship maintenance intentionally.",
		Parameters: schemaObject(map[string]any{
			"limit":  schemaInteger("Maximum number of follow-up records to return."),
			"status": schemaString("Optional status filter."),
		}),
	}
}

func (t *SocialFollowUpListTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Limit  int    `json:"limit"`
		Status string `json:"status"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.ListSocialFollowUps(ctx, input.Limit, input.Status)
}

type ReadConfigTool struct{ rt Runtime }

func NewReadConfigTool(rt Runtime) *ReadConfigTool { return &ReadConfigTool{rt: rt} }

func (t *ReadConfigTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "read_runtime_config",
		Description: "Read the current runtime configuration file for self-inspection and planning. Prefer this before write_runtime_config so changes are based on the latest full config.",
		Parameters:  schemaObject(map[string]any{}),
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
		Description: "Write a full updated runtime configuration. This replaces the config file content, so it should usually be based on a fresh read_runtime_config result. After changing config, use restart_runtime so the running service reloads it.",
		Parameters: schemaObject(map[string]any{
			"config": schemaString("Full replacement config file contents."),
		}, "config"),
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
		Description: "Create or update a SKILL.md file so the agent can extend its own behavior. Prefer narrowly scoped, reusable instructions instead of stuffing long-term behavior into the base prompt.",
		Parameters: schemaObject(map[string]any{
			"name":        schemaString("Skill name, usually matching the skill directory name."),
			"description": schemaString("Short human-readable description of what the skill is for."),
			"body":        schemaString("Complete SKILL.md body to write."),
		}, "name", "description", "body"),
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
		Description: "Record a self-improvement item for the agent to work on later. Use this when the improvement is worth tracking but not worth executing immediately.",
		Parameters: schemaObject(map[string]any{
			"title":       schemaString("Short title of the improvement idea."),
			"description": schemaString("Concrete description of the problem or improvement."),
			"kind":        schemaString("Optional category such as prompt, tool, workflow, or bugfix."),
		}, "title", "description"),
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
		Description: "List queued self-improvement items and recent ideas for evolving the agent. Useful before adding a duplicate improvement or choosing the next one to promote.",
		Parameters: schemaObject(map[string]any{
			"limit": schemaInteger("Maximum number of improvement items to return."),
		}),
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
		Description: "Turn a self-improvement idea into an asynchronous execution task so the agent can work on it later. Use this when the idea is ready to become concrete work.",
		Parameters: schemaObject(map[string]any{
			"title":       schemaString("Short title of the improvement task."),
			"description": schemaString("Concrete description of the improvement work to perform."),
			"model":       schemaString("Optional model override for the promoted task."),
		}, "title", "description"),
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
		Description: "Generate self-improvement candidates from recent audit history and failures. Use this when the assistant should inspect its own recent misses for reusable fixes.",
		Parameters: schemaObject(map[string]any{
			"limit": schemaInteger("Maximum number of mined candidates to return."),
		}),
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
		Description: "Capture a mined improvement into the backlog and optionally promote it into an execution task. Use this to preserve a good mined idea even if execution will happen later.",
		Parameters: schemaObject(map[string]any{
			"title":       schemaString("Short title of the improvement."),
			"description": schemaString("Concrete description of the improvement."),
			"kind":        schemaString("Optional category such as prompt, tool, workflow, or bugfix."),
			"promote":     schemaBoolean("Whether to immediately promote the captured item into an execution task."),
			"model":       schemaString("Optional model override if promote is true."),
		}, "title", "description"),
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

type RestartRuntimeTool struct{ rt Runtime }

func NewRestartRuntimeTool(rt Runtime) *RestartRuntimeTool { return &RestartRuntimeTool{rt: rt} }

func (t *RestartRuntimeTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "restart_runtime",
		Description: "Ask the supervisor to restart the running runtime so config or skill changes take effect. Use this after write_runtime_config or upsert_skill when the live service must reload those changes.",
		Parameters: schemaObject(map[string]any{
			"reason": schemaString("Optional human-readable reason for the restart request."),
		}),
	}
}

func (t *RestartRuntimeTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Reason string `json:"reason"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.RequestRuntimeRestart(ctx, input.Reason)
}

type ApplySelfUpdateTool struct{ rt Runtime }

func NewApplySelfUpdateTool(rt Runtime) *ApplySelfUpdateTool { return &ApplySelfUpdateTool{rt: rt} }

func (t *ApplySelfUpdateTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "apply_self_update",
		Description: "Build a fresh Qorvexus binary from the current source tree and ask the supervisor to restart into it. Use this after source-code changes that should become live. Prefer restart_runtime instead when only config or skills changed.",
		Parameters: schemaObject(map[string]any{
			"run_tests": schemaBoolean("Whether to run tests before applying the self-update."),
			"reason":    schemaString("Optional human-readable reason for the self-update."),
		}),
	}
}

func (t *ApplySelfUpdateTool) Invoke(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		RunTests bool   `json:"run_tests"`
		Reason   string `json:"reason"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
	}
	return t.rt.ApplySelfUpdate(ctx, input.RunTests, input.Reason)
}
