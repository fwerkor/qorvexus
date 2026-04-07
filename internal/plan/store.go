package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusActive    Status = "active"
	StatusBlocked   Status = "blocked"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type StepStatus string

const (
	StepStatusPlanned   StepStatus = "planned"
	StepStatusQueued    StepStatus = "queued"
	StepStatusRunning   StepStatus = "running"
	StepStatusVerifying StepStatus = "verifying"
	StepStatusReviewing StepStatus = "reviewing"
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusDegraded  StepStatus = "degraded"
	StepStatusFailed    StepStatus = "failed"
	StepStatusBlocked   StepStatus = "blocked"
	StepStatusCancelled StepStatus = "cancelled"
)

type ExecutionMode string

const (
	ExecutionSubAgent ExecutionMode = "subagent"
	ExecutionQueued   ExecutionMode = "queued"
)

type CheckStatus string

const (
	CheckStatusPending CheckStatus = "pending"
	CheckStatusPassed  CheckStatus = "passed"
	CheckStatusFailed  CheckStatus = "failed"
	CheckStatusSkipped CheckStatus = "skipped"
)

type FailureStrategy string

const (
	FailureStrategyFail                FailureStrategy = "fail"
	FailureStrategyRollback            FailureStrategy = "rollback"
	FailureStrategyDegrade             FailureStrategy = "degrade"
	FailureStrategyRollbackThenDegrade FailureStrategy = "rollback_then_degrade"
)

type Step struct {
	ID                  string          `json:"id"`
	Title               string          `json:"title"`
	Details             string          `json:"details,omitempty"`
	Prompt              string          `json:"prompt,omitempty"`
	Model               string          `json:"model,omitempty"`
	DependsOn           []string        `json:"depends_on,omitempty"`
	ExecutionMode       ExecutionMode   `json:"execution_mode,omitempty"`
	Status              StepStatus      `json:"status"`
	SessionID           string          `json:"session_id,omitempty"`
	TaskID              string          `json:"task_id,omitempty"`
	Notes               []string        `json:"notes,omitempty"`
	Result              string          `json:"result,omitempty"`
	Error               string          `json:"error,omitempty"`
	Attempts            int             `json:"attempts,omitempty"`
	MaxAttempts         int             `json:"max_attempts,omitempty"`
	RetryBackoffSeconds int             `json:"retry_backoff_seconds,omitempty"`
	ReviewRequired      bool            `json:"review_required,omitempty"`
	ReviewPrompt        string          `json:"review_prompt,omitempty"`
	ReviewModel         string          `json:"review_model,omitempty"`
	ReviewStatus        CheckStatus     `json:"review_status,omitempty"`
	ReviewResult        string          `json:"review_result,omitempty"`
	ReviewSessionID     string          `json:"review_session_id,omitempty"`
	ReviewedAt          time.Time       `json:"reviewed_at,omitempty"`
	VerifyRequired      bool            `json:"verify_required,omitempty"`
	VerifyPrompt        string          `json:"verify_prompt,omitempty"`
	VerifyModel         string          `json:"verify_model,omitempty"`
	VerifyStatus        CheckStatus     `json:"verify_status,omitempty"`
	VerifyResult        string          `json:"verify_result,omitempty"`
	VerifySessionID     string          `json:"verify_session_id,omitempty"`
	VerifiedAt          time.Time       `json:"verified_at,omitempty"`
	FailureStrategy     FailureStrategy `json:"failure_strategy,omitempty"`
	RollbackPrompt      string          `json:"rollback_prompt,omitempty"`
	RollbackModel       string          `json:"rollback_model,omitempty"`
	RollbackSessionID   string          `json:"rollback_session_id,omitempty"`
	RollbackResult      string          `json:"rollback_result,omitempty"`
	RolledBackAt        time.Time       `json:"rolled_back_at,omitempty"`
	DegradePrompt       string          `json:"degrade_prompt,omitempty"`
	DegradeModel        string          `json:"degrade_model,omitempty"`
	DegradeSessionID    string          `json:"degrade_session_id,omitempty"`
	DegradeResult       string          `json:"degrade_result,omitempty"`
	DegradedAt          time.Time       `json:"degraded_at,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	StartedAt           time.Time       `json:"started_at,omitempty"`
	FinishedAt          time.Time       `json:"finished_at,omitempty"`
}

type Plan struct {
	ID                 string    `json:"id"`
	Goal               string    `json:"goal"`
	Summary            string    `json:"summary,omitempty"`
	SessionID          string    `json:"session_id,omitempty"`
	Status             Status    `json:"status"`
	MaxParallel        int       `json:"max_parallel,omitempty"`
	DefaultMaxAttempts int       `json:"default_max_attempts,omitempty"`
	AutoReview         bool      `json:"auto_review,omitempty"`
	AutoVerify         bool      `json:"auto_verify,omitempty"`
	ReviewModel        string    `json:"review_model,omitempty"`
	VerifyModel        string    `json:"verify_model,omitempty"`
	Notes              []string  `json:"notes,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	StartedAt          time.Time `json:"started_at,omitempty"`
	FinishedAt         time.Time `json:"finished_at,omitempty"`
	Steps              []Step    `json:"steps,omitempty"`
}

