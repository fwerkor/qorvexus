package discord

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
)

func TestPluginRegistersConnector(t *testing.T) {
	plugin := New()
	registry := social.NewRegistry()
	cfg := config.SocialConfig{
		Enabled:         true,
		AllowedChannels: []string{"discord"},
		Discord: config.DiscordConfig{
			BotToken: "token",
		},
	}

	runners, err := plugin.Setup(cfg, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 0 {
		t.Fatalf("expected no background runners, got %d", len(runners))
	}
	listed := registry.List()
	if len(listed) != 1 || listed[0] != "discord" {
		t.Fatalf("unexpected registry contents: %#v", listed)
	}
}

func TestDiscordConnectorSendUsesThreadID(t *testing.T) {
	var authHeader string
	var requestPath string
	var requestBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		requestPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		requestBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	connector := NewConnector("token-123", srv.URL, "")
	out, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel:  "discord",
		ThreadID: "channel-1",
		Text:     "hello discord",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "channel-1") {
		t.Fatalf("unexpected send response: %s", out)
	}
	if authHeader != "Bot token-123" {
		t.Fatalf("unexpected auth header: %s", authHeader)
	}
	if requestPath != "/channels/channel-1/messages" {
		t.Fatalf("unexpected request path: %s", requestPath)
	}
	if !strings.Contains(requestBody, "hello discord") {
		t.Fatalf("unexpected request body: %s", requestBody)
	}
}

func TestDiscordConnectorUsesDefaultChannelID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/default-channel/messages" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	connector := NewConnector("token-123", srv.URL, "default-channel")
	_, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel: "discord",
		Text:    "fallback",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDiscordConnectorRequiresChannel(t *testing.T) {
	connector := NewConnector("token-123", "https://discord.example", "")
	_, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel: "discord",
		Text:    "missing target",
	})
	if err == nil {
		t.Fatal("expected missing channel error")
	}
}
