package social

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"qorvexus/internal/types"
)

type OutboundMessage struct {
	ID        string                    `json:"id"`
	Channel   string                    `json:"channel"`
	ThreadID  string                    `json:"thread_id,omitempty"`
	Recipient string                    `json:"recipient,omitempty"`
	Text      string                    `json:"text"`
	Context   types.ConversationContext `json:"context,omitempty"`
	CreatedAt time.Time                 `json:"created_at"`
}

type Connector interface {
	Name() string
	Send(ctx context.Context, msg OutboundMessage) (string, error)
}

type Registry struct {
	mu         sync.RWMutex
	connectors map[string]Connector
}

func NewRegistry() *Registry {
	return &Registry{connectors: map[string]Connector{}}
}

func (r *Registry) Register(connector Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectors[connector.Name()] = connector
}

func (r *Registry) Send(ctx context.Context, channel string, msg OutboundMessage) (string, error) {
	r.mu.RLock()
	connector, ok := r.connectors[channel]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("social connector %q not found", channel)
	}
	return connector.Send(ctx, msg)
}

type FileConnector struct {
	channel string
	path    string
	mu      sync.Mutex
}

func NewFileConnector(channel string, path string) *FileConnector {
	return &FileConnector{channel: channel, path: path}
}

func (c *FileConnector) Name() string { return c.channel }

func (c *FileConnector) Send(_ context.Context, msg OutboundMessage) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("out-%d", time.Now().UTC().UnixNano())
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(c.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return "", err
	}
	return fmt.Sprintf("queued outbound %s message to %s", c.channel, msg.Recipient), nil
}
