package social

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"qorvexus/internal/config"
)

type IdentityState struct {
	OwnerRoutes   []string `json:"owner_routes,omitempty"`
	OwnerIDs      []string `json:"owner_ids,omitempty"`
	OwnerAliases  []string `json:"owner_aliases,omitempty"`
	TrustedRoutes []string `json:"trusted_routes,omitempty"`
	TrustedIDs    []string `json:"trusted_ids,omitempty"`
}

type IdentityStore struct {
	path  string
	mu    sync.RWMutex
	state IdentityState
}

func NewIdentityStore(path string) *IdentityStore {
	s := &IdentityStore{path: path}
	_ = s.load()
	return s
}

func (s *IdentityStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var state IdentityState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	state.normalize()
	s.state = state
	return nil
}

func (s *IdentityStore) Snapshot() IdentityState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.clone()
}

func (s *IdentityStore) GrantOwner(channel string, senderID string, senderName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	targets := make([]string, 0, 2)
	if route := identityRouteKey(channel, senderID); route != "" {
		if appendUnique(&s.state.OwnerRoutes, route) {
			changed = true
		}
		targets = append(targets, route)
	}
	if senderID = strings.TrimSpace(senderID); senderID != "" {
		if appendUnique(&s.state.OwnerIDs, senderID) {
			changed = true
		}
		targets = append(targets, senderID)
	}
	if senderName = strings.TrimSpace(senderName); senderName != "" {
		if appendUniqueFold(&s.state.OwnerAliases, senderName) {
			changed = true
		}
		targets = append(targets, senderName)
	}
	s.state.normalize()
	if changed {
		if err := s.saveLocked(); err != nil {
			return "", err
		}
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("owner grant requires sender_id or sender_name")
	}
	return "granted owner identity for " + strings.Join(targets, ", "), nil
}

func (s *IdentityStore) ClaimFirstOwner(cfg config.IdentityConfig, channel string, senderID string, senderName string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hasConcreteOwnerLocked(cfg, s.state) {
		return false, nil
	}
	if strings.TrimSpace(senderID) == "" && strings.TrimSpace(senderName) == "" {
		return false, nil
	}
	if route := identityRouteKey(channel, senderID); route != "" {
		appendUnique(&s.state.OwnerRoutes, route)
	}
	if senderID = strings.TrimSpace(senderID); senderID != "" {
		appendUnique(&s.state.OwnerIDs, senderID)
	}
	if senderName = strings.TrimSpace(senderName); senderName != "" {
		appendUniqueFold(&s.state.OwnerAliases, senderName)
	}
	s.state.normalize()
	if err := s.saveLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *IdentityStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o644)
}

func (s IdentityState) clone() IdentityState {
	return IdentityState{
		OwnerRoutes:   append([]string(nil), s.OwnerRoutes...),
		OwnerIDs:      append([]string(nil), s.OwnerIDs...),
		OwnerAliases:  append([]string(nil), s.OwnerAliases...),
		TrustedRoutes: append([]string(nil), s.TrustedRoutes...),
		TrustedIDs:    append([]string(nil), s.TrustedIDs...),
	}
}

func (s *IdentityState) normalize() {
	s.OwnerRoutes = dedupeStrings(s.OwnerRoutes)
	s.OwnerIDs = dedupeStrings(s.OwnerIDs)
	s.OwnerAliases = dedupeStringsFold(s.OwnerAliases)
	s.TrustedRoutes = dedupeStrings(s.TrustedRoutes)
	s.TrustedIDs = dedupeStrings(s.TrustedIDs)
}

func hasConcreteOwner(cfg config.IdentityConfig, state IdentityState) bool {
	state.normalize()
	return hasConcreteOwnerLocked(cfg, state)
}

func hasConcreteOwnerLocked(cfg config.IdentityConfig, state IdentityState) bool {
	if len(state.OwnerRoutes) > 0 || len(state.OwnerIDs) > 0 || len(state.OwnerAliases) > 0 {
		return true
	}
	for _, id := range cfg.OwnerIDs {
		if strings.TrimSpace(id) != "" {
			return true
		}
	}
	for _, alias := range cfg.OwnerAliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if strings.EqualFold(alias, "owner") {
			continue
		}
		return true
	}
	return false
}

func identityRouteKey(channel string, senderID string) string {
	channel = strings.TrimSpace(channel)
	senderID = strings.TrimSpace(senderID)
	if channel == "" || senderID == "" {
		return ""
	}
	return channel + ":" + senderID
}

func appendUnique(items *[]string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range *items {
		if strings.TrimSpace(item) == value {
			return false
		}
	}
	*items = append(*items, value)
	return true
}

func appendUniqueFold(items *[]string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range *items {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return false
		}
	}
	*items = append(*items, value)
	return true
}

func dedupeStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if appendUnique(&out, item) {
			continue
		}
	}
	return out
}

func dedupeStringsFold(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if appendUniqueFold(&out, item) {
			continue
		}
	}
	return out
}
