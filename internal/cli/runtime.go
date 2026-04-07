package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"qorvexus/internal/agent"
	"qorvexus/internal/audit"
	"qorvexus/internal/commitment"
	"qorvexus/internal/config"
	"qorvexus/internal/contextx"
	"qorvexus/internal/memory"
	"qorvexus/internal/model"
	"qorvexus/internal/orchestrator"
	"qorvexus/internal/plan"
	"qorvexus/internal/policy"
	"qorvexus/internal/scheduler"
	"qorvexus/internal/self"
	"qorvexus/internal/session"
	"qorvexus/internal/skill"
	"qorvexus/internal/social"
	"qorvexus/internal/socialinsight"
	"qorvexus/internal/socialplugin"
	_ "qorvexus/internal/socialpluginautoload"
	"qorvexus/internal/socialpluginregistry"
	"qorvexus/internal/taskqueue"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
	"qorvexus/internal/webui"
)

type appRuntime struct {
	cfg         *config.Config
	configPath  string
	runner      *agent.Runner
	discussion  *orchestrator.Discussion
	scheduler   *scheduler.Manager
	sessions    *session.Store
	memory      *memory.Store
	plans       *plan.Store
	queue       *taskqueue.Queue
	worker      *taskqueue.Worker
	webServer   *http.Server
	startedAt   time.Time
	social      *social.Gateway
	insights    *socialinsight.Analyzer
	connectors  *social.Registry
	socialJobs  []socialplugin.BackgroundRunner
	commitments *commitment.Store
	self        *self.Manager
	audit       *audit.Logger
}

const (
	ownerOnboardingSessionID = "owner-onboarding"
)

type planStepExecutionItem struct {
	PlanID     string    `json:"plan_id"`
	StepID     string    `json:"step_id"`
	Title      string    `json:"title"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	TaskID     string    `json:"task_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	ExecutedAt time.Time `json:"executed_at"`
	Plan       plan.Plan `json:"plan"`
}

func newRuntime(cfg *config.Config, configPath string) (*appRuntime, error) {
	registry := model.NewRegistry()
	recorder := model.NewRecorder(filepath.Join(cfg.DataDir, "traces", "model_calls.jsonl"))
	for name, modelCfg := range cfg.Models {
		var client model.Client
		switch strings.ToLower(modelCfg.Provider) {
		case "openai", "openai-compatible", "":
			client = model.NewOpenAIClient(modelCfg)
		default:
			return nil, fmt.Errorf("unsupported provider %q for model %s", modelCfg.Provider, name)
		}
		registry.Register(name, modelCfg, recorder.Wrap(client))
	}

	skills, err := skill.NewLoader().LoadDirs(cfg.Skills.Dirs)
	if err != nil {
		return nil, err
	}

	store := session.NewStore(cfg.DataDir)
	discussion := &orchestrator.Discussion{Registry: registry}
	policyEngine := policy.NewEngine(cfg.Tools)
	app := &appRuntime{
		cfg:         cfg,
		configPath:  configPath,
		discussion:  discussion,
		sessions:    store,
		memory:      memory.NewStore(cfg.Memory.File),
		plans:       plan.NewStore(filepath.Join(cfg.DataDir, "plans.json")),
		startedAt:   time.Now().UTC(),
		connectors:  social.NewRegistry(),
		insights:    socialinsight.NewAnalyzer(),
		commitments: commitment.NewStore(cfg.Social.CommitmentFile),
		self:        self.NewManager(cfg.Self.SkillsDir, cfg.Self.BacklogFile),
		audit:       audit.New(cfg.Audit.File),
	}

	toolRegistry := tool.NewRegistry()
	toolRegistry.Register(&tool.ThinkTool{})
	toolRegistry.Register(tool.NewSystemSnapshotTool())
	toolRegistry.Register(tool.NewFilesystemTool(cfg.Tools))
	toolRegistry.Register(tool.NewProcessTool(cfg.Tools, policyEngine))
	toolRegistry.Register(tool.NewCommandTool(cfg.Tools, policyEngine))
	toolRegistry.Register(tool.NewHTTPTool(cfg.Tools))
	toolRegistry.Register(tool.NewPlaywrightTool(cfg.Tools))
	toolRegistry.Register(tool.NewSubAgentTool(app))
	toolRegistry.Register(tool.NewDiscussTool(app))
	toolRegistry.Register(tool.NewScheduleTool(app))
	toolRegistry.Register(tool.NewCreatePlanTool(app))
	toolRegistry.Register(tool.NewGetPlanTool(app))
	toolRegistry.Register(tool.NewListPlansTool(app))
	toolRegistry.Register(tool.NewUpdatePlanStepTool(app))
	toolRegistry.Register(tool.NewExecutePlanStepTool(app))
	toolRegistry.Register(tool.NewAdvancePlanTool(app))
	toolRegistry.Register(tool.NewRememberTool(app))
	toolRegistry.Register(tool.NewRecallTool(app))
	toolRegistry.Register(tool.NewEnqueueTaskTool(app))
	toolRegistry.Register(tool.NewSocialSendTool(app))
	toolRegistry.Register(tool.NewSocialListTool(app))
	toolRegistry.Register(tool.NewReadConfigTool(app))
	toolRegistry.Register(tool.NewWriteConfigTool(app))
	toolRegistry.Register(tool.NewUpsertSkillTool(app))
	toolRegistry.Register(tool.NewSelfBacklogAddTool(app))
	toolRegistry.Register(tool.NewSelfBacklogListTool(app))
	toolRegistry.Register(tool.NewPromoteSelfImprovementTool(app))
	toolRegistry.Register(tool.NewMineSelfImprovementsTool(app))
	toolRegistry.Register(tool.NewCaptureSelfImprovementTool(app))

	app.runner = &agent.Runner{
		Config:   cfg,
		Models:   registry,
		Sessions: store,
		Tools:    toolRegistry,
		Skills:   skills,
		Memory:   app.memory,
		Plans:    app.plans,
		Compressor: &contextx.Compressor{
			Registry:  registry,
			MaxChars:  cfg.Agent.ContextWindowChars,
			Threshold: cfg.Agent.CompressionThreshold,
		},
	}

	app.scheduler = scheduler.NewManager(cfg.Scheduler.TaskFile, app)
	_ = app.scheduler.Load()

	_ = app.plans.Load()
	app.queue = taskqueue.New(cfg.Queue.File, app)
	_ = app.queue.Load()
	app.worker = &taskqueue.Worker{
		Queue:        app.queue,
		PollInterval: time.Duration(cfg.Queue.PollInterval) * time.Second,
	}
	app.social = social.NewGateway(cfg.Social, cfg.Identity, app)
	pluginManager := socialpluginregistry.NewManager()
	app.socialJobs, err = pluginManager.Setup(cfg.Social, app.connectors, cfg.DataDir, func(runCtx context.Context, env social.Envelope) error {
		_, err := app.HandleSocialEnvelope(runCtx, env)
		return err
	})
	if err != nil {
		return nil, err
	}

	if cfg.Web.Enabled {
		panel, err := webui.NewServer(app)
		if err != nil {
			return nil, err
		}
		app.webServer = &http.Server{
			Addr:    cfg.Web.Address,
			Handler: panel.Handler(),
		}
	}

	return app, nil
}

