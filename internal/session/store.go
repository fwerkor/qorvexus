package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"qorvexus/internal/types"
)

type State struct {
	ID        string          `json:"id"`
	Model     string          `json:"model"`
	Messages  []types.Message `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Store struct {
	root  string
	mu    sync.Mutex
	cache map[string]*State
}

func NewStore(root string) *Store {
	return &Store{
		root:  filepath.Join(root, "sessions"),
		cache: map[string]*State{},
	}
}

func (s *Store) Load(id string) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.cache[id]; ok {
		cp := *st
		return &cp, nil
	}
	path := filepath.Join(s.root, id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	st := &State{}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	s.cache[id] = st
	cp := *st
	return &cp, nil
}

func (s *Store) Save(state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	now := time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.root, state.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	cp := *state
	s.cache[state.ID] = &cp
	return nil
}

func (s *Store) List() ([]State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var out []State
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			continue
		}
		var state State
		if err := json.Unmarshal(raw, &state); err != nil {
			continue
		}
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}
