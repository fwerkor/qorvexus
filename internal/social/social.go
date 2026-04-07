package social

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

type Envelope struct {
	ID         string                    `json:"id"`
	Channel    string                    `json:"channel"`
	ThreadID   string                    `json:"thread_id,omitempty"`
	SenderID   string                    `json:"sender_id,omitempty"`
	SenderName string                    `json:"sender_name,omitempty"`
	Text       string                    `json:"text"`
	Images     []string                  `json:"images,omitempty"`
	Context    types.ConversationContext `json:"context"`
	ReceivedAt time.Time                 `json:"received_at"`
	Response   string                    `json:"response,omitempty"`
}

type Handler interface {
	HandleEnvelope(ctx context.Context, env Envelope) (string, error)
}

type Gateway struct {
	cfg     config.SocialConfig
	idCfg   config.IdentityConfig
	path    string
	handler Handler
	mu      sync.Mutex
}

func NewGateway(cfg config.SocialConfig, identity config.IdentityConfig, handler Handler) *Gateway {
	return &Gateway{
		cfg:     cfg,
		idCfg:   identity,
		path:    cfg.InboxFile,
		handler: handler,
	}
}

func (g *Gateway) Receive(ctx context.Context, env Envelope) (string, error) {
	env.Context = g.Classify(env.Channel, env.ThreadID, env.SenderID, env.SenderName)
	env.ReceivedAt = time.Now().UTC()
	if env.ID == "" {
		env.ID = fmt.Sprintf("social-%d", env.ReceivedAt.UnixNano())
	}
	if err := g.append(env); err != nil {
		return "", err
	}
	if g.handler == nil {
		return "", nil
	}
	out, err := g.handler.HandleEnvelope(ctx, env)
	if err != nil {
		return "", err
	}
	env.Response = out
	_ = g.append(env)
	return out, nil
}

func (g *Gateway) Classify(channel string, threadID string, senderID string, senderName string) types.ConversationContext {
	ctx := types.ConversationContext{
		Channel:      channel,
		ThreadID:     threadID,
		SenderID:     senderID,
		SenderName:   senderName,
		ReplyAsAgent: true,
	}
	if contains(g.idCfg.OwnerIDs, senderID) || containsFold(g.idCfg.OwnerAliases, senderName) {
		ctx.IsOwner = true
		ctx.Trust = types.TrustOwner
		return ctx
	}
	if contains(g.idCfg.TrustedIDs, senderID) {
		ctx.Trust = types.TrustTrusted
		ctx.WorkingForUser = true
		return ctx
	}
	ctx.Trust = types.TrustExternal
	ctx.WorkingForUser = true
	return ctx
}

func (g *Gateway) Recent(limit int) ([]Envelope, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, err := os.Open(g.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var items []Envelope
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var env Envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err == nil {
			items = append(items, env)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	return items[max(0, len(items)-limit):], nil
}

func (g *Gateway) append(env Envelope) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(g.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	return err
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(target) && target != "" {
			return true
		}
	}
	return false
}

func containsFold(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(target)) && target != "" {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
