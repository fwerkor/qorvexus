package self

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type BacklogEntry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type Manager struct {
	skillsDir   string
	backlogFile string
	mu          sync.Mutex
}

func NewManager(skillsDir string, backlogFile string) *Manager {
	return &Manager{skillsDir: skillsDir, backlogFile: backlogFile}
}

func (m *Manager) UpsertSkill(name string, description string, body string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	skillDir := filepath.Join(m.skillsDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", err
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n", name, description, body)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		return "", err
	}
	return filepath.Join(skillDir, "SKILL.md"), nil
}

func (m *Manager) AppendBacklog(entry BacklogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("self-%d", time.Now().UTC().UnixNano())
	}
	if entry.Status == "" {
		entry.Status = "open"
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(m.backlogFile), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(m.backlogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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

func (m *Manager) ListBacklog(limit int) ([]BacklogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := os.Open(m.backlogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var items []BacklogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry BacklogEntry
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
