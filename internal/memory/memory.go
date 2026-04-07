package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Source    string    `json:"source,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (e Entry) HasTag(tag string) bool {
	for _, item := range e.Tags {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(tag)) {
			return true
		}
	}
	return false
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("mem-%d", time.Now().UTC().UnixNano())
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	return err
}

func (s *Store) Search(query string, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	terms := tokenize(query)
	type scored struct {
		entry Entry
		score int
	}
	var results []scored
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		score := scoreEntry(entry, terms)
		if score > 0 || len(terms) == 0 {
			results = append(results, scored{entry: entry, score: score})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score == results[j].score {
			return results[i].entry.CreatedAt.After(results[j].entry.CreatedAt)
		}
		return results[i].score > results[j].score
	})
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
	entries, err := s.Search("", 0)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.HasTag(tag) {
			out = append(out, entry)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
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

func scoreEntry(entry Entry, terms []string) int {
	haystack := strings.ToLower(entry.Content + " " + entry.Source + " " + strings.Join(entry.Tags, " "))
	score := 0
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			score++
		}
	}
	return score
}
