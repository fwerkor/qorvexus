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

	"qorvexus/internal/types"
)

type DraftStatus string

const (
	DraftStatusHeld      DraftStatus = "held"
	DraftStatusReady     DraftStatus = "ready"
	DraftStatusSent      DraftStatus = "sent"
	DraftStatusDiscarded DraftStatus = "discarded"
)

type Draft struct {
	ID                  string                    `json:"id"`
	Channel             string                    `json:"channel"`
	ThreadID            string                    `json:"thread_id,omitempty"`
	Recipient           string                    `json:"recipient,omitempty"`
	ContactKey          string                    `json:"contact_key,omitempty"`
	Counterparty        string                    `json:"counterparty,omitempty"`
	Text                string                    `json:"text"`
	Reason              string                    `json:"reason,omitempty"`
	Source              string                    `json:"source,omitempty"`
	Boundary            string                    `json:"boundary,omitempty"`
	Hold                bool                      `json:"hold,omitempty"`
	Status              DraftStatus               `json:"status"`
	Context             types.ConversationContext `json:"context,omitempty"`
	RelatedCommitmentID string                    `json:"related_commitment_id,omitempty"`
	RelatedFollowUpID   string                    `json:"related_followup_id,omitempty"`
	ReviewedBy          string                    `json:"reviewed_by,omitempty"`
	ReviewedAt          time.Time                 `json:"reviewed_at,omitempty"`
	DiscardedAt         time.Time                 `json:"discarded_at,omitempty"`
	SentAt              time.Time                 `json:"sent_at,omitempty"`
	DeliveryResult      string                    `json:"delivery_result,omitempty"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
}

type DraftStore struct {
	path string
	mu   sync.Mutex
}

func NewDraftStore(path string) *DraftStore {
	return &DraftStore{path: path}
}

func (s *DraftStore) Append(draft Draft) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return Draft{}, err
	}
	now := time.Now().UTC()
	if draft.ID == "" {
		draft.ID = fmt.Sprintf("outbox-%d", now.UnixNano())
	}
	draft.Text = strings.TrimSpace(draft.Text)
	draft.Reason = strings.TrimSpace(draft.Reason)
	draft.Source = strings.TrimSpace(draft.Source)
	draft.ContactKey = strings.TrimSpace(draft.ContactKey)
	draft.Counterparty = strings.TrimSpace(draft.Counterparty)
	draft.Boundary = strings.TrimSpace(draft.Boundary)
	if draft.Status == "" {
		if draft.Hold {
			draft.Status = DraftStatusHeld
		} else {
			draft.Status = DraftStatusReady
		}
	}
	if draft.CreatedAt.IsZero() {
		draft.CreatedAt = now
	}
	draft.UpdatedAt = now
	items = append(items, draft)
	if err := s.saveLocked(items); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func (s *DraftStore) Get(id string) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return Draft{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return Draft{}, fmt.Errorf("social outbox entry %q not found", id)
}

func (s *DraftStore) List(limit int, status string) ([]Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(strings.ToLower(status))
	out := make([]Draft, 0, len(items))
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

func (s *DraftStore) Update(id string, mutate func(*Draft) error) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.loadLocked()
	if err != nil {
		return Draft{}, err
	}
	for i := range items {
		if items[i].ID != id {
			continue
		}
		working := items[i]
		if err := mutate(&working); err != nil {
			return Draft{}, err
		}
		working.UpdatedAt = time.Now().UTC()
		items[i] = working
		if err := s.saveLocked(items); err != nil {
			return Draft{}, err
		}
		return working, nil
	}
	return Draft{}, fmt.Errorf("social outbox entry %q not found", id)
}

func (s *DraftStore) loadLocked() ([]Draft, error) {
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
	var items []Draft
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *DraftStore) saveLocked(items []Draft) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}