func (a *appRuntime) RunSocialBackground(ctx context.Context) error {
	for _, job := range a.socialJobs {
		job := job
		go func() {
			_ = job.Run(ctx)
		}()
	}
	return nil
}

func (a *appRuntime) RunSubAgent(ctx context.Context, name string, prompt string, model string) (string, error) {
	return a.runSubAgentWithSession(ctx, subAgentSessionID(name), model, prompt)
}

func (a *appRuntime) ConsultModels(ctx context.Context, prompt string, panel []string) (string, error) {
	return a.discussion.Run(ctx, prompt, panel, a.cfg.Agent.Discussion.SynthesisModel)
}

func (a *appRuntime) AddScheduledTask(_ context.Context, name string, scheduleExpr string, prompt string, model string) (string, error) {
	task := scheduler.Task{
		Name:     name,
		Schedule: scheduleExpr,
		Prompt:   prompt,
		Model:    model,
	}
	if err := a.scheduler.Add(task); err != nil {
		return "", err
	}
	a.logAudit(context.Background(), "schedule_task", "ok", name, map[string]any{"schedule": scheduleExpr, "model": model})
	return fmt.Sprintf("scheduled task %q with cron %q", name, scheduleExpr), nil
}

func (a *appRuntime) CreatePlan(ctx context.Context, item plan.Plan) (string, error) {
	if strings.TrimSpace(item.Goal) == "" {
		return "", fmt.Errorf("plan goal is required")
	}
	if len(item.Steps) == 0 {
		return "", fmt.Errorf("plan requires at least one step")
	}
	stepIDs := map[string]struct{}{}
	for i, step := range item.Steps {
		if strings.TrimSpace(step.Title) == "" {
			return "", fmt.Errorf("step %d title is required", i+1)
		}
		if step.ID == "" {
			continue
		}
		if _, exists := stepIDs[step.ID]; exists {
			return "", fmt.Errorf("duplicate step id %q", step.ID)
		}
		stepIDs[step.ID] = struct{}{}
	}
	for _, step := range item.Steps {
		for _, dep := range step.DependsOn {
			if _, ok := stepIDs[dep]; !ok {
				return "", fmt.Errorf("dependency %q must reference an existing explicit step id", dep)
			}
		}
	}
	if item.SessionID == "" {
		if sessionID, ok := tool.SessionIDFrom(ctx); ok {
			item.SessionID = sessionID
		}
	}
	created, err := a.plans.Create(item)
	if err != nil {
		return "", err
	}
	a.logAudit(ctx, "create_plan", "ok", created.ID, map[string]any{
		"goal":  created.Goal,
		"steps": len(created.Steps),
	})
	return agent.ToolResultJSON(created), nil
}

func (a *appRuntime) GetPlan(_ context.Context, planID string) (string, error) {
	item, err := a.plans.Get(planID)
	if err != nil {
		return "", err
	}
	return agent.ToolResultJSON(item), nil
}

func (a *appRuntime) ListPlans(_ context.Context, limit int, status string) (string, error) {
	items := a.plans.List()
	status = strings.TrimSpace(strings.ToLower(status))
	if limit <= 0 {
		limit = 20
	}
	filtered := make([]plan.Plan, 0, len(items))
	for _, item := range items {
		if status != "" && string(item.Status) != status {
			continue
		}
		filtered = append(filtered, item)
		if len(filtered) >= limit {
			break
		}
	}
	return agent.ToolResultJSON(filtered), nil
}

