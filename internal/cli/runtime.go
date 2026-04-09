package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	"qorvexus/internal/runtimecontrol"
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
	cfg             *config.Config
	configPath      string
	runner          *agent.Runner
	discussion      *orchestrator.Discussion
	scheduler       *scheduler.Manager
	sessions        *session.Store
	memory          *memory.Store
	plans           *plan.Store
	playwright      *tool.PlaywrightManager
	queue           *taskqueue.Queue
	worker          *taskqueue.Worker
	webServer       *http.Server
	startedAt       time.Time
	social          *social.Gateway
	socialDrafts    *social.DraftStore
	socialGraph     *social.GraphStore
	socialFollowUps *social.FollowUpStore
	insights        *socialinsight.Analyzer
	connectors      *social.Registry
	socialJobs      []socialplugin.BackgroundRunner
	commitments     *commitment.Store
	self            *self.Manager
	audit           *audit.Logger
	runtimeControl  *runtimecontrol.Client
	executablePath  string
	sourceRoot      string
	runtimeMu       sync.Mutex
}

const (
	ownerOnboardingSessionID = "owner-onboarding"
)

type planStepExecutionItem struct {
	PlanID         string    `json:"plan_id"`
	StepID         string    `json:"step_id"`
	Title          string    `json:"title"`
	Mode           string    `json:"mode"`
	Status         string    `json:"status"`
	Attempts       int       `json:"attempts,omitempty"`
	TaskID         string    `json:"task_id,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	ReviewStatus   string    `json:"review_status,omitempty"`
	VerifyStatus   string    `json:"verify_status,omitempty"`
	RollbackResult string    `json:"rollback_result,omitempty"`
	DegradeResult  string    `json:"degrade_result,omitempty"`
	Result         string    `json:"result,omitempty"`
	Error          string    `json:"error,omitempty"`
	ExecutedAt     time.Time `json:"executed_at"`
	Plan           plan.Plan `json:"plan"`
}

type planCheckVerdict struct {
	Verdict string   `json:"verdict"`
	Summary string   `json:"summary"`
	Issues  []string `json:"issues,omitempty"`
	Raw     string   `json:"-"`
}

func newRuntime(cfg *config.Config, configPath string) (*appRuntime, error) {
	executablePath, _ := os.Executable()
	workingDir, _ := os.Getwd()
	sourceRoot := discoverSourceRoot(
		os.Getenv(runtimecontrol.EnvSourceRoot),
		workingDir,
		filepath.Dir(configPath),
		filepath.Dir(executablePath),
	)
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
	playwrightManager := tool.NewPlaywrightManager(cfg.Tools)
	app := &appRuntime{
		cfg:        cfg,
		configPath: configPath,
		discussion: discussion,
		sessions:   store,
		memory: memory.NewStoreWithOptions(memory.Options{
			Path:                cfg.Memory.File,
			Models:              registry,
			EmbeddingModel:      cfg.Memory.EmbeddingModel,
			SummaryModel:        cfg.Memory.SummaryModel,
			SemanticSearch:      cfg.Memory.SemanticSearch == nil || *cfg.Memory.SemanticSearch,
			CompactionThreshold: cfg.Memory.CompactionThreshold,
			CompactionRetain:    cfg.Memory.CompactionRetain,
			MaxSummarySources:   cfg.Memory.MaxSummarySources,
		}),
		plans:           plan.NewStore(filepath.Join(cfg.DataDir, "plans.json")),
		playwright:      playwrightManager,
		startedAt:       time.Now().UTC(),
		connectors:      social.NewRegistry(),
		socialDrafts:    social.NewDraftStore(cfg.Social.DraftFile),
		socialGraph:     social.NewGraphStore(cfg.Social.GraphFile),
		socialFollowUps: social.NewFollowUpStore(cfg.Social.FollowUpFile),
		insights:        socialinsight.NewAnalyzer(),
		commitments:     commitment.NewStore(cfg.Social.CommitmentFile),
		self:            self.NewManager(cfg.Self.SkillsDir, cfg.Self.BacklogFile),
		audit:           audit.New(cfg.Audit.File),
		runtimeControl:  runtimecontrol.NewClientFromEnv(),
		executablePath:  executablePath,
		sourceRoot:      sourceRoot,
	}

	toolRegistry := tool.NewRegistry()
	toolRegistry.Register(&tool.ThinkTool{})
	toolRegistry.Register(tool.NewSystemSnapshotTool())
	toolRegistry.Register(tool.NewFilesystemTool(cfg.Tools))
	toolRegistry.Register(tool.NewRepoIndexTool())
	toolRegistry.Register(tool.NewRepoSearchTool(cfg.Tools))
	toolRegistry.Register(tool.NewApplyDiffTool(cfg.Tools))
	toolRegistry.Register(tool.NewChangeSummaryTool(cfg.Tools))
	toolRegistry.Register(tool.NewTestFailureLocatorTool(cfg.Tools, policyEngine))
	toolRegistry.Register(tool.NewProcessTool(cfg.Tools, policyEngine))
	toolRegistry.Register(tool.NewCommandTool(cfg.Tools, policyEngine))
	toolRegistry.Register(tool.NewHTTPTool(cfg.Tools))
	toolRegistry.Register(tool.NewPlaywrightTool(cfg.Tools, playwrightManager))
	toolRegistry.Register(tool.NewBrowserWorkflowTool(cfg.Tools, playwrightManager))
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
	toolRegistry.Register(tool.NewSocialHoldTool(app))
	toolRegistry.Register(tool.NewSocialOutboxListTool(app))
	toolRegistry.Register(tool.NewSocialOutboxManageTool(app))
	toolRegistry.Register(tool.NewSocialListTool(app))
	toolRegistry.Register(tool.NewSocialGraphTool(app))
	toolRegistry.Register(tool.NewSocialFollowUpListTool(app))
	toolRegistry.Register(tool.NewReadConfigTool(app))
	toolRegistry.Register(tool.NewWriteConfigTool(app))
	toolRegistry.Register(tool.NewUpsertSkillTool(app))
	toolRegistry.Register(tool.NewSelfBacklogAddTool(app))
	toolRegistry.Register(tool.NewSelfBacklogListTool(app))
	toolRegistry.Register(tool.NewPromoteSelfImprovementTool(app))
	toolRegistry.Register(tool.NewMineSelfImprovementsTool(app))
	toolRegistry.Register(tool.NewCaptureSelfImprovementTool(app))
	toolRegistry.Register(tool.NewRestartRuntimeTool(app))
	toolRegistry.Register(tool.NewApplySelfUpdateTool(app))

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

func (a *appRuntime) EnsureBrowserRuntime(ctx context.Context) (string, error) {
	if a.playwright == nil {
		return "", nil
	}
	status, err := a.playwright.EnsureInstalled(ctx, a.cfg.Tools.PlaywrightBrowser)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("playwright runtime ready: browser=%s runtime=%s", status.Browser, status.RuntimeDir), nil
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
		if _, err := parseFailureStrategy(string(step.FailureStrategy)); err != nil {
			return "", fmt.Errorf("step %q: %w", step.Title, err)
		}
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
		stepStatus      plan.StepStatus
		executionMode   plan.ExecutionMode
		failureStrategy plan.FailureStrategy
		err             error
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
	if strings.TrimSpace(input.FailureStrategy) != "" {
		failureStrategy, err = parseFailureStrategy(input.FailureStrategy)
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
		if input.MaxAttempts != nil && *input.MaxAttempts > 0 {
			step.MaxAttempts = *input.MaxAttempts
		}
		if input.RetryBackoffSeconds != nil && *input.RetryBackoffSeconds >= 0 {
			step.RetryBackoffSeconds = *input.RetryBackoffSeconds
		}
		if input.ReviewRequired != nil {
			step.ReviewRequired = *input.ReviewRequired
			step.ReviewStatus = pendingCheckStatus(item, *step, true)
		}
		if input.ReviewPrompt != "" {
			step.ReviewPrompt = input.ReviewPrompt
		}
		if input.ReviewModel != "" {
			step.ReviewModel = input.ReviewModel
		}
		if input.VerifyRequired != nil {
			step.VerifyRequired = *input.VerifyRequired
			step.VerifyStatus = pendingCheckStatus(item, *step, false)
		}
		if input.VerifyPrompt != "" {
			step.VerifyPrompt = input.VerifyPrompt
		}
		if input.VerifyModel != "" {
			step.VerifyModel = input.VerifyModel
		}
		if input.FailureStrategy != "" {
			step.FailureStrategy = failureStrategy
		}
		if input.RollbackPrompt != "" {
			step.RollbackPrompt = input.RollbackPrompt
		}
		if input.RollbackModel != "" {
			step.RollbackModel = input.RollbackModel
		}
		if input.DegradePrompt != "" {
			step.DegradePrompt = input.DegradePrompt
		}
		if input.DegradeModel != "" {
			step.DegradeModel = input.DegradeModel
		}
		if input.Status != "" {
			step.Status = stepStatus
			if stepStatus == plan.StepStatusPlanned {
				step.Error = ""
				step.Result = ""
				step.TaskID = ""
				step.SessionID = ""
				step.ReviewResult = ""
				step.ReviewSessionID = ""
				step.ReviewedAt = time.Time{}
				step.VerifyResult = ""
				step.VerifySessionID = ""
				step.VerifiedAt = time.Time{}
				step.RollbackResult = ""
				step.RollbackSessionID = ""
				step.RolledBackAt = time.Time{}
				step.DegradeResult = ""
				step.DegradeSessionID = ""
				step.DegradedAt = time.Time{}
				step.ReviewStatus = pendingCheckStatus(item, *step, true)
				step.VerifyStatus = pendingCheckStatus(item, *step, false)
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
		limit = 4
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
		waveLimit := item.MaxParallel
		if waveLimit <= 0 {
			waveLimit = 1
		}
		remaining := limit - len(report.Actions)
		if waveLimit > remaining {
			waveLimit = remaining
		}
		if waveLimit > len(runnable) {
			waveLimit = len(runnable)
		}
		selected := runnable[:waveLimit]
		results := make([]planStepExecutionItem, len(selected))
		errs := make([]error, len(selected))
		var wg sync.WaitGroup
		for i, step := range selected {
			i := i
			step := step
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], errs[i] = a.executePlanStep(ctx, planID, step.ID, "")
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return "", err
			}
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i].StepID < results[j].StepID
		})
		report.Actions = append(report.Actions, results...)
		if len(results) > 0 {
			report.Plan = results[len(results)-1].Plan
		}
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
	if task.PlanID != "" && task.StepID != "" {
		return a.runQueuedPlanStep(ctx, task)
	}
	sessionID := task.SessionID
	if sessionID == "" {
		sessionID = "queue-" + task.ID
	}
	_, out, err := a.runner.Run(a.toolExecutionContext(ctx, sessionID), agent.Request{
		SessionID: sessionID,
		Model:     task.Model,
		Prompt:    task.Prompt,
		Context:   a.conversationContextForSubagent(ctx),
	})
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
		currentStep.ReviewStatus = pendingCheckStatus(current, *currentStep, true)
		currentStep.VerifyStatus = pendingCheckStatus(current, *currentStep, false)
		return nil
	})
	if err != nil {
		return planStepExecutionItem{}, err
	}
	updatedStep, _ := plan.FindStep(updated, step.ID)
	a.logAudit(ctx, "queue_plan_step", "ok", item.ID+":"+step.ID, map[string]any{
		"task_id": task.ID,
		"mode":    "queued",
	})
	return planStepExecutionItem{
		PlanID:       item.ID,
		StepID:       step.ID,
		Title:        step.Title,
		Mode:         string(plan.ExecutionQueued),
		Status:       string(plan.StepStatusQueued),
		Attempts:     updatedStep.Attempts,
		TaskID:       task.ID,
		SessionID:    sessionID,
		ReviewStatus: string(updatedStep.ReviewStatus),
		VerifyStatus: string(updatedStep.VerifyStatus),
		ExecutedAt:   time.Now().UTC(),
		Plan:         updated,
	}, nil
}

func (a *appRuntime) runPlanStepWithSubAgent(ctx context.Context, item plan.Plan, step plan.Step) (planStepExecutionItem, error) {
	sessionID := planStepSessionID(item.ID, step.ID)
	return a.executeManagedPlanStep(ctx, item.ID, step.ID, plan.ExecutionSubAgent, sessionID, "")
}

func (a *appRuntime) runQueuedPlanStep(ctx context.Context, task taskqueue.Task) (string, error) {
	sessionID := task.SessionID
	if sessionID == "" {
		sessionID = planStepSessionID(task.PlanID, task.StepID)
	}
	report, err := a.executeManagedPlanStep(ctx, task.PlanID, task.StepID, plan.ExecutionQueued, sessionID, task.ID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(report.DegradeResult) != "" {
		return report.DegradeResult, nil
	}
	return report.Result, nil
}

func (a *appRuntime) executeManagedPlanStep(ctx context.Context, planID string, stepID string, mode plan.ExecutionMode, sessionID string, taskID string) (planStepExecutionItem, error) {
	item, err := a.plans.Get(planID)
	if err != nil {
		return planStepExecutionItem{}, err
	}
	step, ok := plan.FindStep(item, stepID)
	if !ok {
		return planStepExecutionItem{}, fmt.Errorf("step %q not found in plan %q", stepID, planID)
	}
	maxAttempts := step.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = item.DefaultMaxAttempts
	}
	if maxAttempts <= 0 {
		maxAttempts = 2
	}

	var lastErr error
	var lastOut string
	for attempt := step.Attempts + 1; attempt <= maxAttempts; attempt++ {
		item, step, err = a.markPlanStepAttemptStart(planID, stepID, sessionID, taskID, attempt)
		if err != nil {
			return planStepExecutionItem{}, err
		}
		lastOut, lastErr = a.runSubAgentWithSession(ctx, sessionID, resolvePlanStepModel(a.cfg, step), buildPlanStepPrompt(item, step))
		if lastErr == nil {
			step, item, lastErr = a.verifyAndReviewStep(ctx, item, step, sessionID, lastOut)
			if lastErr == nil {
				updated, err := a.plans.UpdateStep(planID, stepID, func(current *plan.Plan, currentStep *plan.Step) error {
					currentStep.Status = plan.StepStatusSucceeded
					currentStep.Result = lastOut
					currentStep.Error = ""
					currentStep.FinishedAt = time.Now().UTC()
					currentStep.ReviewStatus = step.ReviewStatus
					currentStep.ReviewResult = step.ReviewResult
					currentStep.ReviewSessionID = step.ReviewSessionID
					currentStep.ReviewedAt = step.ReviewedAt
					currentStep.VerifyStatus = step.VerifyStatus
					currentStep.VerifyResult = step.VerifyResult
					currentStep.VerifySessionID = step.VerifySessionID
					currentStep.VerifiedAt = step.VerifiedAt
					return nil
				})
				if err != nil {
					return planStepExecutionItem{}, err
				}
				a.logAudit(ctx, "execute_plan_step", "ok", planID+":"+stepID, map[string]any{
					"mode":          string(mode),
					"attempts":      step.Attempts,
					"review_status": step.ReviewStatus,
					"verify_status": step.VerifyStatus,
				})
				return planStepExecutionItem{
					PlanID:       planID,
					StepID:       stepID,
					Title:        step.Title,
					Mode:         string(mode),
					Status:       string(plan.StepStatusSucceeded),
					Attempts:     step.Attempts,
					TaskID:       taskID,
					SessionID:    sessionID,
					Result:       lastOut,
					ReviewStatus: string(step.ReviewStatus),
					VerifyStatus: string(step.VerifyStatus),
					ExecutedAt:   time.Now().UTC(),
					Plan:         updated,
				}, nil
			}
		}

		if attempt < maxAttempts {
			if _, noteErr := a.plans.UpdateStep(planID, stepID, func(current *plan.Plan, currentStep *plan.Step) error {
				currentStep.Status = plan.StepStatusPlanned
				currentStep.Error = ""
				currentStep.TaskID = taskID
				currentStep.SessionID = sessionID
				currentStep.Notes = append(currentStep.Notes, fmt.Sprintf("attempt %d/%d failed, retrying automatically: %s", attempt, maxAttempts, truncateText(lastErr.Error(), 240)))
				current.Notes = append(current.Notes, fmt.Sprintf("%s retry %d/%d: %s", currentStep.ID, attempt, maxAttempts, truncateText(lastErr.Error(), 200)))
				return nil
			}); noteErr != nil {
				return planStepExecutionItem{}, noteErr
			}
			if step.RetryBackoffSeconds > 0 {
				select {
				case <-ctx.Done():
					return planStepExecutionItem{}, ctx.Err()
				case <-time.After(time.Duration(step.RetryBackoffSeconds) * time.Second):
				}
			}
			continue
		}
	}

	return a.finalizeFailedPlanStep(ctx, planID, stepID, mode, sessionID, taskID, lastOut, lastErr)
}

func pendingCheckStatus(item *plan.Plan, step plan.Step, review bool) plan.CheckStatus {
	if item == nil {
		return plan.CheckStatusSkipped
	}
	if review {
		if step.ReviewRequired || item.AutoReview {
			return plan.CheckStatusPending
		}
		return plan.CheckStatusSkipped
	}
	if step.VerifyRequired || item.AutoVerify {
		return plan.CheckStatusPending
	}
	return plan.CheckStatusSkipped
}

func (a *appRuntime) markPlanStepAttemptStart(planID string, stepID string, sessionID string, taskID string, attempt int) (plan.Plan, plan.Step, error) {
	updated, err := a.plans.UpdateStep(planID, stepID, func(current *plan.Plan, currentStep *plan.Step) error {
		currentStep.Status = plan.StepStatusRunning
		currentStep.SessionID = sessionID
		currentStep.TaskID = taskID
		currentStep.Attempts = attempt
		currentStep.Error = ""
		currentStep.Result = ""
		currentStep.StartedAt = time.Now().UTC()
		currentStep.FinishedAt = time.Time{}
		currentStep.ReviewStatus = pendingCheckStatus(current, *currentStep, true)
		currentStep.ReviewResult = ""
		currentStep.ReviewSessionID = ""
		currentStep.ReviewedAt = time.Time{}
		currentStep.VerifyStatus = pendingCheckStatus(current, *currentStep, false)
		currentStep.VerifyResult = ""
		currentStep.VerifySessionID = ""
		currentStep.VerifiedAt = time.Time{}
		currentStep.RollbackResult = ""
		currentStep.RollbackSessionID = ""
		currentStep.RolledBackAt = time.Time{}
		currentStep.DegradeResult = ""
		currentStep.DegradeSessionID = ""
		currentStep.DegradedAt = time.Time{}
		return nil
	})
	if err != nil {
		return plan.Plan{}, plan.Step{}, err
	}
	step, ok := plan.FindStep(updated, stepID)
	if !ok {
		return plan.Plan{}, plan.Step{}, fmt.Errorf("step %q not found in plan %q after update", stepID, planID)
	}
	return updated, step, nil
}

func (a *appRuntime) verifyAndReviewStep(ctx context.Context, item plan.Plan, step plan.Step, sessionID string, out string) (plan.Step, plan.Plan, error) {
	currentPlan := item
	currentStep := step
	if pendingCheckStatus(&currentPlan, currentStep, false) == plan.CheckStatusPending {
		verifySession := auxiliaryPlanSessionID(item.ID, step.ID, "verify", currentStep.Attempts)
		var err error
		currentPlan, currentStep, err = a.runPlanCheck(
			ctx,
			currentPlan,
			currentStep,
			plan.StepStatusVerifying,
			buildPlanVerifierPrompt(currentPlan, currentStep, out),
			resolvePlanVerifyModel(a.cfg, currentPlan, currentStep),
			verifySession,
			false,
		)
		if err != nil {
			return currentStep, currentPlan, err
		}
	}
	if pendingCheckStatus(&currentPlan, currentStep, true) == plan.CheckStatusPending {
		reviewSession := auxiliaryPlanSessionID(item.ID, step.ID, "review", currentStep.Attempts)
		var err error
		currentPlan, currentStep, err = a.runPlanCheck(
			ctx,
			currentPlan,
			currentStep,
			plan.StepStatusReviewing,
			buildPlanReviewerPrompt(currentPlan, currentStep, out),
			resolvePlanReviewModel(a.cfg, currentPlan, currentStep),
			reviewSession,
			true,
		)
		if err != nil {
			return currentStep, currentPlan, err
		}
	}
	return currentStep, currentPlan, nil
}

func (a *appRuntime) runPlanCheck(ctx context.Context, item plan.Plan, step plan.Step, stage plan.StepStatus, prompt string, modelName string, sessionID string, review bool) (plan.Plan, plan.Step, error) {
	updated, err := a.plans.UpdateStep(item.ID, step.ID, func(current *plan.Plan, currentStep *plan.Step) error {
		currentStep.Status = stage
		if review {
			currentStep.ReviewStatus = plan.CheckStatusPending
			currentStep.ReviewSessionID = sessionID
		} else {
			currentStep.VerifyStatus = plan.CheckStatusPending
			currentStep.VerifySessionID = sessionID
		}
		return nil
	})
	if err != nil {
		return plan.Plan{}, plan.Step{}, err
	}
	step, ok := plan.FindStep(updated, step.ID)
	if !ok {
		return plan.Plan{}, plan.Step{}, fmt.Errorf("step %q not found in plan %q after %s start", step.ID, item.ID, stage)
	}

	raw, runErr := a.runSubAgentWithSession(ctx, sessionID, modelName, prompt)
	verdict := parsePlanCheckVerdict(raw)
	if runErr == nil && verdict.Verdict == "" {
		runErr = fmt.Errorf("%s returned an unreadable verdict", stage)
	}

	now := time.Now().UTC()
	updated, err = a.plans.UpdateStep(item.ID, step.ID, func(current *plan.Plan, currentStep *plan.Step) error {
		currentStep.Status = plan.StepStatusRunning
		if review {
			currentStep.ReviewSessionID = sessionID
			currentStep.ReviewedAt = now
			if runErr != nil {
				currentStep.ReviewStatus = plan.CheckStatusFailed
				currentStep.ReviewResult = runErr.Error()
			} else {
				currentStep.ReviewStatus = verdict.status()
				currentStep.ReviewResult = verdict.display()
			}
		} else {
			currentStep.VerifySessionID = sessionID
			currentStep.VerifiedAt = now
			if runErr != nil {
				currentStep.VerifyStatus = plan.CheckStatusFailed
				currentStep.VerifyResult = runErr.Error()
			} else {
				currentStep.VerifyStatus = verdict.status()
				currentStep.VerifyResult = verdict.display()
			}
		}
		return nil
	})
	if err != nil {
		return plan.Plan{}, plan.Step{}, err
	}
	step, ok = plan.FindStep(updated, step.ID)
	if !ok {
		return plan.Plan{}, plan.Step{}, fmt.Errorf("step %q not found in plan %q after %s completion", step.ID, item.ID, stage)
	}
	if runErr != nil {
		return updated, step, runErr
	}
	if verdict.status() != plan.CheckStatusPassed {
		label := "verification"
		if review {
			label = "review"
		}
		return updated, step, fmt.Errorf("%s failed: %s", label, verdict.summary())
	}
	return updated, step, nil
}

func (a *appRuntime) finalizeFailedPlanStep(ctx context.Context, planID string, stepID string, mode plan.ExecutionMode, sessionID string, taskID string, lastOut string, lastErr error) (planStepExecutionItem, error) {
	item, err := a.plans.Get(planID)
	if err != nil {
		return planStepExecutionItem{}, err
	}
	step, ok := plan.FindStep(item, stepID)
	if !ok {
		return planStepExecutionItem{}, fmt.Errorf("step %q not found in plan %q", stepID, planID)
	}
	failureMessage := "step failed"
	if lastErr != nil && strings.TrimSpace(lastErr.Error()) != "" {
		failureMessage = strings.TrimSpace(lastErr.Error())
	}

	rollbackResult := ""
	if step.FailureStrategy == plan.FailureStrategyRollback || step.FailureStrategy == plan.FailureStrategyRollbackThenDegrade {
		if strings.TrimSpace(step.RollbackPrompt) != "" {
			rollbackSession := auxiliaryPlanSessionID(planID, stepID, "rollback", step.Attempts)
			rollbackResult, err = a.runSubAgentWithSession(ctx, rollbackSession, resolvePlanRecoveryModel(a.cfg, step, step.RollbackModel), buildPlanRollbackPrompt(item, step, lastOut, failureMessage))
			updateErr := a.recordPlanRecovery(planID, stepID, rollbackSession, rollbackResult, err, true)
			if updateErr != nil {
				return planStepExecutionItem{}, updateErr
			}
		}
	}

	degradeResult := ""
	degraded := false
	if step.FailureStrategy == plan.FailureStrategyDegrade || step.FailureStrategy == plan.FailureStrategyRollbackThenDegrade {
		if strings.TrimSpace(step.DegradePrompt) != "" {
			degradeSession := auxiliaryPlanSessionID(planID, stepID, "degrade", step.Attempts)
			degradeResult, err = a.runSubAgentWithSession(ctx, degradeSession, resolvePlanRecoveryModel(a.cfg, step, step.DegradeModel), buildPlanDegradePrompt(item, step, lastOut, failureMessage))
			if err == nil && strings.TrimSpace(degradeResult) != "" {
				degraded = true
			}
			updateErr := a.recordPlanRecovery(planID, stepID, degradeSession, degradeResult, err, false)
			if updateErr != nil {
				return planStepExecutionItem{}, updateErr
			}
		}
	}

	finalStatus := plan.StepStatusFailed
	finalError := failureMessage
	finalResult := lastOut
	if degraded {
		finalStatus = plan.StepStatusDegraded
		finalError = ""
		finalResult = degradeResult
	}

	updated, err := a.plans.UpdateStep(planID, stepID, func(current *plan.Plan, currentStep *plan.Step) error {
		currentStep.Status = finalStatus
		currentStep.Error = finalError
		currentStep.Result = finalResult
		currentStep.TaskID = taskID
		currentStep.SessionID = sessionID
		currentStep.FinishedAt = time.Now().UTC()
		if degraded {
			currentStep.DegradedAt = time.Now().UTC()
			currentStep.Notes = append(currentStep.Notes, "degraded fallback completed")
			current.Notes = append(current.Notes, fmt.Sprintf("%s degraded after failure: %s", currentStep.ID, truncateText(failureMessage, 180)))
		} else {
			currentStep.Notes = append(currentStep.Notes, "step failed after automatic retries")
			current.Notes = append(current.Notes, fmt.Sprintf("%s failed after %d attempts: %s", currentStep.ID, currentStep.Attempts, truncateText(failureMessage, 180)))
		}
		return nil
	})
	if err != nil {
		return planStepExecutionItem{}, err
	}
	finalStep, ok := plan.FindStep(updated, stepID)
	if !ok {
		return planStepExecutionItem{}, fmt.Errorf("step %q not found in plan %q after finalization", stepID, planID)
	}
	a.logAudit(ctx, "execute_plan_step", "ok", planID+":"+stepID, map[string]any{
		"mode":             string(mode),
		"attempts":         finalStep.Attempts,
		"status":           finalStep.Status,
		"review_status":    finalStep.ReviewStatus,
		"verify_status":    finalStep.VerifyStatus,
		"rollback_applied": strings.TrimSpace(finalStep.RollbackResult) != "",
		"degraded":         finalStep.Status == plan.StepStatusDegraded,
	})
	report := planStepExecutionItem{
		PlanID:         planID,
		StepID:         stepID,
		Title:          finalStep.Title,
		Mode:           string(mode),
		Status:         string(finalStep.Status),
		Attempts:       finalStep.Attempts,
		TaskID:         taskID,
		SessionID:      sessionID,
		ReviewStatus:   string(finalStep.ReviewStatus),
		VerifyStatus:   string(finalStep.VerifyStatus),
		RollbackResult: finalStep.RollbackResult,
		DegradeResult:  finalStep.DegradeResult,
		Result:         finalStep.Result,
		Error:          finalStep.Error,
		ExecutedAt:     time.Now().UTC(),
		Plan:           updated,
	}
	if degraded {
		return report, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf(finalError)
	}
	return report, lastErr
}

func (a *appRuntime) recordPlanRecovery(planID string, stepID string, sessionID string, out string, recoveryErr error, rollback bool) error {
	_, err := a.plans.UpdateStep(planID, stepID, func(current *plan.Plan, currentStep *plan.Step) error {
		now := time.Now().UTC()
		if rollback {
			currentStep.RollbackSessionID = sessionID
			currentStep.RolledBackAt = now
			if recoveryErr != nil {
				currentStep.RollbackResult = "rollback failed: " + recoveryErr.Error()
			} else {
				currentStep.RollbackResult = strings.TrimSpace(out)
			}
		} else {
			currentStep.DegradeSessionID = sessionID
			currentStep.DegradedAt = now
			if recoveryErr != nil {
				currentStep.DegradeResult = "degrade failed: " + recoveryErr.Error()
			} else {
				currentStep.DegradeResult = strings.TrimSpace(out)
			}
		}
		return nil
	})
	return err
}

func auxiliaryPlanSessionID(planID string, stepID string, kind string, attempt int) string {
	base := planStepSessionID(planID, stepID)
	if attempt <= 0 {
		return fmt.Sprintf("%s-%s", base, sanitize(kind))
	}
	return fmt.Sprintf("%s-%s-%d", base, sanitize(kind), attempt)
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

func buildPlanVerifierPrompt(item plan.Plan, step plan.Step, executionOutput string) string {
	var b strings.Builder
	b.WriteString("You are the verifier for a durable plan step in Qorvexus.\n")
	b.WriteString("Plan goal: ")
	b.WriteString(strings.TrimSpace(item.Goal))
	b.WriteString("\n")
	b.WriteString("Step: ")
	b.WriteString(strings.TrimSpace(step.Title))
	b.WriteString("\n")
	if details := strings.TrimSpace(step.Details); details != "" {
		b.WriteString("Step details: ")
		b.WriteString(details)
		b.WriteString("\n")
	}
	if instruction := strings.TrimSpace(step.VerifyPrompt); instruction != "" {
		b.WriteString("Verification focus: ")
		b.WriteString(instruction)
		b.WriteString("\n")
	} else {
		b.WriteString("Verification focus: confirm the step result actually satisfies the requested work, dependency expectations, and does not leave a hidden blocker.\n")
	}
	b.WriteString("Execution output:\n")
	b.WriteString(executionOutput)
	b.WriteString("\n")
	b.WriteString("Return JSON only with keys verdict, summary, and issues. verdict must be pass or fail.\n")
	b.WriteString(`Example: {"verdict":"pass","summary":"Result is complete and usable.","issues":[]}`)
	return strings.TrimSpace(b.String())
}

func buildPlanReviewerPrompt(item plan.Plan, step plan.Step, executionOutput string) string {
	var b strings.Builder
	b.WriteString("You are the reviewer for a durable plan step in Qorvexus.\n")
	b.WriteString("Plan goal: ")
	b.WriteString(strings.TrimSpace(item.Goal))
	b.WriteString("\n")
	b.WriteString("Step: ")
	b.WriteString(strings.TrimSpace(step.Title))
	b.WriteString("\n")
	if details := strings.TrimSpace(step.Details); details != "" {
		b.WriteString("Step details: ")
		b.WriteString(details)
		b.WriteString("\n")
	}
	if instruction := strings.TrimSpace(step.ReviewPrompt); instruction != "" {
		b.WriteString("Review focus: ")
		b.WriteString(instruction)
		b.WriteString("\n")
	} else {
		b.WriteString("Review focus: find quality issues, missing follow-through, safety concerns, or regression risk in the step result.\n")
	}
	b.WriteString("Execution output:\n")
	b.WriteString(executionOutput)
	b.WriteString("\n")
	b.WriteString("Return JSON only with keys verdict, summary, and issues. verdict must be pass or fail.\n")
	b.WriteString(`Example: {"verdict":"fail","summary":"The step did not prove the fix works.","issues":["No validation evidence was included."]}`)
	return strings.TrimSpace(b.String())
}

func buildPlanRollbackPrompt(item plan.Plan, step plan.Step, executionOutput string, failureMessage string) string {
	var b strings.Builder
	b.WriteString("A durable plan step failed and needs a rollback.\n")
	b.WriteString("Plan goal: ")
	b.WriteString(strings.TrimSpace(item.Goal))
	b.WriteString("\n")
	b.WriteString("Step: ")
	b.WriteString(strings.TrimSpace(step.Title))
	b.WriteString("\n")
	if details := strings.TrimSpace(step.Details); details != "" {
		b.WriteString("Step details: ")
		b.WriteString(details)
		b.WriteString("\n")
	}
	b.WriteString("Failure: ")
	b.WriteString(strings.TrimSpace(failureMessage))
	b.WriteString("\n")
	if result := strings.TrimSpace(executionOutput); result != "" {
		b.WriteString("Failed execution output:\n")
		b.WriteString(result)
		b.WriteString("\n")
	}
	b.WriteString("Rollback instruction: ")
	b.WriteString(strings.TrimSpace(step.RollbackPrompt))
	b.WriteString("\n")
	b.WriteString("Perform the rollback directly. Return a concise report of what you restored, cleaned up, or disabled.\n")
	return strings.TrimSpace(b.String())
}

func buildPlanDegradePrompt(item plan.Plan, step plan.Step, executionOutput string, failureMessage string) string {
	var b strings.Builder
	b.WriteString("A durable plan step failed and now needs a degraded fallback.\n")
	b.WriteString("Plan goal: ")
	b.WriteString(strings.TrimSpace(item.Goal))
	b.WriteString("\n")
	b.WriteString("Step: ")
	b.WriteString(strings.TrimSpace(step.Title))
	b.WriteString("\n")
	if details := strings.TrimSpace(step.Details); details != "" {
		b.WriteString("Step details: ")
		b.WriteString(details)
		b.WriteString("\n")
	}
	b.WriteString("Failure: ")
	b.WriteString(strings.TrimSpace(failureMessage))
	b.WriteString("\n")
	if result := strings.TrimSpace(executionOutput); result != "" {
		b.WriteString("Failed execution output:\n")
		b.WriteString(result)
		b.WriteString("\n")
	}
	b.WriteString("Fallback instruction: ")
	b.WriteString(strings.TrimSpace(step.DegradePrompt))
	b.WriteString("\n")
	b.WriteString("Produce the best safe fallback you can. Return the degraded result and explain what remains incomplete.\n")
	return strings.TrimSpace(b.String())
}

func resolvePlanStepModel(cfg *config.Config, step plan.Step) string {
	if strings.TrimSpace(step.Model) != "" {
		return step.Model
	}
	return cfg.Agent.DefaultModel
}

func resolvePlanReviewModel(cfg *config.Config, item plan.Plan, step plan.Step) string {
	if strings.TrimSpace(step.ReviewModel) != "" {
		return step.ReviewModel
	}
	if strings.TrimSpace(item.ReviewModel) != "" {
		return item.ReviewModel
	}
	return resolvePlanStepModel(cfg, step)
}

func resolvePlanVerifyModel(cfg *config.Config, item plan.Plan, step plan.Step) string {
	if strings.TrimSpace(step.VerifyModel) != "" {
		return step.VerifyModel
	}
	if strings.TrimSpace(item.VerifyModel) != "" {
		return item.VerifyModel
	}
	return resolvePlanStepModel(cfg, step)
}

func resolvePlanRecoveryModel(cfg *config.Config, step plan.Step, preferred string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
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

func (v planCheckVerdict) status() plan.CheckStatus {
	switch strings.ToLower(strings.TrimSpace(v.Verdict)) {
	case "pass", "passed", "ok", "approve", "approved":
		return plan.CheckStatusPassed
	case "skip", "skipped":
		return plan.CheckStatusSkipped
	case "fail", "failed", "reject", "rejected":
		return plan.CheckStatusFailed
	default:
		return ""
	}
}

func (v planCheckVerdict) summary() string {
	if trimmed := strings.TrimSpace(v.Summary); trimmed != "" {
		return trimmed
	}
	if len(v.Issues) > 0 {
		return strings.TrimSpace(v.Issues[0])
	}
	return truncateText(v.Raw, 180)
}

func (v planCheckVerdict) display() string {
	if strings.TrimSpace(v.Raw) != "" {
		return strings.TrimSpace(v.Raw)
	}
	out := map[string]any{
		"verdict": strings.ToLower(strings.TrimSpace(v.Verdict)),
		"summary": v.Summary,
	}
	if len(v.Issues) > 0 {
		out["issues"] = v.Issues
	}
	raw, _ := json.Marshal(out)
	return string(raw)
}

func parsePlanCheckVerdict(raw string) planCheckVerdict {
	result := planCheckVerdict{Raw: strings.TrimSpace(raw)}
	if result.Raw == "" {
		return result
	}
	if err := json.Unmarshal([]byte(result.Raw), &result); err == nil && result.status() != "" {
		result.Verdict = strings.ToLower(strings.TrimSpace(result.Verdict))
		result.Summary = strings.TrimSpace(result.Summary)
		return result
	}
	lower := strings.ToLower(result.Raw)
	switch {
	case strings.Contains(lower, `"verdict":"pass"`), strings.HasPrefix(lower, "pass"), strings.Contains(lower, " looks good"), strings.Contains(lower, "approved"):
		result.Verdict = "pass"
	case strings.Contains(lower, `"verdict":"fail"`), strings.HasPrefix(lower, "fail"), strings.Contains(lower, "issue"), strings.Contains(lower, "missing"), strings.Contains(lower, "risk"):
		result.Verdict = "fail"
	}
	result.Summary = truncateText(result.Raw, 200)
	return result
}

func parsePlanStepStatus(value string) (plan.StepStatus, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(plan.StepStatusPlanned):
		return plan.StepStatusPlanned, nil
	case string(plan.StepStatusQueued):
		return plan.StepStatusQueued, nil
	case string(plan.StepStatusRunning):
		return plan.StepStatusRunning, nil
	case string(plan.StepStatusVerifying):
		return plan.StepStatusVerifying, nil
	case string(plan.StepStatusReviewing):
		return plan.StepStatusReviewing, nil
	case string(plan.StepStatusSucceeded):
		return plan.StepStatusSucceeded, nil
	case string(plan.StepStatusDegraded):
		return plan.StepStatusDegraded, nil
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

func parseFailureStrategy(value string) (plan.FailureStrategy, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", string(plan.FailureStrategyFail):
		return plan.FailureStrategyFail, nil
	case string(plan.FailureStrategyRollback):
		return plan.FailureStrategyRollback, nil
	case string(plan.FailureStrategyDegrade):
		return plan.FailureStrategyDegrade, nil
	case string(plan.FailureStrategyRollbackThenDegrade):
		return plan.FailureStrategyRollbackThenDegrade, nil
	default:
		return "", fmt.Errorf("unsupported failure strategy %q", value)
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
		RuntimeMode:              a.runtimeMode(),
		RuntimeApplyEnabled:      a.runtimeApplyEnabled(),
		ExecutablePath:           a.executablePath,
		SourceRoot:               a.sourceRoot,
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
	trimmed := strings.TrimSpace(text)
	if social.IsSilentReply(trimmed) {
		return agent.ToolResultJSON(map[string]any{
			"mode":   "silent",
			"reason": "the caller intentionally chose not to send a message",
		}), nil
	}
	out, err := a.connectors.Send(ctx, channel, social.OutboundMessage{
		Channel:   channel,
		ThreadID:  threadID,
		Recipient: recipient,
		Text:      trimmed,
		Context: types.ConversationContext{
			Channel:      channel,
			Trust:        types.TrustOwner,
			IsOwner:      true,
			ReplyAsAgent: true,
		},
	})
	if err == nil {
		a.recordSocialGraphInteraction(social.Interaction{
			Kind:        social.InteractionOutbound,
			Channel:     channel,
			ThreadID:    threadID,
			ContactID:   recipient,
			ContactName: recipient,
			Trust:       types.TrustOwner,
			Message:     trimmed,
			OccurredAt:  time.Now().UTC(),
		})
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

func (a *appRuntime) HoldSocialMessage(ctx context.Context, channel string, threadID string, recipient string, text string, reason string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("holding outbound social messages requires owner context")
	}
	draft, err := a.createSocialDraft(ctx, social.Draft{
		Channel:   channel,
		ThreadID:  threadID,
		Recipient: recipient,
		Text:      text,
		Reason:    reason,
		Source:    "owner:hold_social_message",
		Hold:      true,
		Context: types.ConversationContext{
			Channel:      channel,
			ThreadID:     threadID,
			Trust:        types.TrustOwner,
			IsOwner:      true,
			ReplyAsAgent: true,
		},
	})
	if err != nil {
		return "", err
	}
	return agent.ToolResultJSON(draft), nil
}

func (a *appRuntime) ListSocialOutbox(ctx context.Context, limit int, status string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("listing social outbox entries requires owner context")
	}
	if limit <= 0 {
		limit = 50
	}
	items, err := a.socialDrafts.List(limit, status)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *appRuntime) ManageSocialOutbox(ctx context.Context, outboxID string, action string, editedText string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("managing social outbox entries requires owner context")
	}
	action = strings.TrimSpace(strings.ToLower(action))
	if action == "" {
		action = "send"
	}
	actor := socialActor(ctx)
	draft, err := a.socialDrafts.Update(outboxID, func(item *social.Draft) error {
		if strings.TrimSpace(editedText) != "" {
			item.Text = strings.TrimSpace(editedText)
		}
		switch action {
		case "send":
			item.Status = social.DraftStatusReady
			item.ReviewedBy = actor
			item.ReviewedAt = time.Now().UTC()
		case "hold":
			item.Status = social.DraftStatusHeld
			item.Hold = true
			item.ReviewedBy = actor
			item.ReviewedAt = time.Now().UTC()
		case "discard":
			item.Status = social.DraftStatusDiscarded
			item.ReviewedBy = actor
			item.ReviewedAt = time.Now().UTC()
			item.DiscardedAt = time.Now().UTC()
		default:
			return fmt.Errorf("unsupported outbox action %q", action)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if action == "discard" {
		a.logAudit(ctx, "discard_social_outbox", "ok", draft.ID, map[string]any{"channel": draft.Channel, "recipient": draft.Recipient})
		if draft.RelatedFollowUpID != "" {
			_, _ = a.socialFollowUps.Update(draft.RelatedFollowUpID, func(item *social.FollowUp) error {
				item.Status = social.FollowUpStatusDismissed
				item.LastActionAt = time.Now().UTC()
				return nil
			})
		}
		return agent.ToolResultJSON(draft), nil
	}
	if action == "hold" {
		a.logAudit(ctx, "update_social_outbox", "ok", draft.ID, map[string]any{"channel": draft.Channel, "recipient": draft.Recipient, "send": false})
		if draft.RelatedFollowUpID != "" {
			_, _ = a.socialFollowUps.Update(draft.RelatedFollowUpID, func(item *social.FollowUp) error {
				item.Status = social.FollowUpStatusHeld
				item.RelatedOutboxID = draft.ID
				item.LastActionAt = time.Now().UTC()
				return nil
			})
		}
		return agent.ToolResultJSON(draft), nil
	}
	delivery, err := a.connectors.Send(ctx, draft.Channel, social.OutboundMessage{
		Channel:   draft.Channel,
		ThreadID:  draft.ThreadID,
		Recipient: draft.Recipient,
		Text:      draft.Text,
		Context:   draft.Context,
	})
	if err != nil {
		return "", err
	}
	draft, err = a.socialDrafts.Update(draft.ID, func(item *social.Draft) error {
		item.Status = social.DraftStatusSent
		item.SentAt = time.Now().UTC()
		item.DeliveryResult = delivery
		return nil
	})
	if err != nil {
		return "", err
	}
	a.recordSocialGraphInteraction(social.Interaction{
		Kind:        social.InteractionOutbound,
		Channel:     draft.Channel,
		ThreadID:    draft.ThreadID,
		ContactID:   draft.Recipient,
		ContactName: draft.Counterparty,
		Trust:       draft.Context.Trust,
		Message:     draft.Text,
		OccurredAt:  time.Now().UTC(),
	})
	if draft.RelatedFollowUpID != "" {
		_, _ = a.socialFollowUps.Update(draft.RelatedFollowUpID, func(item *social.FollowUp) error {
			item.Status = social.FollowUpStatusSent
			item.RelatedOutboxID = draft.ID
			item.LastActionAt = time.Now().UTC()
			return nil
		})
	}
	a.logAudit(ctx, "send_social_outbox", "ok", draft.ID, map[string]any{"channel": draft.Channel, "recipient": draft.Recipient, "send": true})
	return agent.ToolResultJSON(map[string]any{
		"outbox":   draft,
		"delivery": delivery,
	}), nil
}

func (a *appRuntime) SocialContactGraph(ctx context.Context) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("reading the social contact graph requires owner context")
	}
	snapshot, err := a.socialGraph.Snapshot()
	if err != nil {
		return "", err
	}
	openCommitments := map[string]int{}
	if items, err := a.commitments.List(0); err == nil {
		for _, item := range items {
			if item.Status != commitment.StatusOpen && item.Status != commitment.StatusOverdue {
				continue
			}
			key := strings.TrimSpace(item.ContactKey)
			if key == "" {
				key = social.ContactKey(item.Channel, "", item.Counterparty)
			}
			if key != "" {
				openCommitments[key]++
			}
		}
	}
	heldOutbox := map[string]int{}
	if items, err := a.socialDrafts.List(0, ""); err == nil {
		for _, item := range items {
			if item.Status == social.DraftStatusSent || item.Status == social.DraftStatusDiscarded {
				continue
			}
			if item.ContactKey != "" {
				heldOutbox[item.ContactKey]++
			}
		}
	}
	openFollowUps := map[string]int{}
	if items, err := a.socialFollowUps.List(0, ""); err == nil {
		for _, item := range items {
			if item.Status == social.FollowUpStatusCompleted || item.Status == social.FollowUpStatusDismissed {
				continue
			}
			if item.ContactKey != "" {
				openFollowUps[item.ContactKey]++
			}
		}
	}
	type nodeView struct {
		social.ContactNode
		OpenCommitments int `json:"open_commitments,omitempty"`
		HeldOutbox      int `json:"held_outbox,omitempty"`
		OpenFollowUps   int `json:"open_followups,omitempty"`
		ContactCard     any `json:"contact_card,omitempty"`
	}
	nodes := make([]nodeView, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		var card any
		if a.memory != nil && a.cfg.Memory.Enabled {
			identity := memory.ResolveContactIdentity(types.ConversationContext{
				Channel:    node.Channel,
				SenderID:   node.ContactID,
				SenderName: node.DisplayName,
				Trust:      types.TrustLevel(node.Trust),
			}, "")
			items := []memory.Entry{}
			if identity.CanonicalSubject != "" {
				if subjectItems, err := a.memory.SearchWithOptions(memory.SearchOptions{
					Layers:           []string{"people"},
					Areas:            []string{"contacts"},
					Subjects:         []string{identity.CanonicalSubject},
					Limit:            20,
					IncludeSummaries: true,
				}); err == nil {
					items = append(items, subjectItems...)
				}
			}
			if identity.RouteKey != "" {
				if routeItems, err := a.memory.SearchWithOptions(memory.SearchOptions{
					Layers:           []string{"people"},
					Areas:            []string{"contacts"},
					Tags:             []string{"contact_route:" + identity.RouteKey},
					Limit:            12,
					IncludeSummaries: true,
				}); err == nil {
					items = append(items, routeItems...)
				}
			}
			if len(items) > 0 {
				card = memory.BuildContactCard(items)
			}
		}
		nodes = append(nodes, nodeView{
			ContactNode:     node,
			OpenCommitments: openCommitments[node.ID],
			HeldOutbox:      heldOutbox[node.ID],
			OpenFollowUps:   openFollowUps[node.ID],
			ContactCard:     card,
		})
	}
	return agent.ToolResultJSON(map[string]any{
		"updated_at": snapshot.UpdatedAt,
		"nodes":      nodes,
		"edges":      snapshot.Edges,
	}), nil
}

func (a *appRuntime) ListSocialFollowUps(ctx context.Context, limit int, status string) (string, error) {
	if !ownerAllowedFromContext(ctx) {
		return "", fmt.Errorf("listing social follow-ups requires owner context")
	}
	if limit <= 0 {
		limit = 50
	}
	items, err := a.socialFollowUps.List(limit, status)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
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
		SentReminders   []string `json:"sent_reminders"`
		HeldReminders   []string `json:"held_reminders"`
	}
	out := scanSummary{Checked: len(items)}
	reminderCooldown := time.Duration(a.cfg.Social.ReminderCooldownHours) * time.Hour
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
		if !commitment.ShouldCreateReminder(entry, now, reminderCooldown) {
			continue
		}
		delivery, err := a.issueCommitmentReminder(ctx, entry, now)
		if err != nil || delivery.Mode == "" || delivery.Mode == string(social.DeliveryModeSilent) {
			continue
		}
		refID := strings.TrimSpace(delivery.OutboxID)
		if refID == "" && delivery.Mode == string(social.DeliveryModeSend) {
			refID = "direct-send"
		}
		_ = a.commitments.NoteReminderIssued(entry.ID, refID, delivery.Mode, now)
		switch delivery.Mode {
		case string(social.DeliveryModeSend):
			out.SentReminders = append(out.SentReminders, entry.ID)
		case string(social.DeliveryModeHold):
			out.HeldReminders = append(out.HeldReminders, entry.ID)
		}
	}
	for _, entry := range scan.Stale {
		if !commitment.ShouldQueueReview(entry, now) {
			// still allow autonomous reminders for stale commitments even when a review was recently queued
		} else {
			level := commitment.NextEscalationLevel(entry, now)
			taskID, err := a.enqueueCommitmentReview(ctx, entry, level, "This commitment is getting stale. Review whether it needs a reminder, internal preparation, or a concrete next-step task before it becomes overdue.")
			if err == nil && taskID != "" {
				out.QueuedReview = append(out.QueuedReview, taskID)
				if level > entry.EscalationLevel {
					out.EscalatedReview = append(out.EscalatedReview, entry.ID)
				}
				_ = a.commitments.NoteReviewQueued(entry.ID, taskID, level, now)
			}
		}
		if !commitment.ShouldCreateReminder(entry, now, reminderCooldown) {
			continue
		}
		delivery, err := a.issueCommitmentReminder(ctx, entry, now)
		if err != nil || delivery.Mode == "" || delivery.Mode == string(social.DeliveryModeSilent) {
			continue
		}
		refID := strings.TrimSpace(delivery.OutboxID)
		if refID == "" && delivery.Mode == string(social.DeliveryModeSend) {
			refID = "direct-send"
		}
		_ = a.commitments.NoteReminderIssued(entry.ID, refID, delivery.Mode, now)
		switch delivery.Mode {
		case string(social.DeliveryModeSend):
			out.SentReminders = append(out.SentReminders, entry.ID)
		case string(social.DeliveryModeHold):
			out.HeldReminders = append(out.HeldReminders, entry.ID)
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
	a.syncCommitmentFollowUps(id, commitment.Status(status))
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

type socialDeliveryResult struct {
	Mode           string `json:"mode"`
	Boundary       string `json:"boundary,omitempty"`
	Reason         string `json:"reason,omitempty"`
	OutboxID       string `json:"outbox_id,omitempty"`
	DeliveryResult string `json:"delivery_result,omitempty"`
	HighRisk       bool   `json:"high_risk,omitempty"`
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
	a.recordSocialGraphInteraction(social.Interaction{
		Kind:        social.InteractionInbound,
		Channel:     env.Channel,
		ThreadID:    env.ThreadID,
		ContactID:   env.SenderID,
		ContactName: env.SenderName,
		Trust:       env.Context.Trust,
		Message:     env.Text,
		OccurredAt:  time.Now().UTC(),
	})
	var parts []types.ContentPart
	for _, image := range env.Images {
		parts = append(parts, types.ContentPart{Type: "image_url", ImageURL: image})
	}
	_, out, err := a.runner.Run(toolCtx, agent.Request{
		SessionID: sessionID,
		Prompt:    env.Text,
		Parts:     parts,
		Context:   &env.Context,
	})
	if err == nil {
		delivery, routeErr := a.routeSocialReply(toolCtx, env, out)
		if routeErr != nil {
			return out, routeErr
		}
		replyText := out
		if delivery.Mode == string(social.DeliveryModeSilent) {
			replyText = ""
		}
		a.logAudit(toolCtx, "reply_social_message", "ok", sessionID, map[string]any{
			"channel":   env.Channel,
			"thread_id": env.ThreadID,
			"mode":      delivery.Mode,
			"outbox_id": delivery.OutboxID,
		})
		a.captureSocialInsights(toolCtx, env, replyText, delivery)
		out = replyText
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
		Layers: []string{"owner"},
		Limit:  limit,
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

func (a *appRuntime) captureSocialInsights(ctx context.Context, env social.Envelope, response string, delivery socialDeliveryResult) {
	if a.insights == nil {
		return
	}
	result := a.insights.Analyze(env, response)
	var followUpTaskID string
	contactKey := social.ContactKey(env.Channel, env.SenderID, env.SenderName)
	identity := memory.ResolveContactIdentity(env.Context, env.Text)
	contactSubject := identity.CanonicalSubject
	if contactSubject == "" {
		contactSubject = contactKey
	}
	contactName := socialCounterpartyName(env)
	for _, note := range result.Memories {
		if a.cfg.Memory.Enabled {
			if err := a.memory.Upsert(memory.Entry{
				Key:        "person:" + contactSubject + ":social_note:" + memory.HashKey(note.Content),
				Layer:      "people",
				Area:       "contacts",
				Kind:       "interaction_note",
				Subject:    contactSubject,
				Summary:    note.Content,
				Content:    note.Content,
				Source:     note.Source,
				Tags:       append(memory.ContactIdentityTags(identity, env.Context), note.Tags...),
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
	if a.cfg.Memory.Enabled && strings.TrimSpace(contactSubject) != "" {
		_ = a.memory.RefreshContactCard(contactSubject)
	}
	for _, suggestion := range result.FollowUps {
		followUp, err := a.socialFollowUps.Upsert(social.FollowUp{
			Channel:           env.Channel,
			ThreadID:          env.ThreadID,
			ContactKey:        contactKey,
			ContactName:       contactName,
			Trust:             string(env.Context.Trust),
			Summary:           suggestion.Summary,
			RecommendedAction: suggestion.RecommendedAction,
			Reason:            suggestion.Reason,
			DueHint:           suggestion.DueHint,
			Priority:          suggestion.Priority,
			Disposition:       suggestion.Disposition,
		})
		if err == nil && followUpTaskID != "" {
			_, _ = a.socialFollowUps.Update(followUp.ID, func(item *social.FollowUp) error {
				item.RelatedTaskID = followUpTaskID
				item.Status = social.FollowUpStatusQueued
				item.LastActionAt = time.Now().UTC()
				return nil
			})
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
	if delivery.OutboxID != "" {
		_, _ = a.socialFollowUps.Upsert(social.FollowUp{
			Channel:           env.Channel,
			ThreadID:          env.ThreadID,
			ContactKey:        contactKey,
			ContactName:       contactName,
			Trust:             string(env.Context.Trust),
			Summary:           "Review held outbound message",
			RecommendedAction: "review_held_outbox",
			Reason:            delivery.Reason,
			Priority:          socialPriorityFromDelivery(delivery),
			Disposition:       "internal_prep",
			Status:            social.FollowUpStatusHeld,
			RelatedOutboxID:   delivery.OutboxID,
			LastActionAt:      time.Now().UTC(),
		})
	}
	for _, suggestion := range result.Commitments {
		relatedTaskID := followUpTaskID
		if a.cfg.Queue.Enabled && (suggestion.DueHint != "" || relatedTaskID == "") {
			taskID, err := a.enqueueCommitmentReview(ctx, commitment.Entry{
				Channel:      env.Channel,
				ThreadID:     env.ThreadID,
				ContactKey:   contactKey,
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
			ContactKey:    contactKey,
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
			_, _ = a.socialFollowUps.Upsert(social.FollowUp{
				Channel:             entry.Channel,
				ThreadID:            entry.ThreadID,
				ContactKey:          entry.ContactKey,
				ContactName:         entry.Counterparty,
				Trust:               entry.Trust,
				Summary:             entry.Summary,
				RecommendedAction:   "fulfill_commitment",
				Reason:              "A social exchange created a commitment that now needs explicit follow-through.",
				DueHint:             entry.DueHint,
				Priority:            socialPriorityFromDueHint(entry.DueHint),
				Disposition:         "autonomous_send",
				RelatedCommitmentID: entry.ID,
				RelatedTaskID:       relatedTaskID,
			})
		}
	}
}

func (a *appRuntime) routeSocialReply(ctx context.Context, env social.Envelope, out string) (socialDeliveryResult, error) {
	if social.IsSilentReply(out) {
		return socialDeliveryResult{Mode: "silent"}, nil
	}
	contactKey := social.ContactKey(env.Channel, env.SenderID, env.SenderName)
	node, ok, err := a.socialGraph.Get(contactKey)
	if err != nil {
		return socialDeliveryResult{}, err
	}
	if !ok {
		node = social.ContactNode{
			ID:               contactKey,
			Channel:          env.Channel,
			ContactID:        env.SenderID,
			DisplayName:      env.SenderName,
			Trust:            string(env.Context.Trust),
			Boundary:         social.DefaultBoundary(env.Context.Trust, 0),
			InteractionCount: 0,
		}
	}
	decision := social.DecideOutboundAuthorization(env, out, node, derefBool(a.cfg.Social.AutoSendTrustedReplies), derefBool(a.cfg.Social.AutoSendExternalReplies))
	if decision.Mode == social.DeliveryModeSilent {
		return socialDeliveryResult{
			Mode:     string(social.DeliveryModeSilent),
			Boundary: string(decision.Boundary),
			Reason:   decision.Reason,
			HighRisk: decision.HighRisk,
		}, nil
	}
	if decision.Mode == social.DeliveryModeHold {
		draft, err := a.createSocialDraft(ctx, social.Draft{
			Channel:      env.Channel,
			ThreadID:     env.ThreadID,
			Recipient:    env.SenderID,
			ContactKey:   contactKey,
			Counterparty: socialCounterpartyName(env),
			Text:         out,
			Reason:       decision.Reason,
			Source:       "social:auto_hold",
			Boundary:     string(decision.Boundary),
			Hold:         true,
			Context:      env.Context,
		})
		if err != nil {
			return socialDeliveryResult{}, err
		}
		return socialDeliveryResult{
			Mode:     string(social.DeliveryModeHold),
			Boundary: string(decision.Boundary),
			Reason:   decision.Reason,
			OutboxID: draft.ID,
			HighRisk: decision.HighRisk,
		}, nil
	}
	delivery, err := a.connectors.Send(ctx, env.Channel, social.OutboundMessage{
		Channel:   env.Channel,
		ThreadID:  env.ThreadID,
		Recipient: env.SenderID,
		Text:      out,
		Context:   env.Context,
	})
	if err != nil {
		a.logAudit(ctx, "deliver_social_reply", "error", env.Channel+"-"+env.ThreadID, map[string]any{
			"channel":   env.Channel,
			"thread_id": env.ThreadID,
			"error":     err.Error(),
		})
		return socialDeliveryResult{}, err
	}
	a.recordSocialGraphInteraction(social.Interaction{
		Kind:        social.InteractionOutbound,
		Channel:     env.Channel,
		ThreadID:    env.ThreadID,
		ContactID:   env.SenderID,
		ContactName: env.SenderName,
		Trust:       env.Context.Trust,
		Message:     out,
		OccurredAt:  time.Now().UTC(),
	})
	a.logAudit(ctx, "deliver_social_reply", "ok", env.Channel+"-"+env.ThreadID, map[string]any{
		"channel":   env.Channel,
		"thread_id": env.ThreadID,
	})
	return socialDeliveryResult{
		Mode:           string(social.DeliveryModeSend),
		Boundary:       string(decision.Boundary),
		Reason:         decision.Reason,
		DeliveryResult: delivery,
		HighRisk:       decision.HighRisk,
	}, nil
}

func (a *appRuntime) createSocialDraft(ctx context.Context, draft social.Draft) (social.Draft, error) {
	if strings.TrimSpace(draft.ContactKey) == "" {
		draft.ContactKey = social.ContactKey(draft.Channel, draft.Recipient, draft.Counterparty)
	}
	if strings.TrimSpace(draft.Counterparty) == "" {
		draft.Counterparty = chooseSocialCounterparty(draft.Recipient, draft.ContactKey)
	}
	created, err := a.socialDrafts.Append(draft)
	if err != nil {
		return social.Draft{}, err
	}
	a.recordSocialGraphInteraction(social.Interaction{
		Kind:        social.InteractionOutbox,
		Channel:     created.Channel,
		ThreadID:    created.ThreadID,
		ContactID:   created.Recipient,
		ContactName: created.Counterparty,
		Trust:       created.Context.Trust,
		Message:     created.Text,
		OccurredAt:  created.CreatedAt,
	})
	if created.RelatedFollowUpID != "" {
		_, _ = a.socialFollowUps.Update(created.RelatedFollowUpID, func(item *social.FollowUp) error {
			item.Status = social.FollowUpStatusHeld
			item.RelatedOutboxID = created.ID
			item.LastActionAt = time.Now().UTC()
			return nil
		})
	}
	a.logAudit(ctx, "create_social_outbox", "ok", created.ID, map[string]any{
		"channel":   created.Channel,
		"thread_id": created.ThreadID,
		"recipient": created.Recipient,
		"reason":    created.Reason,
	})
	return created, nil
}

func (a *appRuntime) recordSocialGraphInteraction(interaction social.Interaction) {
	if a.socialGraph == nil {
		return
	}
	_, _ = a.socialGraph.RecordInteraction(interaction)
}

func socialActor(ctx context.Context) string {
	if convo, ok := tool.ConversationContextFrom(ctx); ok {
		if strings.TrimSpace(convo.SenderID) != "" {
			return strings.TrimSpace(convo.SenderID)
		}
		if strings.TrimSpace(convo.SenderName) != "" {
			return strings.TrimSpace(convo.SenderName)
		}
	}
	return "owner"
}

func socialPriorityFromDelivery(delivery socialDeliveryResult) string {
	if delivery.HighRisk {
		return "high"
	}
	return "medium"
}

func socialPriorityFromDueHint(dueHint string) string {
	switch strings.ToLower(strings.TrimSpace(dueHint)) {
	case "today", "tomorrow":
		return "high"
	case "this week", "next week":
		return "medium"
	default:
		return "low"
	}
}

func socialCounterpartyName(env social.Envelope) string {
	if strings.TrimSpace(env.SenderName) != "" {
		return strings.TrimSpace(env.SenderName)
	}
	if strings.TrimSpace(env.SenderID) != "" {
		return strings.TrimSpace(env.SenderID)
	}
	return "unknown contact"
}

func chooseSocialCounterparty(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func derefBool(value *bool) bool {
	return value != nil && *value
}

func (a *appRuntime) issueCommitmentReminder(ctx context.Context, entry commitment.Entry, now time.Time) (socialDeliveryResult, error) {
	if strings.TrimSpace(entry.Channel) == "" {
		return socialDeliveryResult{Mode: string(social.DeliveryModeSilent), Reason: "commitment has no channel routing"}, nil
	}
	recipient := socialRecipientFromCommitment(entry)
	contactKey := strings.TrimSpace(entry.ContactKey)
	if contactKey == "" {
		contactKey = social.ContactKey(entry.Channel, recipient, entry.Counterparty)
	}
	replyText := buildCommitmentReminderText(entry)
	if social.IsSilentReply(replyText) {
		return socialDeliveryResult{Mode: string(social.DeliveryModeSilent), Reason: "reminder text is empty"}, nil
	}
	convo := types.ConversationContext{
		Channel:        entry.Channel,
		ThreadID:       entry.ThreadID,
		SenderID:       recipient,
		SenderName:     entry.Counterparty,
		Trust:          parseTrustLevel(entry.Trust),
		ReplyAsAgent:   true,
		WorkingForUser: true,
	}
	if strings.TrimSpace(recipient) == "" && strings.TrimSpace(entry.ThreadID) == "" {
		draft, err := a.createSocialDraft(tool.WithConversationContext(ctx, convo), social.Draft{
			Channel:             entry.Channel,
			ThreadID:            entry.ThreadID,
			Recipient:           recipient,
			ContactKey:          contactKey,
			Counterparty:        entry.Counterparty,
			Text:                replyText,
			Reason:              "commitment reminder is ready but lacks routing information",
			Source:              "commitment:reminder",
			Boundary:            "routing_hold",
			Hold:                true,
			Context:             convo,
			RelatedCommitmentID: entry.ID,
		})
		if err != nil {
			return socialDeliveryResult{}, err
		}
		a.upsertCommitmentFollowUp(entry, social.FollowUpStatusHeld, "internal_prep", draft.ID, "")
		return socialDeliveryResult{
			Mode:     string(social.DeliveryModeHold),
			Reason:   "commitment reminder is held until routing information is available",
			OutboxID: draft.ID,
		}, nil
	}
	deliveryCtx := tool.WithConversationContext(ctx, convo)
	delivery, err := a.connectors.Send(deliveryCtx, entry.Channel, social.OutboundMessage{
		Channel:   entry.Channel,
		ThreadID:  entry.ThreadID,
		Recipient: recipient,
		Text:      replyText,
		Context:   convo,
	})
	if err != nil {
		draft, holdErr := a.createSocialDraft(deliveryCtx, social.Draft{
			Channel:             entry.Channel,
			ThreadID:            entry.ThreadID,
			Recipient:           recipient,
			ContactKey:          contactKey,
			Counterparty:        entry.Counterparty,
			Text:                replyText,
			Reason:              "delivery failed; reminder moved to outbox: " + err.Error(),
			Source:              "commitment:reminder_delivery_failure",
			Boundary:            "delivery_failure_hold",
			Hold:                true,
			Context:             convo,
			RelatedCommitmentID: entry.ID,
		})
		if holdErr != nil {
			return socialDeliveryResult{}, err
		}
		a.upsertCommitmentFollowUp(entry, social.FollowUpStatusHeld, "internal_prep", draft.ID, "")
		return socialDeliveryResult{
			Mode:     string(social.DeliveryModeHold),
			Reason:   "delivery failed and the reminder was moved to outbox",
			OutboxID: draft.ID,
		}, nil
	}
	a.recordSocialGraphInteraction(social.Interaction{
		Kind:        social.InteractionOutbound,
		Channel:     entry.Channel,
		ThreadID:    entry.ThreadID,
		ContactID:   recipient,
		ContactName: entry.Counterparty,
		Trust:       parseTrustLevel(entry.Trust),
		Message:     replyText,
		OccurredAt:  now,
	})
	a.upsertCommitmentFollowUp(entry, social.FollowUpStatusSent, "autonomous_send", "", "direct-send")
	a.logAudit(deliveryCtx, "send_commitment_reminder", "ok", entry.ID, map[string]any{
		"channel":      entry.Channel,
		"thread_id":    entry.ThreadID,
		"counterparty": entry.Counterparty,
	})
	return socialDeliveryResult{
		Mode:           string(social.DeliveryModeSend),
		Reason:         "assistant autonomously sent a commitment reminder",
		DeliveryResult: delivery,
	}, nil
}

func (a *appRuntime) upsertCommitmentFollowUp(entry commitment.Entry, status social.FollowUpStatus, disposition string, outboxID string, taskID string) {
	if a.socialFollowUps == nil {
		return
	}
	_, _ = a.socialFollowUps.Upsert(social.FollowUp{
		Channel:             entry.Channel,
		ThreadID:            entry.ThreadID,
		ContactKey:          entry.ContactKey,
		ContactName:         entry.Counterparty,
		Trust:               entry.Trust,
		Summary:             entry.Summary,
		RecommendedAction:   "follow_up_on_commitment",
		Reason:              "An open commitment needs a proactive reminder or next step.",
		DueHint:             entry.DueHint,
		Priority:            socialPriorityFromDueHint(entry.DueHint),
		Disposition:         disposition,
		Status:              status,
		RelatedCommitmentID: entry.ID,
		RelatedOutboxID:     outboxID,
		RelatedTaskID:       taskID,
		LastActionAt:        time.Now().UTC(),
	})
}

func (a *appRuntime) syncCommitmentFollowUps(commitmentID string, status commitment.Status) {
	items, err := a.socialFollowUps.List(0, "")
	if err != nil {
		return
	}
	for _, item := range items {
		if item.RelatedCommitmentID != commitmentID {
			continue
		}
		_, _ = a.socialFollowUps.Update(item.ID, func(existing *social.FollowUp) error {
			switch status {
			case commitment.StatusCompleted:
				existing.Status = social.FollowUpStatusCompleted
			case commitment.StatusCanceled:
				existing.Status = social.FollowUpStatusDismissed
			default:
				return nil
			}
			existing.LastActionAt = time.Now().UTC()
			return nil
		})
	}
}

func socialRecipientFromCommitment(entry commitment.Entry) string {
	contactKey := strings.TrimSpace(entry.ContactKey)
	if idx := strings.Index(contactKey, ":"); idx >= 0 && idx+1 < len(contactKey) {
		return strings.TrimSpace(contactKey[idx+1:])
	}
	if strings.TrimSpace(entry.Counterparty) != "" {
		return strings.TrimSpace(entry.Counterparty)
	}
	return ""
}

func buildCommitmentReminderText(entry commitment.Entry) string {
	summary := strings.TrimSpace(entry.Summary)
	if summary == "" {
		summary = "our earlier conversation"
	}
	var b strings.Builder
	b.WriteString("Quick follow-up on ")
	b.WriteString(summary)
	b.WriteString(".")
	switch strings.ToLower(strings.TrimSpace(entry.DueHint)) {
	case "today", "tomorrow":
		b.WriteString(" I want to keep this moving and check whether the timing still works on your side.")
	case "this week", "next week":
		b.WriteString(" Let me know what timing works best on your side.")
	default:
		b.WriteString(" Let me know the best next step or timing from your side.")
	}
	return strings.TrimSpace(b.String())
}

func parseTrustLevel(raw string) types.TrustLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(types.TrustOwner):
		return types.TrustOwner
	case string(types.TrustTrusted):
		return types.TrustTrusted
	case string(types.TrustSystem):
		return types.TrustSystem
	default:
		return types.TrustExternal
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
				"Decide whether to prepare a deliverable, send a follow-up, stay silent for now, or queue further work. Respect delegated authority boundaries and keep the commitment moving.",
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