type Store struct {
	path  string
	mu    sync.Mutex
	plans []Plan
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var plans []Plan
	if err := json.Unmarshal(raw, &plans); err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range plans {
		normalizePlan(&plans[i], now)
	}
	sortPlans(plans)
	s.plans = plans
	return nil
}

func (s *Store) Create(plan Plan) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	normalizePlan(&plan, now)
	s.plans = append(s.plans, plan)
	sortPlans(s.plans)
	if err := s.saveLocked(); err != nil {
		return Plan{}, err
	}
	return clonePlan(plan), nil
}

func (s *Store) List() []Plan {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Plan, len(s.plans))
	for i, item := range s.plans {
		out[i] = clonePlan(item)
	}
	return out
}

func (s *Store) ActiveForSession(sessionID string, limit int) []Plan {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Plan
	for _, item := range s.plans {
		if item.SessionID != sessionID || !isOpenStatus(item.Status) {
			continue
		}
		out = append(out, clonePlan(item))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) Get(id string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.plans {
		if item.ID == id {
			return clonePlan(item), nil
		}
	}
	return Plan{}, fmt.Errorf("plan %q not found", id)
}

func (s *Store) Update(id string, mutate func(*Plan) error) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.plans {
		if s.plans[i].ID != id {
			continue
		}
		working := clonePlan(s.plans[i])
		if err := mutate(&working); err != nil {
			return Plan{}, err
		}
		normalizePlan(&working, time.Now().UTC())
		s.plans[i] = working
		sortPlans(s.plans)
		if err := s.saveLocked(); err != nil {
			return Plan{}, err
		}
		return clonePlan(working), nil
	}
	return Plan{}, fmt.Errorf("plan %q not found", id)
}

func (s *Store) UpdateStep(planID string, stepID string, mutate func(*Plan, *Step) error) (Plan, error) {
	return s.Update(planID, func(item *Plan) error {
		index := FindStepIndex(*item, stepID)
		if index == -1 {
			return fmt.Errorf("step %q not found in plan %q", stepID, planID)
		}
		return mutate(item, &item.Steps[index])
	})
}

func FindStep(plan Plan, stepID string) (Step, bool) {
	index := FindStepIndex(plan, stepID)
	if index == -1 {
		return Step{}, false
	}
	return cloneStep(plan.Steps[index]), true
}

func FindStepIndex(plan Plan, stepID string) int {
	for i := range plan.Steps {
		if plan.Steps[i].ID == stepID {
			return i
		}
	}
	return -1
}

func RunnableSteps(plan Plan) []Step {
	var out []Step
	for _, step := range plan.Steps {
		if step.Status != StepStatusPlanned {
			continue
		}
		if dependenciesSatisfied(plan, step) {
			out = append(out, cloneStep(step))
		}
	}
	return out
}

func dependenciesSatisfied(plan Plan, step Step) bool {
	if len(step.DependsOn) == 0 {
		return true
	}
	for _, dep := range step.DependsOn {
		depStep, ok := FindStep(plan, dep)
		if !ok || !dependencySatisfiedStatus(depStep.Status) {
			return false
		}
	}
	return true
}

