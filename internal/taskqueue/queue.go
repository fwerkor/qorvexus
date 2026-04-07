package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type Task struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Prompt     string    `json:"prompt"`
	Model      string    `json:"model,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	PlanID     string    `json:"plan_id,omitempty"`
	StepID     string    `json:"step_id,omitempty"`
	Status     Status    `json:"status"`
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type Runner interface {
	RunQueuedTask(ctx context.Context, task Task) (string, error)
}

type Queue struct {
	path   string
	runner Runner
	mu     sync.Mutex
	tasks  []Task
}

func New(path string, runner Runner) *Queue {
	return &Queue{path: path, runner: runner}
}

func (q *Queue) Load() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	raw, err := os.ReadFile(q.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(raw, &q.tasks)
}

func (q *Queue) Add(task Task) (Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if task.ID == "" {
		task.ID = fmt.Sprintf("queue-%d", time.Now().UTC().UnixNano())
	}
	task.Status = StatusQueued
	task.CreatedAt = time.Now().UTC()
	q.tasks = append(q.tasks, task)
	return task, q.saveLocked()
}

func (q *Queue) List() []Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Task, len(q.tasks))
	copy(out, q.tasks)
	return out
}

func (q *Queue) RunNext(ctx context.Context) (bool, error) {
	q.mu.Lock()
	index := -1
	for i := range q.tasks {
		if q.tasks[i].Status == StatusQueued {
			index = i
			q.tasks[i].Status = StatusRunning
			q.tasks[i].StartedAt = time.Now().UTC()
			break
		}
	}
	if index == -1 {
		q.mu.Unlock()
		return false, nil
	}
	task := q.tasks[index]
	if err := q.saveLocked(); err != nil {
		q.mu.Unlock()
		return false, err
	}
	q.mu.Unlock()

	result, err := q.runner.RunQueuedTask(ctx, task)

	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks[index].FinishedAt = time.Now().UTC()
	if err != nil {
		q.tasks[index].Status = StatusFailed
		q.tasks[index].Error = err.Error()
	} else {
		q.tasks[index].Status = StatusSucceeded
		q.tasks[index].Result = result
	}
	return true, q.saveLocked()
}

func (q *Queue) Retry(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.tasks {
		if q.tasks[i].ID == id {
			q.tasks[i].Status = StatusQueued
			q.tasks[i].Error = ""
			q.tasks[i].Result = ""
			q.tasks[i].StartedAt = time.Time{}
			q.tasks[i].FinishedAt = time.Time{}
			return q.saveLocked()
		}
	}
	return fmt.Errorf("task %q not found", id)
}

func (q *Queue) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(q.tasks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(q.path, raw, 0o644)
}
