package socialplugin

import (
	"context"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
)

type stubPlugin struct {
	channel string
	called  bool
}

func (p *stubPlugin) Channel() string { return p.channel }

func (p *stubPlugin) Setup(_ config.SocialConfig, registry *social.Registry, _ func(context.Context, social.Envelope) error) ([]BackgroundRunner, error) {
	p.called = true
	registry.Register(social.NewFileConnector(p.channel, tTempPath))
	return nil, nil
}

var tTempPath = "/tmp/plugin-outbox.jsonl"

func TestManagerUsesRegisteredPluginAndFallback(t *testing.T) {
	manager := NewManager()
	plug := &stubPlugin{channel: "telegram"}
	manager.Register(plug)
	registry := social.NewRegistry()
	cfg := config.SocialConfig{
		Enabled:         true,
		AllowedChannels: []string{"telegram", "slack"},
	}

	runners, err := manager.Setup(cfg, registry, "/tmp", func(context.Context, social.Envelope) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 0 {
		t.Fatalf("expected no background runners, got %d", len(runners))
	}
	if !plug.called {
		t.Fatal("expected plugin setup to be called")
	}
	listed := registry.List()
	if len(listed) != 2 {
		t.Fatalf("expected two connectors, got %#v", listed)
	}
}
