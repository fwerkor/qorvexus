package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	ID             string    `json:"id"`
	Key            string    `json:"key,omitempty"`
	Area           string    `json:"area,omitempty"`
	Kind           string    `json:"kind,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	Summary        string    `json:"summary,omitempty"`
	Content        string    `json:"content"`
	Source         string    `json:"source,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Importance     int       `json:"importance,omitempty"`
	Confidence     float64   `json:"confidence,omitempty"`
	AccessCount    int       `json:"access_count,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	LastAccessedAt time.Time `json:"last_accessed_at,omitempty"`
	Archived       bool      `json:"archived,omitempty"`
}

type SearchOptions struct {
	Query           string
	Limit           int
	Areas           []string
	Kinds           []string
	Tags            []string
	IncludeArchived bool
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (e Entry) HasTag(tag string) bool {
	for _, item := range e.Tags {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(tag)) {
			return true
		}
	}
	return false
}

func (e Entry) MatchArea(area string) bool {
	return strings.EqualFold(strings.TrimSpace(e.Area), strings.TrimSpace(area))
}

func (e Entry) MatchKind(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(e.Kind), strings.TrimSpace(kind))
}

func (s *Store) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadEntriesLocked()
	if err != nil {
		return err
	}
	entries = append(entries, normalizeEntry(entry, time.Now().UTC(), false))
	return s.saveEntriesLocked(entries)
}

func (s *Store) Upsert(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	entries, err := s.loadEntriesLocked()
	if err != nil {
		return err
	}
	entry = normalizeEntry(entry, now, true)
	for i := range entries {
		if sameLogicalEntry(entries[i], entry) {
			entry.ID = preferString(entry.ID, entries[i].ID)
			if entry.CreatedAt.IsZero() {
				entry.CreatedAt = entries[i].CreatedAt
			}
			entry.AccessCount += entries[i].AccessCount
			if entry.LastAccessedAt.IsZero() {
				entry.LastAccessedAt = entries[i].LastAccessedAt
			}
			entries[i] = mergeEntries(entries[i], entry, now)
			return s.saveEntriesLocked(entries)
		}
	}
	entries = append(entries, entry)
	return s.saveEntriesLocked(entries)
}

func (s *Store) Search(query string, limit int) ([]Entry, error) {
	return s.SearchWithOptions(SearchOptions{Query: query, Limit: limit})
}

func (s *Store) SearchWithOptions(opts SearchOptions) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadEntriesLocked()
	if err != nil {
		return nil, err
	}
	terms := tokenize(opts.Query)
	type scored struct {
		entry Entry
		score float64
	}
	results := make([]scored, 0, len(entries))
	for _, entry := range entries {
		if !matchesFilters(entry, opts) {
			continue
		}
		score := scoreEntry(entry, terms)
		if score > 0 || len(terms) == 0 {
			results = append(results, scored{entry: entry, score: score})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if math.Abs(results[i].score-results[j].score) < 0.001 {
			return effectiveTime(results[i].entry).After(effectiveTime(results[j].entry))
		}
		return results[i].score > results[j].score
	})
	limit := opts.Limit
	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}
	out := make([]Entry, 0, limit)
	for _, item := range results[:limit] {
		out = append(out, item.entry)
	}
	return out, nil
}

func (s *Store) SearchByTag(tag string, limit int) ([]Entry, error) {
	return s.SearchWithOptions(SearchOptions{
		Limit: limit,
		Tags:  []string{tag},
	})
}

func (s *Store) SearchByArea(area string, limit int) ([]Entry, error) {
	return s.SearchWithOptions(SearchOptions{
		Limit: limit,
		Areas: []string{area},
	})
}

func (s *Store) MarkAccessed(ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return nil
	}
	lookup := map[string]struct{}{}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			lookup[id] = struct{}{}
		}
	}
	entries, err := s.loadEntriesLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	changed := false
	for i := range entries {
		if _, ok := lookup[entries[i].ID]; ok {
			entries[i].AccessCount++
			entries[i].LastAccessedAt = now
			entries[i].UpdatedAt = now
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveEntriesLocked(entries)
}

func (s *Store) loadEntriesLocked() ([]Entry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		entry = normalizeEntry(entry, time.Now().UTC(), false)
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return compactEntries(entries), nil
}

func (s *Store) saveEntriesLocked(entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return effectiveTime(entries[i]).Before(effectiveTime(entries[j]))
	})
	f, err := os.Create(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, entry := range entries {
		raw, err := json.Marshal(normalizeEntry(entry, time.Now().UTC(), false))
		if err != nil {
			return err
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func compactEntries(entries []Entry) []Entry {
	byKey := map[string]int{}
	var out []Entry
	for _, entry := range entries {
		if entry.Key != "" {
			if idx, ok := byKey[strings.ToLower(entry.Key)]; ok {
				out[idx] = mergeEntries(out[idx], entry, effectiveTime(entry))
				continue
			}
			byKey[strings.ToLower(entry.Key)] = len(out)
		}
		out = append(out, entry)
	}
	return out
}

func normalizeEntry(entry Entry, now time.Time, preserveID bool) Entry {
	if strings.TrimSpace(entry.ID) == "" && !preserveID {
		entry.ID = fmt.Sprintf("mem-%d", now.UnixNano())
	}
	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = fmt.Sprintf("mem-%d", now.UnixNano())
	}
	entry.Key = strings.TrimSpace(entry.Key)
	entry.Area = strings.TrimSpace(entry.Area)
	entry.Kind = strings.TrimSpace(entry.Kind)
	entry.Subject = strings.TrimSpace(entry.Subject)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Source = strings.TrimSpace(entry.Source)
	entry.Tags = dedupeTags(entry.Tags)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.CreatedAt
	}
	if entry.Summary == "" {
		entry.Summary = compact(entry.Content, 200)
	}
	if entry.Importance < 0 {
		entry.Importance = 0
	}
	if entry.Importance > 10 {
		entry.Importance = 10
	}
	if entry.Confidence < 0 {
		entry.Confidence = 0
	}
	if entry.Confidence > 1 {
		entry.Confidence = 1
	}
	return entry
}

func mergeEntries(existing Entry, incoming Entry, now time.Time) Entry {
	out := existing
	out.Key = preferString(incoming.Key, existing.Key)
	out.Area = preferString(incoming.Area, existing.Area)
	out.Kind = preferString(incoming.Kind, existing.Kind)
	out.Subject = preferString(incoming.Subject, existing.Subject)
	out.Summary = preferString(incoming.Summary, existing.Summary)
	out.Content = preferString(incoming.Content, existing.Content)
	out.Source = preferString(incoming.Source, existing.Source)
	out.Tags = dedupeTags(append(existing.Tags, incoming.Tags...))
	if incoming.Importance > 0 || existing.Importance == 0 {
		out.Importance = max(existing.Importance, incoming.Importance)
	}
	if incoming.Confidence > 0 || existing.Confidence == 0 {
		out.Confidence = math.Max(existing.Confidence, incoming.Confidence)
	}
	out.AccessCount = max(existing.AccessCount, incoming.AccessCount)
	out.Archived = incoming.Archived
	out.UpdatedAt = now
	if existing.CreatedAt.IsZero() {
		out.CreatedAt = incoming.CreatedAt
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	out.LastAccessedAt = laterTime(existing.LastAccessedAt, incoming.LastAccessedAt)
	return normalizeEntry(out, now, true)
}

func matchesFilters(entry Entry, opts SearchOptions) bool {
	if entry.Archived && !opts.IncludeArchived {
		return false
	}
	if len(opts.Areas) > 0 {
		matched := false
		for _, area := range opts.Areas {
			if entry.MatchArea(area) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(opts.Kinds) > 0 {
		matched := false
		for _, kind := range opts.Kinds {
			if entry.MatchKind(kind) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, tag := range opts.Tags {
		if !entry.HasTag(tag) {
			return false
		}
	}
	return true
}

func scoreEntry(entry Entry, terms []string) float64 {
	score := float64(entry.Importance) * 1.5
	score += entry.Confidence * 2
	if !entry.LastAccessedAt.IsZero() && time.Since(entry.LastAccessedAt) < 72*time.Hour {
		score += 1.5
	}
	if age := time.Since(effectiveTime(entry)); age < 7*24*time.Hour {
		score += 2
	} else if age < 30*24*time.Hour {
		score += 0.75
	}
	score += math.Min(float64(entry.AccessCount)*0.25, 2)
	if len(terms) == 0 {
		return score
	}

	content := strings.ToLower(entry.Content)
	summary := strings.ToLower(entry.Summary)
	subject := strings.ToLower(entry.Subject)
	source := strings.ToLower(entry.Source)
	key := strings.ToLower(entry.Key)
	area := strings.ToLower(entry.Area)
	kind := strings.ToLower(entry.Kind)
	tags := strings.ToLower(strings.Join(entry.Tags, " "))
	for _, term := range terms {
		switch {
		case summary == term || subject == term || key == term:
			score += 8
		case strings.Contains(tags, term) || area == term || kind == term:
			score += 7
		case strings.Contains(summary, term):
			score += 6
		case strings.Contains(subject, term):
			score += 5
		case strings.Contains(content, term):
			score += 4
		case strings.Contains(source, term) || strings.Contains(key, term):
			score += 3
		}
	}
	return score
}

func tokenize(value string) []string {
	fields := strings.Fields(strings.ToLower(value))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, ".,!?;:\"'`()[]{}")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func compact(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func stableKey(parts ...string) string {
	var cleaned []string
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		part = strings.ReplaceAll(part, "\n", " ")
		part = strings.Join(strings.Fields(part), "-")
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	return strings.Join(cleaned, ":")
}

func HashKey(value string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(value))))
	return fmt.Sprintf("%x", h.Sum64())
}

func sameLogicalEntry(existing Entry, incoming Entry) bool {
	if existing.Key != "" && incoming.Key != "" && strings.EqualFold(existing.Key, incoming.Key) {
		return true
	}
	return existing.ID != "" && incoming.ID != "" && existing.ID == incoming.ID
}

func preferString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func dedupeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func effectiveTime(entry Entry) time.Time {
	if !entry.UpdatedAt.IsZero() {
		return entry.UpdatedAt
	}
	return entry.CreatedAt
}

func laterTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
