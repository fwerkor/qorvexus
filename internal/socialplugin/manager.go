package socialplugin

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
)

type BackgroundRunner interface {
	Name() string
	Run(ctx context.Context) error
}

type Plugin interface {
	Channel() string
	Setup(cfg config.SocialConfig, registry *social.Registry, handle func(context.Context, social.Envelope) error) ([]BackgroundRunner, error)
}

type Manager struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func NewManager() *Manager {
	return &Manager{plugins: map[string]Plugin{}}
}

func (m *Manager) Register(plugin Plugin) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plugins[plugin.Channel()] = plugin
}

func (m *Manager) Setup(cfg config.SocialConfig, registry *social.Registry, dataDir string, handle func(context.Context, social.Envelope) error) ([]BackgroundRunner, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var runners []BackgroundRunner
	for _, channel := range cfg.AllowedChannels {
		if plugin, ok := m.plugins[channel]; ok {
			pluginRunners, err := plugin.Setup(cfg, registry, handle)
			if err != nil {
				return nil, fmt.Errorf("setup social plugin %s: %w", channel, err)
			}
			runners = append(runners, pluginRunners...)
			continue
		}
		registry.Register(social.NewFileConnector(channel, filepath.Join(dataDir, "social_outbox_"+channel+".jsonl")))
	}
	sort.Slice(runners, func(i, j int) bool { return runners[i].Name() < runners[j].Name() })
	return runners, nil
}
