package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Task struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"`
	Prompt    string    `json:"prompt"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
}

type Runner interface {
	RunScheduled(ctx context.Context, task Task) error
}

type Manager struct {
	path    string
	runner  Runner
	cron    *cron.Cron
	mu      sync.Mutex
	tasks   []Task
	started bool
}

func NewManager(path string, runner Runner) *Manager {
	return &Manager{
		path:   path,
		runner: runner,
		cron:   cron.New(cron.WithSeconds()),
	}
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(raw, &m.tasks)
}

func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	for _, task := range m.tasks {
		if err := m.registerLocked(task); err != nil {
			return err
		}
	}
	m.cron.Start()
	m.started = true
	return nil
}

func (m *Manager) Add(task Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task.CreatedAt = time.Now().UTC()
	if task.ID == "" {
		task.ID = fmt.Sprintf("task-%d", task.CreatedAt.UnixNano())
	}
	if err := m.registerLocked(task); err != nil {
		return err
	}
	m.tasks = append(m.tasks, task)
	return m.saveLocked()
}

func (m *Manager) registerLocked(task Task) error {
	_, err := m.cron.AddFunc(task.Schedule, func() {
		_ = m.runner.RunScheduled(context.Background(), task)
	})
	return err
}

func (m *Manager) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m.tasks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, raw, 0o644)
}