func dependencySatisfiedStatus(status StepStatus) bool {
	switch status {
	case StepStatusSucceeded, StepStatusDegraded:
		return true
	default:
		return false
	}
}

func normalizePlan(plan *Plan, now time.Time) {
	plan.Goal = strings.TrimSpace(plan.Goal)
	plan.Summary = strings.TrimSpace(plan.Summary)
	plan.SessionID = strings.TrimSpace(plan.SessionID)
	plan.ReviewModel = strings.TrimSpace(plan.ReviewModel)
	plan.VerifyModel = strings.TrimSpace(plan.VerifyModel)
	if plan.ID == "" {
		plan.ID = fmt.Sprintf("plan-%d", now.UnixNano())
	}
	if plan.MaxParallel <= 0 {
		plan.MaxParallel = 2
	}
	if plan.DefaultMaxAttempts <= 0 {
		plan.DefaultMaxAttempts = 2
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	if plan.StartedAt.IsZero() && hasStartedStep(plan.Steps) {
		plan.StartedAt = now
	}
	for i := range plan.Steps {
		normalizeStep(plan, &plan.Steps[i], i, now)
	}
	plan.Status = derivePlanStatus(*plan)
	plan.UpdatedAt = now
	if isTerminalStatus(plan.Status) {
		if plan.FinishedAt.IsZero() {
			plan.FinishedAt = now
		}
	} else {
		plan.FinishedAt = time.Time{}
	}
}

func normalizeStep(plan *Plan, step *Step, index int, now time.Time) {
	step.Title = strings.TrimSpace(step.Title)
	step.Details = strings.TrimSpace(step.Details)
	step.Prompt = strings.TrimSpace(step.Prompt)
	step.Model = strings.TrimSpace(step.Model)
	step.SessionID = strings.TrimSpace(step.SessionID)
	step.TaskID = strings.TrimSpace(step.TaskID)
	step.ReviewPrompt = strings.TrimSpace(step.ReviewPrompt)
	step.ReviewModel = strings.TrimSpace(step.ReviewModel)
	step.VerifyPrompt = strings.TrimSpace(step.VerifyPrompt)
	step.VerifyModel = strings.TrimSpace(step.VerifyModel)
	step.RollbackPrompt = strings.TrimSpace(step.RollbackPrompt)
	step.RollbackModel = strings.TrimSpace(step.RollbackModel)
	step.DegradePrompt = strings.TrimSpace(step.DegradePrompt)
	step.DegradeModel = strings.TrimSpace(step.DegradeModel)
	step.ReviewSessionID = strings.TrimSpace(step.ReviewSessionID)
	step.VerifySessionID = strings.TrimSpace(step.VerifySessionID)
	step.RollbackSessionID = strings.TrimSpace(step.RollbackSessionID)
	step.DegradeSessionID = strings.TrimSpace(step.DegradeSessionID)
	if step.ID == "" {
		step.ID = fmt.Sprintf("step-%d", index+1)
	}
	if step.ExecutionMode == "" {
		step.ExecutionMode = ExecutionSubAgent
	}
	if step.MaxAttempts <= 0 {
		step.MaxAttempts = plan.DefaultMaxAttempts
		if step.MaxAttempts <= 0 {
			step.MaxAttempts = 2
		}
	}
	if step.RetryBackoffSeconds < 0 {
		step.RetryBackoffSeconds = 0
	}
	if step.Status == "" {
		step.Status = StepStatusPlanned
	}
	if step.FailureStrategy == "" {
		switch {
		case step.RollbackPrompt != "" && step.DegradePrompt != "":
			step.FailureStrategy = FailureStrategyRollbackThenDegrade
		case step.RollbackPrompt != "":
			step.FailureStrategy = FailureStrategyRollback
		case step.DegradePrompt != "":
			step.FailureStrategy = FailureStrategyDegrade
		default:
			step.FailureStrategy = FailureStrategyFail
		}
	}
	if step.CreatedAt.IsZero() {
		step.CreatedAt = now
	}
	step.UpdatedAt = now
	if step.Status == StepStatusRunning && step.StartedAt.IsZero() {
		step.StartedAt = now
	}
	if step.ReviewRequired || plan.AutoReview {
		if step.ReviewStatus == "" && !isTerminalCheckStatus(step.ReviewStatus) {
			step.ReviewStatus = CheckStatusPending
		}
	} else if step.ReviewStatus == "" {
		step.ReviewStatus = CheckStatusSkipped
	}
	if step.VerifyRequired || plan.AutoVerify {
		if step.VerifyStatus == "" && !isTerminalCheckStatus(step.VerifyStatus) {
			step.VerifyStatus = CheckStatusPending
		}
	} else if step.VerifyStatus == "" {
		step.VerifyStatus = CheckStatusSkipped
	}
	if isTerminalStepStatus(step.Status) {
		if step.FinishedAt.IsZero() {
			step.FinishedAt = now
		}
	} else {
		step.FinishedAt = time.Time{}
	}
}

func derivePlanStatus(plan Plan) Status {
	if len(plan.Steps) == 0 {
		return StatusDraft
	}
	allCancelled := true
	allComplete := true
	hasQueuedOrRunning := false
	hasPlanned := false
	hasBlocked := false
	for _, step := range plan.Steps {
		switch step.Status {
		case StepStatusQueued, StepStatusRunning, StepStatusVerifying, StepStatusReviewing:
			hasQueuedOrRunning = true
			allCancelled = false
			allComplete = false
		case StepStatusSucceeded, StepStatusDegraded:
			allCancelled = false
		case StepStatusCancelled:
		case StepStatusFailed:
			return StatusFailed
		case StepStatusBlocked:
			hasBlocked = true
			allCancelled = false
			allComplete = false
		default:
			hasPlanned = true
			allCancelled = false
			allComplete = false
		}
		if step.Status != StepStatusSucceeded && step.Status != StepStatusCancelled && step.Status != StepStatusDegraded {
			allComplete = false
		}
	}
	if allCancelled {
		return StatusCancelled
	}
	if allComplete {
		return StatusCompleted
	}
	if hasQueuedOrRunning || hasPlanned {
		return StatusActive
	}
	if hasBlocked {
		return StatusBlocked
	}
	return StatusActive
}

func hasStartedStep(steps []Step) bool {
	for _, step := range steps {
		if !step.StartedAt.IsZero() || step.Status == StepStatusRunning || step.Status == StepStatusVerifying || step.Status == StepStatusReviewing || step.Status == StepStatusSucceeded || step.Status == StepStatusDegraded || step.Status == StepStatusFailed {
			return true
		}
	}
	return false
}

func isOpenStatus(status Status) bool {
	switch status {
	case StatusCompleted, StatusCancelled:
		return false
	default:
		return true
	}
}

func isTerminalStatus(status Status) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func isTerminalStepStatus(status StepStatus) bool {
	switch status {
	case StepStatusSucceeded, StepStatusDegraded, StepStatusFailed, StepStatusCancelled:
		return true
	default:
		return false
	}
}

func isTerminalCheckStatus(status CheckStatus) bool {
	switch status {
	case CheckStatusPassed, CheckStatusFailed, CheckStatusSkipped:
		return true
	default:
		return false
	}
}

func sortPlans(plans []Plan) {
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].UpdatedAt.After(plans[j].UpdatedAt)
	})
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.plans, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

func clonePlan(plan Plan) Plan {
	out := plan
	if len(plan.Notes) > 0 {
		out.Notes = append([]string(nil), plan.Notes...)
	}
	if len(plan.Steps) > 0 {
		out.Steps = make([]Step, len(plan.Steps))
		for i, step := range plan.Steps {
			out.Steps[i] = cloneStep(step)
		}
	}
	return out
}

func cloneStep(step Step) Step {
	out := step
	if len(step.DependsOn) > 0 {
		out.DependsOn = append([]string(nil), step.DependsOn...)
	}
	if len(step.Notes) > 0 {
		out.Notes = append([]string(nil), step.Notes...)
	}
	return out
}