func (a *appRuntime) UpdatePlanStep(ctx context.Context, input tool.PlanStepUpdateInput) (string, error) {
	if strings.TrimSpace(input.PlanID) == "" || strings.TrimSpace(input.StepID) == "" {
		return "", fmt.Errorf("plan_id and step_id are required")
	}
	var (
		stepStatus    plan.StepStatus
		executionMode plan.ExecutionMode
		err           error
	)
	if strings.TrimSpace(input.Status) != "" {
		stepStatus, err = parsePlanStepStatus(input.Status)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(input.ExecutionMode) != "" {
		executionMode, err = parseExecutionMode(input.ExecutionMode)
		if err != nil {
			return "", err
		}
	}
	updated, err := a.plans.UpdateStep(input.PlanID, input.StepID, func(item *plan.Plan, step *plan.Step) error {
		if input.Title != "" {
			step.Title = input.Title
		}
		if input.Details != "" {
			step.Details = input.Details
		}
		if input.Prompt != "" {
			step.Prompt = input.Prompt
		}
		if input.Model != "" {
			step.Model = input.Model
		}
		if input.ExecutionMode != "" {
			step.ExecutionMode = executionMode
		}
		if input.DependsOn != nil {
			step.DependsOn = append([]string(nil), input.DependsOn...)
		}
		if input.Status != "" {
			step.Status = stepStatus
			if stepStatus == plan.StepStatusPlanned {
				step.Error = ""
				step.Result = ""
				step.TaskID = ""
				step.FinishedAt = time.Time{}
			}
		}
		if strings.TrimSpace(input.Note) != "" {
			step.Notes = append(step.Notes, strings.TrimSpace(input.Note))
		}
		if input.Result != "" {
			step.Result = input.Result
		}
		if input.Error != "" {
			step.Error = input.Error
		}
		if strings.TrimSpace(input.Note) != "" {
			item.Notes = append(item.Notes, fmt.Sprintf("%s: %s", step.ID, strings.TrimSpace(input.Note)))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	a.logAudit(ctx, "update_plan_step", "ok", input.PlanID+":"+input.StepID, map[string]any{
		"status": input.Status,
	})
	return agent.ToolResultJSON(updated), nil
}

func (a *appRuntime) ExecutePlanStep(ctx context.Context, planID string, stepID string, mode string) (string, error) {
	report, err := a.executePlanStep(ctx, planID, stepID, mode)
	if err != nil {
		return "", err
	}
	return agent.ToolResultJSON(report), nil
}

func (a *appRuntime) AdvancePlan(ctx context.Context, planID string, limit int) (string, error) {
	if strings.TrimSpace(planID) == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	if limit <= 0 {
		limit = 3
	}
	type advanceReport struct {
		PlanID  string                  `json:"plan_id"`
		Actions []planStepExecutionItem `json:"actions"`
		Plan    plan.Plan               `json:"plan"`
		Message string                  `json:"message,omitempty"`
	}
	report := advanceReport{PlanID: planID}
	for len(report.Actions) < limit {
		item, err := a.plans.Get(planID)
		if err != nil {
			return "", err
		}
		runnable := plan.RunnableSteps(item)
		if len(runnable) == 0 {
			report.Plan = item
			if len(report.Actions) == 0 {
				report.Message = "no runnable steps"
			}
			break
		}
		action, err := a.executePlanStep(ctx, planID, runnable[0].ID, "")
		if err != nil {
			return "", err
		}
		report.Actions = append(report.Actions, action)
		report.Plan = action.Plan
	}
	if report.Plan.ID == "" {
		item, err := a.plans.Get(planID)
		if err != nil {
			return "", err
		}
		report.Plan = item
	}
	return agent.ToolResultJSON(report), nil
}

func (a *appRuntime) RunScheduled(_ context.Context, task scheduler.Task) error {
	_, err := a.queue.Add(taskqueue.Task{
		Name:      task.Name,
		Prompt:    task.Prompt,
		Model:     task.Model,
		SessionID: "cron-" + task.ID,
	})
	return err
}

func (a *appRuntime) Remember(_ context.Context, entry memory.Entry) (string, error) {
	if !a.cfg.Memory.Enabled {
		return "", fmt.Errorf("memory is disabled")
	}
	if err := a.memory.Upsert(entry); err != nil {
		return "", err
	}
	if entry.Key != "" {
		return "memory upserted", nil
	}
	return "memory stored", nil
}

func (a *appRuntime) Recall(_ context.Context, query string, limit int) (string, error) {
	if !a.cfg.Memory.Enabled {
		return "", fmt.Errorf("memory is disabled")
	}
	results, err := a.memory.Search(query, limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) EnqueueTask(_ context.Context, name string, prompt string, model string, sessionID string) (string, error) {
	if !a.cfg.Queue.Enabled {
		return "", fmt.Errorf("queue is disabled")
	}
	task, err := a.queue.Add(taskqueue.Task{
		Name:      name,
		Prompt:    prompt,
		Model:     model,
		SessionID: sessionID,
	})
	if err != nil {
		return "", err
	}
	a.logAudit(context.Background(), "enqueue_task", "ok", task.ID, map[string]any{"name": name, "model": model})
	return fmt.Sprintf("queued task %q as %s", name, task.ID), nil
}

func (a *appRuntime) RunQueuedTask(ctx context.Context, task taskqueue.Task) (string, error) {
	sessionID := task.SessionID
	if sessionID == "" {
		sessionID = "queue-" + task.ID
	}
	if task.PlanID != "" && task.StepID != "" {
		if _, err := a.plans.UpdateStep(task.PlanID, task.StepID, func(item *plan.Plan, step *plan.Step) error {
			step.Status = plan.StepStatusRunning
			step.SessionID = sessionID
			step.Attempts++
			step.StartedAt = time.Now().UTC()
			step.TaskID = task.ID
			return nil
		}); err != nil {
			return "", err
		}
	}
	_, out, err := a.runner.Run(a.toolExecutionContext(ctx, sessionID), agent.Request{
		SessionID: sessionID,
		Model:     task.Model,
		Prompt:    task.Prompt,
		Context:   a.conversationContextForSubagent(ctx),
	})
	if task.PlanID != "" && task.StepID != "" {
		now := time.Now().UTC()
		if err != nil {
			_, _ = a.plans.UpdateStep(task.PlanID, task.StepID, func(item *plan.Plan, step *plan.Step) error {
				step.Status = plan.StepStatusFailed
				step.Error = err.Error()
				step.FinishedAt = now
				return nil
			})
		} else {
			_, _ = a.plans.UpdateStep(task.PlanID, task.StepID, func(item *plan.Plan, step *plan.Step) error {
				step.Status = plan.StepStatusSucceeded
				step.Result = out
				step.Error = ""
				step.FinishedAt = now
				return nil
			})
		}
	}
	return out, err
}

func (a *appRuntime) runSubAgentWithSession(ctx context.Context, sessionID string, modelName string, prompt string) (string, error) {
	_, out, err := a.runner.Run(a.toolExecutionContext(ctx, sessionID), agent.Request{
		SessionID: sessionID,
		Model:     modelName,
		Prompt:    prompt,
		Context:   a.conversationContextForSubagent(ctx),
	})
	return out, err
}

func (a *appRuntime) executePlanStep(ctx context.Context, planID string, stepID string, mode string) (planStepExecutionItem, error) {
	item, err := a.plans.Get(planID)
	if err != nil {
		return planStepExecutionItem{}, err
	}
	step, ok := plan.FindStep(item, stepID)
	if !ok {
		return planStepExecutionItem{}, fmt.Errorf("step %q not found in plan %q", stepID, planID)
	}
	if step.Status != plan.StepStatusPlanned {
		return planStepExecutionItem{}, fmt.Errorf("step %q is %s, expected planned", stepID, step.Status)
	}
	runnable := false
	for _, candidate := range plan.RunnableSteps(item) {
		if candidate.ID == stepID {
			runnable = true
			break
		}
	}
	if !runnable {
		return planStepExecutionItem{}, fmt.Errorf("step %q has unmet dependencies", stepID)
	}
	executionMode := step.ExecutionMode
	if strings.TrimSpace(mode) != "" {
		executionMode, err = parseExecutionMode(mode)
		if err != nil {
			return planStepExecutionItem{}, err
		}
	}
	if executionMode == "" {
		executionMode = plan.ExecutionSubAgent
	}
	switch executionMode {
	case plan.ExecutionQueued:
		return a.queuePlanStep(ctx, item, step)
	case plan.ExecutionSubAgent:
		return a.runPlanStepWithSubAgent(ctx, item, step)
	default:
		return planStepExecutionItem{}, fmt.Errorf("unsupported execution mode %q", executionMode)
	}
}

func (a *appRuntime) queuePlanStep(ctx context.Context, item plan.Plan, step plan.Step) (planStepExecutionItem, error) {
	if !a.cfg.Queue.Enabled {
		return planStepExecutionItem{}, fmt.Errorf("queue is disabled")
	}
	sessionID := planStepSessionID(item.ID, step.ID)
	task, err := a.queue.Add(taskqueue.Task{
		Name:      "plan-step: " + step.Title,
		Prompt:    buildPlanStepPrompt(item, step),
		Model:     resolvePlanStepModel(a.cfg, step),
		SessionID: sessionID,
		PlanID:    item.ID,
		StepID:    step.ID,
	})
	if err != nil {
		return planStepExecutionItem{}, err
	}
	updated, err := a.plans.UpdateStep(item.ID, step.ID, func(current *plan.Plan, currentStep *plan.Step) error {
		currentStep.Status = plan.StepStatusQueued
		currentStep.TaskID = task.ID
		currentStep.SessionID = sessionID
		currentStep.Error = ""
		currentStep.Result = ""
		return nil
	})
	if err != nil {
		return planStepExecutionItem{}, err
	}
	a.logAudit(ctx, "queue_plan_step", "ok", item.ID+":"+step.ID, map[string]any{
		"task_id": task.ID,
		"mode":    "queued",
	})
	return planStepExecutionItem{
		PlanID:     item.ID,
		StepID:     step.ID,
		Title:      step.Title,
		Mode:       string(plan.ExecutionQueued),
		Status:     string(plan.StepStatusQueued),
		TaskID:     task.ID,
		SessionID:  sessionID,
		ExecutedAt: time.Now().UTC(),
		Plan:       updated,
	}, nil
}

func (a *appRuntime) runPlanStepWithSubAgent(ctx context.Context, item plan.Plan, step plan.Step) (planStepExecutionItem, error) {
	sessionID := planStepSessionID(item.ID, step.ID)
	_, err := a.plans.UpdateStep(item.ID, step.ID, func(current *plan.Plan, currentStep *plan.Step) error {
		currentStep.Status = plan.StepStatusRunning
		currentStep.SessionID = sessionID
		currentStep.TaskID = ""
		currentStep.Error = ""
		currentStep.Result = ""
		currentStep.Attempts++
		currentStep.StartedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return planStepExecutionItem{}, err
	}
	out, runErr := a.runSubAgentWithSession(ctx, sessionID, resolvePlanStepModel(a.cfg, step), buildPlanStepPrompt(item, step))
	now := time.Now().UTC()
	if runErr != nil {
		updated, err := a.plans.UpdateStep(item.ID, step.ID, func(current *plan.Plan, currentStep *plan.Step) error {
			currentStep.Status = plan.StepStatusFailed
			currentStep.Error = runErr.Error()
			currentStep.FinishedAt = now
			return nil
		})
		if err != nil {
			return planStepExecutionItem{}, err
		}
		a.logAudit(ctx, "execute_plan_step", "error", item.ID+":"+step.ID, map[string]any{
			"mode":  "subagent",
			"error": runErr.Error(),
		})
		return planStepExecutionItem{
			PlanID:     item.ID,
			StepID:     step.ID,
			Title:      step.Title,
			Mode:       string(plan.ExecutionSubAgent),
			Status:     string(plan.StepStatusFailed),
			SessionID:  sessionID,
			Error:      runErr.Error(),
			ExecutedAt: now,
			Plan:       updated,
		}, nil
	}
	updated, err := a.plans.UpdateStep(item.ID, step.ID, func(current *plan.Plan, currentStep *plan.Step) error {
		currentStep.Status = plan.StepStatusSucceeded
		currentStep.Result = out
		currentStep.Error = ""
		currentStep.FinishedAt = now
		return nil
	})
	if err != nil {
		return planStepExecutionItem{}, err
	}
	a.logAudit(ctx, "execute_plan_step", "ok", item.ID+":"+step.ID, map[string]any{
		"mode": "subagent",
	})
	return planStepExecutionItem{
		PlanID:     item.ID,
		StepID:     step.ID,
		Title:      step.Title,
		Mode:       string(plan.ExecutionSubAgent),
		Status:     string(plan.StepStatusSucceeded),
		SessionID:  sessionID,
		Result:     out,
		ExecutedAt: now,
		Plan:       updated,
	}, nil
}

func buildPlanStepPrompt(item plan.Plan, step plan.Step) string {
	var b strings.Builder
	b.WriteString("You are executing a focused step inside a larger durable plan for Qorvexus.\n")
	b.WriteString("Plan goal: ")
	b.WriteString(strings.TrimSpace(item.Goal))
	b.WriteString("\n")
	if summary := strings.TrimSpace(item.Summary); summary != "" {
		b.WriteString("Plan summary: ")
		b.WriteString(summary)
		b.WriteString("\n")
	}
	if len(item.Notes) > 0 {
		b.WriteString("Plan notes:\n")
		for _, note := range item.Notes {
			if trimmed := strings.TrimSpace(note); trimmed != "" {
				b.WriteString("- ")
				b.WriteString(trimmed)
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("Current step: ")
	b.WriteString(strings.TrimSpace(step.Title))
	b.WriteString("\n")
	if details := strings.TrimSpace(step.Details); details != "" {
		b.WriteString("Step details: ")
		b.WriteString(details)
		b.WriteString("\n")
	}
	if len(step.DependsOn) > 0 {
		b.WriteString("Completed dependencies:\n")
		for _, depID := range step.DependsOn {
			dep, ok := plan.FindStep(item, depID)
			if !ok {
				continue
			}
			b.WriteString("- ")
			b.WriteString(dep.ID)
			b.WriteString(": ")
			b.WriteString(strings.TrimSpace(dep.Title))
			if result := truncateText(dep.Result, 240); result != "" {
				b.WriteString(" | result: ")
				b.WriteString(result)
			}
			b.WriteString("\n")
		}
	}
	if prompt := strings.TrimSpace(step.Prompt); prompt != "" {
		b.WriteString("Specific instruction: ")
		b.WriteString(prompt)
		b.WriteString("\n")
	}
	b.WriteString("Complete this step directly. Use tools if needed. Return a concise but concrete step result, including any blocker that still remains.\n")
	return strings.TrimSpace(b.String())
}

func resolvePlanStepModel(cfg *config.Config, step plan.Step) string {
	if strings.TrimSpace(step.Model) != "" {
		return step.Model
	}
	return cfg.Agent.DefaultModel
}

func planStepSessionID(planID string, stepID string) string {
	return fmt.Sprintf("plan-%s-%s", sanitize(planID), sanitize(stepID))
}

func subAgentSessionID(name string) string {
	return fmt.Sprintf("subagent-%s-%d", sanitize(name), time.Now().UTC().UnixNano())
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func parsePlanStepStatus(value string) (plan.StepStatus, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(plan.StepStatusPlanned):
		return plan.StepStatusPlanned, nil
	case string(plan.StepStatusQueued):
		return plan.StepStatusQueued, nil
	case string(plan.StepStatusRunning):
		return plan.StepStatusRunning, nil
	case string(plan.StepStatusSucceeded):
		return plan.StepStatusSucceeded, nil
	case string(plan.StepStatusFailed):
		return plan.StepStatusFailed, nil
	case string(plan.StepStatusBlocked):
		return plan.StepStatusBlocked, nil
	case string(plan.StepStatusCancelled):
		return plan.StepStatusCancelled, nil
	default:
		return "", fmt.Errorf("unsupported step status %q", value)
	}
}

func parseExecutionMode(value string) (plan.ExecutionMode, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", string(plan.ExecutionSubAgent):
		return plan.ExecutionSubAgent, nil
	case string(plan.ExecutionQueued):
		return plan.ExecutionQueued, nil
	default:
		return "", fmt.Errorf("unsupported execution mode %q", value)
	}
}

func (a *appRuntime) conversationContextForSubagent(ctx context.Context) *types.ConversationContext {
	convo, ok := tool.ConversationContextFrom(ctx)
	if !ok {
		return nil
	}
	return &convo
}

func (a *appRuntime) toolExecutionContext(ctx context.Context, sessionID string) context.Context {
	next := tool.WithSessionID(ctx, sessionID)
	if convo, ok := tool.ConversationContextFrom(ctx); ok {
		next = tool.WithConversationContext(next, convo)
	}
	return next
}

func (a *appRuntime) Status() webui.Status {
	required, sessionID, prompt := a.ownerOnboardingState()
	return webui.Status{
		StartedAt:                a.startedAt,
		DefaultModel:             a.cfg.Agent.DefaultModel,
		SchedulerEnabled:         a.cfg.Scheduler.Enabled,
		QueueEnabled:             a.cfg.Queue.Enabled,
		MemoryEnabled:            a.cfg.Memory.Enabled,
		SelfEnabled:              a.cfg.Self.Enabled,
		SocialEnabled:            a.cfg.Social.Enabled,
		WebAddress:               a.cfg.Web.Address,
		OwnerOnboardingRequired:  required,
		OwnerOnboardingSessionID: sessionID,
		OwnerOnboardingPrompt:    prompt,
	}
}

func (a *appRuntime) RunPrompt(ctx context.Context, prompt string, model string, sessionID string) (string, error) {
	if sessionID == "" {
		if required, onboardingSessionID, _ := a.ownerOnboardingState(); required {
			sessionID = onboardingSessionID
		}
	}
	_, out, err := a.runner.Run(ctx, agent.Request{
		SessionID: sessionID,
		Model:     model,
		Prompt:    prompt,
		Context: &types.ConversationContext{
			Channel:  "web",
			SenderID: "owner",
			Trust:    types.TrustOwner,
			IsOwner:  true,
		},
	})
	return out, err
}

func (a *appRuntime) ListSessions() ([]session.State, error) {
	return a.sessions.List()
}

func (a *appRuntime) ListQueue() []taskqueue.Task {
	if a.queue == nil {
		return nil
	}
	return a.queue.List()
}

func (a *appRuntime) SearchMemory(query string, limit int) (string, error) {
	return a.Recall(context.Background(), query, limit)
}

func (a *appRuntime) EnsureOwnerOnboarding(ctx context.Context) (string, error) {
	required, sessionID, prompt := a.ownerOnboardingState()
	if !required {
		return "", nil
	}
	if strings.TrimSpace(prompt) != "" {
		return prompt, nil
	}
	_, out, err := a.runner.Run(ownerContext(ctx), agent.Request{
		SessionID: sessionID,
		Prompt: strings.TrimSpace(`You are onboarding your owner for the first time.
Your goal is to learn who the owner is and preserve a durable owner profile for future sessions.
Ask a short, warm set of questions that helps you learn:
- what the owner wants to be called
- their role or background
- timezone or locale
- what they want this bot to help with
- communication style preferences
- any boundaries, do-not-do rules, or identity details that must be remembered

Do not dump a long questionnaire. Ask up to five concise questions in this turn and invite the owner to reply in the same session.
As soon as you have enough stable facts, call the remember tool to store them as structured owner memories.
Use stable keys like "owner:identity:name", "owner:identity:preferred_name", "owner:identity:timezone", "owner:goals:primary_needs", or hashed keys for preferences and rules.
Use areas such as "owner_profile", "owner_preferences", and "owner_rules", source "bootstrap:owner_onboarding", and tags including "owner_profile" and "memory_area:owner_profile".
Keep the questions practical and focused on facts that will help you serve the owner well.`),
		Context: &types.ConversationContext{
			Channel:  "bootstrap",
			SenderID: "owner",
			Trust:    types.TrustOwner,
			IsOwner:  true,
		},
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

func (a *appRuntime) LoadConfigText() (string, error) {
	return webui.LoadConfigText(a.configPath)
}

func (a *appRuntime) SocialWebhookAdapters() []social.WebhookAdapter {
	return a.connectors.Webhooks()
}

func (a *appRuntime) SaveConfigText(raw string) error {
	return webui.SaveConfigText(a.configPath, raw)
}

func (a *appRuntime) ReadRuntimeConfig(_ context.Context) (string, error) {
	return webui.LoadConfigText(a.configPath)
}

func (a *appRuntime) WriteRuntimeConfig(ctx context.Context, raw string) (string, error) {
	if !a.cfg.Self.Enabled || !a.cfg.Self.AllowConfigEdits {
		return "", fmt.Errorf("self config edits are disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("config edits require owner context")
	}
	if err := webui.SaveConfigText(a.configPath, raw); err != nil {
		return "", err
	}
	a.logAudit(ctx, "write_runtime_config", "ok", a.configPath, nil)
	return "runtime config updated", nil
}

func (a *appRuntime) UpsertSkill(ctx context.Context, name string, description string, body string) (string, error) {
	if !a.cfg.Self.Enabled || !a.cfg.Self.AllowSkillWrites {
		return "", fmt.Errorf("self skill writes are disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("skill writes require owner context")
	}
	path, err := a.self.UpsertSkill(name, description, body)
	if err != nil {
		return "", err
	}
	a.logAudit(ctx, "upsert_skill", "ok", path, map[string]any{"skill": name})
	return path, nil
}

func (a *appRuntime) AddSelfImprovement(ctx context.Context, title string, description string, kind string) (string, error) {
	if !a.cfg.Self.Enabled {
		return "", fmt.Errorf("self improvement is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("self improvement backlog writes require owner context")
	}
	if kind == "" {
		kind = "general"
	}
	if err := a.self.AppendBacklog(self.BacklogEntry{
		Title:       title,
		Description: description,
		Kind:        kind,
	}); err != nil {
		return "", err
	}
	a.logAudit(ctx, "add_self_improvement", "ok", title, map[string]any{"kind": kind})
	return "self-improvement item recorded", nil
}

func (a *appRuntime) ListSelfImprovements(_ context.Context, limit int) (string, error) {
	items, err := a.self.ListBacklog(limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) ListRecentSocial(_ context.Context, limit int) (string, error) {
	if a.social == nil {
		return "[]", nil
	}
	items, err := a.social.Recent(limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) PromoteSelfImprovement(ctx context.Context, title string, description string, modelName string) (string, error) {
	if !a.cfg.Self.Enabled {
		return "", fmt.Errorf("self improvement is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("promoting self improvement requires owner context")
	}
	prompt := "Work on this self-improvement task for Qorvexus.\nTitle: " + title + "\nDescription: " + description + "\nMake concrete progress and use tools if needed."
	out, err := a.EnqueueTask(ctx, "self-improvement: "+title, prompt, modelName, "")
	if err == nil {
		a.logAudit(ctx, "promote_self_improvement", "ok", title, map[string]any{"model": modelName})
	}
	return out, err
}

func (a *appRuntime) MineSelfImprovements(ctx context.Context, limit int) (string, error) {
	if !a.cfg.Self.Enabled {
		return "", fmt.Errorf("self improvement is disabled")
	}
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("mining self improvements requires owner context")
	}
	if limit <= 0 {
		limit = 20
	}
	if !a.cfg.Audit.Enabled {
		return "[]", nil
	}
	entries, err := a.audit.Recent(limit)
	if err != nil {
		return "", err
	}
	type candidate struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		Source      string `json:"source"`
	}
	var out []candidate
	for _, entry := range entries {
		if entry.Status != "ok" && entry.Status != "" {
			out = append(out, candidate{
				Title:       "Investigate failed action: " + entry.Action,
				Description: "A recent audited action did not complete cleanly and may need a workflow, prompt, or tool improvement.",
				Kind:        "reliability",
				Source:      entry.Action,
			})
			continue
		}
		if entry.Channel != "" && entry.Trust == string(types.TrustExternal) {
			out = append(out, candidate{
				Title:       "Harden external social boundary on " + entry.Channel,
				Description: "Recent external social activity suggests Qorvexus should further refine delegated authority, message review heuristics, or outbound safeguards on this channel.",
				Kind:        "social-safety",
				Source:      entry.Action,
			})
		}
		switch entry.Action {
		case "retry_queue_task":
			out = append(out, candidate{
				Title:       "Reduce queue retries",
				Description: "Recent manual queue retries suggest the task execution flow should be more reliable or recoverable.",
				Kind:        "reliability",
				Source:      entry.Action,
			})
		case "write_runtime_config", "upsert_skill":
			out = append(out, candidate{
				Title:       "Review self-modification ergonomics",
				Description: "Frequent self-modification actions suggest a chance to make configuration and skill evolution safer or more structured.",
				Kind:        "self-optimization",
				Source:      entry.Action,
			})
		}
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) CaptureSelfImprovement(ctx context.Context, title string, description string, kind string, promote bool, modelName string) (string, error) {
	out, err := a.AddSelfImprovement(ctx, title, description, kind)
	if err != nil {
		return "", err
	}
	if !promote {
		return out, nil
	}
	promoted, err := a.PromoteSelfImprovement(ctx, title, description, modelName)
	if err != nil {
		return out + "\n" + err.Error(), nil
	}
	return out + "\n" + promoted, nil
}

func (a *appRuntime) HandleSocialEnvelope(ctx context.Context, env social.Envelope) (string, error) {
	if a.social == nil {
		return a.HandleEnvelope(ctx, env)
	}
	return a.social.Receive(ctx, env)
}

func (a *appRuntime) SendSocialMessage(ctx context.Context, channel string, threadID string, recipient string, text string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("outbound social messages require owner context")
	}
	out, err := a.connectors.Send(ctx, channel, social.OutboundMessage{
		Channel:   channel,
		ThreadID:  threadID,
		Recipient: recipient,
		Text:      text,
		Context: types.ConversationContext{
			Channel:      channel,
			Trust:        types.TrustOwner,
			IsOwner:      true,
			ReplyAsAgent: true,
		},
	})
	if err == nil {
		a.logAudit(ctx, "send_social_message", "ok", channel, map[string]any{"thread_id": threadID, "recipient": recipient})
	}
	return out, err
}

func (a *appRuntime) ListSocialConnectors(_ context.Context) (string, error) {
	raw, err := json.MarshalIndent(a.connectors.List(), "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) ListCommitments(_ context.Context, limit int) (string, error) {
	items, err := a.commitments.List(limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) CommitmentSummary(_ context.Context) (string, error) {
	summary, err := a.commitments.Summary()
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) ScanCommitments(ctx context.Context) (string, error) {
	items, err := a.commitments.List(0)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	scan := commitment.Scan(items, now)
	type scanSummary struct {
		Checked         int      `json:"checked"`
		MarkedOverdue   []string `json:"marked_overdue"`
		QueuedReview    []string `json:"queued_review"`
		EscalatedReview []string `json:"escalated_review"`
	}
	out := scanSummary{Checked: len(items)}
	for _, entry := range scan.Overdue {
		if err := a.commitments.UpdateStatus(entry.ID, commitment.StatusOverdue); err == nil {
			out.MarkedOverdue = append(out.MarkedOverdue, entry.ID)
			a.logAudit(ctx, "mark_commitment_overdue", "ok", entry.ID, map[string]any{
				"due_hint": entry.DueHint,
				"channel":  entry.Channel,
			})
		}
		updated, getErr := a.commitments.Get(entry.ID)
		if getErr == nil {
			entry = updated
		}
		if !commitment.ShouldQueueReview(entry, now) {
			continue
		}
		level := commitment.NextEscalationLevel(entry, now)
		taskID, err := a.enqueueCommitmentReview(ctx, entry, level, "This commitment appears overdue. Review what was promised, decide whether to follow up externally, escalate to the owner, or queue concrete delivery work.")
		if err == nil && taskID != "" {
			out.QueuedReview = append(out.QueuedReview, taskID)
			if level > entry.EscalationLevel {
				out.EscalatedReview = append(out.EscalatedReview, entry.ID)
			}
			_ = a.commitments.NoteReviewQueued(entry.ID, taskID, level, now)
		}
	}
	for _, entry := range scan.Stale {
		if !commitment.ShouldQueueReview(entry, now) {
			continue
		}
		level := commitment.NextEscalationLevel(entry, now)
		taskID, err := a.enqueueCommitmentReview(ctx, entry, level, "This commitment is getting stale. Review whether it needs a reminder, owner escalation, or a concrete next-step task before it becomes overdue.")
		if err == nil && taskID != "" {
			out.QueuedReview = append(out.QueuedReview, taskID)
			if level > entry.EscalationLevel {
				out.EscalatedReview = append(out.EscalatedReview, entry.ID)
			}
			_ = a.commitments.NoteReviewQueued(entry.ID, taskID, level, now)
		}
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) RunCommitmentWatchdog(ctx context.Context) error {
	interval := time.Duration(a.cfg.Social.CommitmentScanIntervalSeconds) * time.Second
	if !a.cfg.Social.Enabled || interval <= 0 {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, _ = a.ScanCommitments(context.Background())
		}
	}
}

func (a *appRuntime) UpdateCommitmentStatus(ctx context.Context, id string, status string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("updating commitments requires owner context")
	}
	if err := a.commitments.UpdateStatus(id, commitment.Status(status)); err != nil {
		return "", err
	}
	a.logAudit(ctx, "update_commitment_status", "ok", id, map[string]any{"status": status})
	return "commitment status updated", nil
}

func (a *appRuntime) RetryQueueTask(ctx context.Context, id string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("retrying queue tasks requires owner context")
	}
	if err := a.queue.Retry(id); err != nil {
		return "", err
	}
	a.logAudit(ctx, "retry_queue_task", "ok", id, nil)
	return "queue task retried", nil
}

func (a *appRuntime) UpdateSelfImprovementStatus(ctx context.Context, id string, status string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("updating self improvement status requires owner context")
	}
	if err := a.self.UpdateStatus(id, status); err != nil {
		return "", err
	}
	a.logAudit(ctx, "update_self_improvement_status", "ok", id, map[string]any{"status": status})
	return "self improvement status updated", nil
}

func (a *appRuntime) ListAudit(_ context.Context, limit int) (string, error) {
	if !a.cfg.Audit.Enabled {
		return "[]", nil
	}
	items, err := a.audit.Recent(limit)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) HandleEnvelope(ctx context.Context, env social.Envelope) (string, error) {
	sessionID := env.Channel + "-" + env.ThreadID
	if sessionID == "-" || sessionID == "" {
		sessionID = env.Channel + "-" + env.SenderID
	}
	toolCtx := tool.WithConversationContext(ctx, env.Context)
	a.logAudit(toolCtx, "receive_social_message", "ok", sessionID, map[string]any{
		"channel":     env.Channel,
		"thread_id":   env.ThreadID,
		"sender_id":   env.SenderID,
		"sender_name": env.SenderName,
	})
	var parts []types.ContentPart
	for _, image := range env.Images {
		parts = append(parts, types.ContentPart{Type: "image_url", ImageURL: image})
	}
	_, out, err := a.runner.Run(ctx, agent.Request{
		SessionID: sessionID,
		Prompt:    env.Text,
		Parts:     parts,
		Context:   &env.Context,
	})
	if err == nil {
		a.logAudit(toolCtx, "reply_social_message", "ok", sessionID, map[string]any{
			"channel":   env.Channel,
			"thread_id": env.ThreadID,
		})
		if strings.TrimSpace(out) != "" {
			if _, sendErr := a.connectors.Send(toolCtx, env.Channel, social.OutboundMessage{
				Channel:   env.Channel,
				ThreadID:  env.ThreadID,
				Recipient: env.SenderID,
				Text:      out,
				Context:   env.Context,
			}); sendErr == nil {
				a.logAudit(toolCtx, "deliver_social_reply", "ok", sessionID, map[string]any{
					"channel":   env.Channel,
					"thread_id": env.ThreadID,
				})
			} else {
				a.logAudit(toolCtx, "deliver_social_reply", "error", sessionID, map[string]any{
					"channel":   env.Channel,
					"thread_id": env.ThreadID,
					"error":     sendErr.Error(),
				})
			}
		}
		a.captureSocialInsights(toolCtx, env, out)
	}
	return out, err
}

func sanitize(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func ownerAllowedFromContext(ctx context.Context) bool {
	convo, ok := tool.ConversationContextFrom(ctx)
	if !ok {
		return false
	}
	return convo.IsOwner || convo.Trust == types.TrustOwner
}

func ownerContext(ctx context.Context) context.Context {
	return tool.WithConversationContext(ctx, types.ConversationContext{
		Channel:  "bootstrap",
		SenderID: "owner",
		Trust:    types.TrustOwner,
		IsOwner:  true,
	})
}

func (a *appRuntime) ownerProfileEntries(limit int) []memory.Entry {
	if a.memory == nil || !a.cfg.Memory.Enabled {
		return nil
	}
	items, err := a.memory.SearchWithOptions(memory.SearchOptions{
		Areas: []string{"owner_profile", "owner_preferences", "owner_rules"},
		Limit: limit,
	})
	if err != nil {
		return nil
	}
	return items
}

func (a *appRuntime) ownerOnboardingState() (bool, string, string) {
	if !a.cfg.Memory.Enabled {
		return false, "", ""
	}
	if len(a.ownerProfileEntries(1)) > 0 {
		return false, "", ""
	}
	st, err := a.sessions.Load(ownerOnboardingSessionID)
	if err != nil {
		return true, ownerOnboardingSessionID, ""
	}
	for i := len(st.Messages) - 1; i >= 0; i-- {
		msg := st.Messages[i]
		if msg.Role == types.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
			return true, ownerOnboardingSessionID, strings.TrimSpace(msg.Content)
		}
	}
	return true, ownerOnboardingSessionID, ""
}

func (a *appRuntime) logAudit(ctx context.Context, action string, status string, target string, metadata map[string]any) {
	if !a.cfg.Audit.Enabled {
		return
	}
	var actor, channel, trust string
	if convo, ok := tool.ConversationContextFrom(ctx); ok {
		actor = convo.SenderID
		channel = convo.Channel
		trust = string(convo.Trust)
	}
	_ = a.audit.Append(audit.Entry{
		Action:   action,
		Actor:    actor,
		Channel:  channel,
		Trust:    trust,
		Status:   status,
		Target:   target,
		Metadata: metadata,
	})
}

func (a *appRuntime) captureSocialInsights(ctx context.Context, env social.Envelope, response string) {
	if a.insights == nil {
		return
	}
	result := a.insights.Analyze(env, response)
	var followUpTaskID string
	for _, note := range result.Memories {
		if a.cfg.Memory.Enabled {
			if err := a.memory.Append(memory.Entry{
				Area:       "contacts",
				Kind:       "contact_note",
				Subject:    env.SenderID,
				Summary:    note.Content,
				Content:    note.Content,
				Source:     note.Source,
				Tags:       note.Tags,
				Importance: 6,
				Confidence: 0.8,
			}); err == nil {
				a.logAudit(ctx, "remember_social_contact", "ok", env.Channel, map[string]any{
					"sender_id": env.SenderID,
					"thread_id": env.ThreadID,
				})
			}
		}
	}
	for _, suggestion := range result.Tasks {
		if a.cfg.Queue.Enabled {
			task, err := a.queue.Add(taskqueue.Task{
				Name:      suggestion.Name,
				Prompt:    suggestion.Prompt,
				Model:     a.cfg.Agent.DefaultModel,
				SessionID: "social-followup-" + sanitize(env.Channel+"-"+env.ThreadID+"-"+env.SenderID),
			})
			if err == nil {
				if followUpTaskID == "" {
					followUpTaskID = task.ID
				}
				a.logAudit(ctx, "enqueue_social_followup", "ok", task.ID, map[string]any{
					"name":      suggestion.Name,
					"sender_id": env.SenderID,
					"thread_id": env.ThreadID,
				})
			}
		}
	}
	for _, suggestion := range result.Commitments {
		relatedTaskID := followUpTaskID
		if a.cfg.Queue.Enabled && (suggestion.DueHint != "" || relatedTaskID == "") {
			taskID, err := a.enqueueCommitmentReview(ctx, commitment.Entry{
				Channel:      env.Channel,
				ThreadID:     env.ThreadID,
				Counterparty: suggestion.Counterparty,
				Summary:      suggestion.Summary,
				DueHint:      suggestion.DueHint,
				Trust:        string(env.Context.Trust),
				Source:       "social:" + env.Channel,
			}, 1, "Review and advance this commitment for Qorvexus based on the recent social exchange.")
			if err == nil && taskID != "" {
				relatedTaskID = taskID
			}
		}
		entry, err := a.commitments.Append(commitment.Entry{
			Channel:       env.Channel,
			ThreadID:      env.ThreadID,
			Counterparty:  suggestion.Counterparty,
			Summary:       suggestion.Summary,
			DueHint:       suggestion.DueHint,
			Trust:         string(env.Context.Trust),
			Source:        "social:" + env.Channel,
			RelatedTaskID: relatedTaskID,
		})
		if err == nil {
			a.logAudit(ctx, "record_social_commitment", "ok", entry.ID, map[string]any{
				"summary":      suggestion.Summary,
				"counterparty": suggestion.Counterparty,
				"due_hint":     suggestion.DueHint,
			})
		}
	}
}

func (a *appRuntime) enqueueCommitmentReview(ctx context.Context, entry commitment.Entry, escalationLevel int, instruction string) (string, error) {
	if !a.cfg.Queue.Enabled {
		return "", nil
	}
	task, err := a.queue.Add(taskqueue.Task{
		Name: "commitment-review: " + entry.Summary,
		Prompt: strings.TrimSpace(fmt.Sprintf(
			"%s\n"+
				"Channel: %s\nThread: %s\nCounterparty: %s\nTrust: %s\nCommitment: %s\nDue hint: %s\nEscalation level: %d\nSource: %s\n"+
				"Decide whether to prepare a deliverable, send a follow-up, ask the owner for approval, or queue further work. Respect authority boundaries and keep the commitment moving.",
			instruction,
			entry.Channel,
			entry.ThreadID,
			entry.Counterparty,
			entry.Trust,
			entry.Summary,
			entry.DueHint,
			escalationLevel,
			entry.Source,
		)),
		Model:     a.cfg.Agent.DefaultModel,
		SessionID: "commitment-review-" + sanitize(entry.Channel+"-"+entry.ThreadID+"-"+entry.Counterparty),
	})
	if err != nil {
		return "", err
	}
	a.logAudit(ctx, "enqueue_commitment_review", "ok", task.ID, map[string]any{
		"summary":          entry.Summary,
		"counterparty":     entry.Counterparty,
		"due_hint":         entry.DueHint,
		"escalation_level": escalationLevel,
	})
	return task.ID, nil
}
