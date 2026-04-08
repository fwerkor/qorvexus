package social

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

type FollowUpStatus string

const (
	FollowUpStatusOpen      FollowUpStatus = "open"
	FollowUpStatusQueued    FollowUpStatus = "queued"
	FollowUpStatusHeld      FollowUpStatus = "held"
	FollowUpStatusSent      FollowUpStatus = "sent"
	FollowUpStatusCompleted FollowUpStatus = "completed"
	FollowUpStatusDismissed FollowUpStatus = "dismissed"
)

type FollowUp struct {
	ID                  string         `json:"id"`
	ScopeKey            string         `json:"scope_key"`
	Channel             string         `json:"channel,omitempty"`
	ThreadID            string         `json:"thread_id,omitempty"`
	ContactKey          string         `json:"contact_key,omitempty"`
	ContactName         string         `json:"contact_name,omitempty"`
	Trust               string         `json:"trust,omitempty"`
	Summary             string         `json:"summary"`
	RecommendedAction   string         `json:"recommended_action,omitempty"`
	Reason              string         `json:"reason,omitempty"`
	DueHint             string         `json:"due_hint,omitempty"`
	Priority            string         `json:"priority,omitempty"`
	Disposition         string         `json:"disposition,omitempty"`
	Status              FollowUpStatus `json:"status"`
	RelatedCommitmentID string         `json:"related_commitment_id,omitempty"`
	RelatedOutboxID     string         `json:"related_outbox_id,omitempty"`
	RelatedTaskID       string         `json:"related_task_id,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	LastActionAt        time.Time      `json:"last_action_at,omitempty"`
}

type FollowUpStore struct {
	path string
	mu   sync.Mutex
}

func NewFollowUpStore(path string) *FollowUpStore {
	return &FollowUpStore{path: path}
}

func (s *FollowUpStore) Upsert(item FollowUp) (FollowUp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return FollowUp{}, err
	}
	now := time.Now().UTC()
	item.ScopeKey = strings.TrimSpace(item.ScopeKey)
	item.Summary = strings.TrimSpace(item.Summary)
	item.RecommendedAction = strings.TrimSpace(item.RecommendedAction)
	item.Reason = strings.TrimSpace(item.Reason)
	item.Priority = strings.TrimSpace(strings.ToLower(item.Priority))
	item.Disposition = strings.TrimSpace(strings.ToLower(item.Disposition))
	if item.ScopeKey == "" {
		item.ScopeKey = defaultFollowUpScope(item)
	}
	for i := range items {
		if items[i].ScopeKey != item.ScopeKey {
			continue
		}
		existing := items[i]
		existing.Channel = chooseString(item.Channel, existing.Channel)
		existing.ThreadID = chooseString(item.ThreadID, existing.ThreadID)
		existing.ContactKey = chooseString(item.ContactKey, existing.ContactKey)
		existing.ContactName = chooseString(item.ContactName, existing.ContactName)
		existing.Trust = chooseString(item.Trust, existing.Trust)
		existing.Summary = chooseString(item.Summary, existing.Summary)
		existing.RecommendedAction = chooseString(item.RecommendedAction, existing.RecommendedAction)
		existing.Reason = chooseString(item.Reason, existing.Reason)
		existing.DueHint = chooseString(item.DueHint, existing.DueHint)
		existing.Priority = chooseString(item.Priority, existing.Priority)
		existing.Disposition = chooseString(item.Disposition, existing.Disposition)
		if item.Status != "" {
			existing.Status = item.Status
		}
		existing.RelatedCommitmentID = chooseString(item.RelatedCommitmentID, existing.RelatedCommitmentID)
		existing.RelatedOutboxID = chooseString(item.RelatedOutboxID, existing.RelatedOutboxID)
		existing.RelatedTaskID = chooseString(item.RelatedTaskID, existing.RelatedTaskID)
		if item.LastActionAt.After(existing.LastActionAt) {
			existing.LastActionAt = item.LastActionAt
		}
		existing.UpdatedAt = now
		items[i] = existing
		if err := s.saveLocked(items); err != nil {
			return FollowUp{}, err
		}
		return existing, nil
	}
	if item.ID == "" {
		item.ID = fmt.Sprintf("followup-%d", now.UnixNano())
	}
	if item.Status == "" {
		item.Status = FollowUpStatusOpen
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	items = append(items, item)
	if err := s.saveLocked(items); err != nil {
		return FollowUp{}, err
	}
	return item, nil
}

func (s *FollowUpStore) List(limit int, status string) ([]FollowUp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(strings.ToLower(status))
	out := make([]FollowUp, 0, len(items))
	for _, item := range items {
		if status != "" && string(item.Status) != status {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *FollowUpStore) Update(id string, mutate func(*FollowUp) error) (FollowUp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return FollowUp{}, err
	}
	for i := range items {
		if items[i].ID != id {
			continue
		}
		working := items[i]
		if err := mutate(&working); err != nil {
			return FollowUp{}, err
		}
		working.UpdatedAt = time.Now().UTC()
		items[i] = working
		if err := s.saveLocked(items); err != nil {
			return FollowUp{}, err
		}
		return working, nil
	}
	return FollowUp{}, fmt.Errorf("social follow-up %q not found", id)
}

func defaultFollowUpScope(item FollowUp) string {
	var parts []string
	if item.Channel != "" {
		parts = append(parts, strings.ToLower(strings.TrimSpace(item.Channel)))
	}
	if item.ThreadID != "" {
		parts = append(parts, strings.ToLower(strings.TrimSpace(item.ThreadID)))
	}
	if item.ContactKey != "" {
		parts = append(parts, strings.ToLower(strings.TrimSpace(item.ContactKey)))
	}
	if item.RelatedCommitmentID != "" {
		parts = append(parts, "commitment:"+strings.ToLower(strings.TrimSpace(item.RelatedCommitmentID)))
	}
	if item.RecommendedAction != "" {
		parts = append(parts, strings.ToLower(strings.TrimSpace(item.RecommendedAction)))
	}
	if item.Summary != "" {
		parts = append(parts, sanitizeSummary(item.Summary))
	}
	if len(parts) == 0 {
		return "followup:general"
	}
	return strings.Join(parts, "|")
}

func sanitizeSummary(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), "-")
	if len(value) > 48 {
		value = value[:48]
	}
	return value
}

func chooseString(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func (s *FollowUpStore) loadLocked() ([]FollowUp, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var items []FollowUp
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *FollowUpStore) saveLocked(items []FollowUp) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}
