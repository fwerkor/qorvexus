package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	ID        string         `json:"id"`
	Action    string         `json:"action"`
	Actor     string         `json:"actor,omitempty"`
	Channel   string         `json:"channel,omitempty"`
	Trust     string         `json:"trust,omitempty"`
	Target    string         `json:"target,omitempty"`
	Status    string         `json:"status,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type Logger struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Logger {
	return &Logger{path: path}
}

func (l *Logger) Append(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("audit-%d", time.Now().UTC().UnixNano())
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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

func (l *Logger) Recent(limit int) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
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
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}
