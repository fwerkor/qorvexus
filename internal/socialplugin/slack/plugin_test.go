package slack

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
		AllowedChannels: []string{"slack"},
		Slack: config.SlackConfig{
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
	if len(listed) != 1 || listed[0] != "slack" {
		t.Fatalf("unexpected registry contents: %#v", listed)
	}
}

func TestSlackConnectorSendUsesThreadID(t *testing.T) {
	var authHeader string
	var requestPath string
	var requestBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		requestPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		requestBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"1.0"}`))
	}))
	defer srv.Close()

	connector := NewConnector("xoxb-test", srv.URL, "")
	out, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel:  "slack",
		ThreadID: "C123",
		Text:     "hello slack",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "C123") {
		t.Fatalf("unexpected send response: %s", out)
	}
	if authHeader != "Bearer xoxb-test" {
		t.Fatalf("unexpected auth header: %s", authHeader)
	}
	if requestPath != "/chat.postMessage" {
		t.Fatalf("unexpected request path: %s", requestPath)
	}
	if !strings.Contains(requestBody, "hello slack") || !strings.Contains(requestBody, "C123") {
		t.Fatalf("unexpected request body: %s", requestBody)
	}
}

func TestSlackConnectorUsesDefaultChannelID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "CDEFAULT") {
			t.Fatalf("unexpected request body: %s", string(body))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"1.0"}`))
	}))
	defer srv.Close()

	connector := NewConnector("xoxb-test", srv.URL, "CDEFAULT")
	_, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel: "slack",
		Text:    "fallback",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSlackConnectorRequiresChannel(t *testing.T) {
	connector := NewConnector("xoxb-test", "https://slack.example", "")
	_, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel: "slack",
		Text:    "missing target",
	})
	if err == nil {
		t.Fatal("expected missing channel error")
	}
}

func TestSlackConnectorSurfacesSlackErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()

	connector := NewConnector("xoxb-test", srv.URL, "C123")
	_, err := connector.Send(context.Background(), social.OutboundMessage{
		Channel: "slack",
		Text:    "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("expected slack API error, got %v", err)
	}
}
