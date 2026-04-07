package commitment

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Status string

const (
	StatusOpen      Status = "open"
	StatusCompleted Status = "completed"
	StatusCanceled  Status = "canceled"
	StatusOverdue   Status = "overdue"
)

type Entry struct {
	ID               string    `json:"id"`
	Channel          string    `json:"channel,omitempty"`
	ThreadID         string    `json:"thread_id,omitempty"`
	Counterparty     string    `json:"counterparty,omitempty"`
	Summary          string    `json:"summary"`
	DueHint          string    `json:"due_hint,omitempty"`
	Trust            string    `json:"trust,omitempty"`
	Status           Status    `json:"status"`
	Source           string    `json:"source,omitempty"`
	RelatedTaskID    string    `json:"related_task_id,omitempty"`
	LastReviewTaskID string    `json:"last_review_task_id,omitempty"`
	LastReviewAt     time.Time `json:"last_review_at,omitempty"`
	EscalationLevel  int       `json:"escalation_level,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

type Summary struct {
	Total              int            `json:"total"`
	Open               int            `json:"open"`
	Completed          int            `json:"completed"`
	Canceled           int            `json:"canceled"`
	Overdue            int            `json:"overdue"`
	WithDueHint        int            `json:"with_due_hint"`
	WithReviewHistory  int            `json:"with_review_history"`
	MaxEscalationLevel int            `json:"max_escalation_level"`
	ByChannel          map[string]int `json:"by_channel"`
	ByTrust            map[string]int `json:"by_trust"`
	LastUpdatedAt      time.Time      `json:"last_updated_at,omitempty"`
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Append(entry Entry) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return Entry{}, err
	}
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("commit-%d", time.Now().UTC().UnixNano())
	}
	if entry.Status == "" {
		entry.Status = StatusOpen
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	items = append(items, entry)
	return entry, s.saveLocked(items)
}

func (s *Store) List(limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (s *Store) Get(id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return Entry{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return Entry{}, fmt.Errorf("commitment %q not found", id)
}

func (s *Store) UpdateStatus(id string, status Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].ID == id {
			items[i].Status = status
			items[i].UpdatedAt = time.Now().UTC()
			return s.saveLocked(items)
		}
	}
	return fmt.Errorf("commitment %q not found", id)
}

func (s *Store) Summary() (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return Summary{}, err
	}
	out := Summary{
		ByChannel: map[string]int{},
		ByTrust:   map[string]int{},
	}
	for _, item := range items {
		out.Total++
		switch item.Status {
		case StatusOpen:
			out.Open++
		case StatusCompleted:
			out.Completed++
		case StatusCanceled:
			out.Canceled++
		case StatusOverdue:
			out.Overdue++
		}
		if item.DueHint != "" {
			out.WithDueHint++
		}
		if !item.LastReviewAt.IsZero() {
			out.WithReviewHistory++
		}
		if item.EscalationLevel > out.MaxEscalationLevel {
			out.MaxEscalationLevel = item.EscalationLevel
		}
		if item.Channel != "" {
			out.ByChannel[item.Channel]++
		}
		if item.Trust != "" {
			out.ByTrust[item.Trust]++
		}
		if item.UpdatedAt.After(out.LastUpdatedAt) {
			out.LastUpdatedAt = item.UpdatedAt
		}
	}
	return out, nil
}

func (s *Store) AttachTask(id string, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].ID == id {
			items[i].RelatedTaskID = taskID
			items[i].UpdatedAt = time.Now().UTC()
			return s.saveLocked(items)
		}
	}
	return fmt.Errorf("commitment %q not found", id)
}

func (s *Store) NoteReviewQueued(id string, taskID string, escalationLevel int, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].ID == id {
			items[i].RelatedTaskID = taskID
			items[i].LastReviewTaskID = taskID
			items[i].LastReviewAt = at
			if escalationLevel > items[i].EscalationLevel {
				items[i].EscalationLevel = escalationLevel
			}
			items[i].UpdatedAt = at
			return s.saveLocked(items)
		}
	}
	return fmt.Errorf("commitment %q not found", id)
}

func (s *Store) loadLocked() ([]Entry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var items []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil {
			items = append(items, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) saveLocked(items []Entry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, entry := range items {
		if err := enc.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}
