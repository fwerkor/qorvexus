package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
)

func TestPluginRegistersWebhookAdapterInWebhookMode(t *testing.T) {
	plugin := New()
	registry := social.NewRegistry()
	cfg := config.SocialConfig{
		Enabled:         true,
		AllowedChannels: []string{"telegram"},
		Telegram: config.TelegramConfig{
			Mode:          "webhook",
			WebhookPath:   "/webhooks/telegram",
			WebhookSecret: "secret",
		},
	}

	runners, err := plugin.Setup(cfg, registry, func(context.Context, social.Envelope) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 0 {
		t.Fatalf("expected no background runners, got %d", len(runners))
	}
	webhooks := registry.Webhooks()
	if len(webhooks) != 1 || webhooks[0].Path() != "/webhooks/telegram" {
		t.Fatalf("unexpected webhook registry contents: %+v", webhooks)
	}
}

func TestWebhookAdapterParsesTelegramUpdate(t *testing.T) {
	adapter := NewWebhookAdapter("/webhooks/telegram", "secret")
	req := httptest.NewRequest("POST", "/webhooks/telegram", strings.NewReader(`{
		"update_id": 1,
		"message": {
			"message_id": 2,
			"text": "hello",
			"chat": {"id": 123, "type": "private"},
			"from": {"id": 456, "first_name": "Ada", "last_name": "Lovelace"}
		}
	}`))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")

	env, ok, err := adapter.ParseWebhook(req)
	if err != nil {
		t.Fatalf("parse webhook: %v", err)
	}
	if !ok || env.Channel != "telegram" || env.ThreadID != "123" || env.SenderID != "456" {
		t.Fatalf("unexpected parsed envelope: %+v", env)
	}
}

func TestEnvelope(t *testing.T) {
	env, ok := Envelope(Update{
		UpdateID: 100,
		Message: &Message{
			MessageID: 10,
			Text:      "hello",
			Chat: Chat{
				ID:   12345,
				Type: "private",
			},
			From: &User{
				ID:        999,
				FirstName: "Ada",
				LastName:  "Lovelace",
			},
		},
	})
	if !ok {
		t.Fatalf("expected telegram update to produce envelope")
	}
	if env.Channel != "telegram" || env.ThreadID != "12345" || env.SenderID != "999" {
		t.Fatalf("unexpected telegram envelope: %+v", env)
	}
	if env.SenderName != "Ada Lovelace" || env.Text != "hello" {
		t.Fatalf("unexpected sender/text mapping: %+v", env)
	}
}

func TestPollerConsumesUpdates(t *testing.T) {
	var deleteWebhookCalls int
	var getUpdatesCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/deleteWebhook"):
			deleteWebhookCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			getUpdatesCalls++
			w.Header().Set("Content-Type", "application/json")
			if getUpdatesCalls == 1 {
				_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":41,"message":{"message_id":7,"text":"hello","chat":{"id":123,"type":"private"},"from":{"id":456,"first_name":"Ada"}}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	poller := NewPoller("test-token", 1, 10*time.Millisecond)
	poller.apiBaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan social.Envelope, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- poller.Run(ctx, func(_ context.Context, env social.Envelope) error {
			received <- env
			cancel()
			return nil
		})
	}()

	select {
	case env := <-received:
		if env.Channel != "telegram" || env.ThreadID != "123" || env.SenderID != "456" {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for polled telegram message")
	}

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("unexpected poller error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for poller shutdown")
	}

	if deleteWebhookCalls != 1 {
		t.Fatalf("expected deleteWebhook once, got %d", deleteWebhookCalls)
	}
	if getUpdatesCalls == 0 {
		t.Fatal("expected getUpdates to be called")
	}
	if poller.offset != 42 {
		t.Fatalf("expected offset to advance to 42, got %d", poller.offset)
	}
}

func TestPollerDeleteWebhookFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"failed"}`))
	}))
	defer srv.Close()

	poller := NewPoller("test-token", 1, 0)
	poller.apiBaseURL = srv.URL

	err := poller.deleteWebhook(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected deleteWebhook failure, got %v", err)
	}
}

func TestWebhookURL(t *testing.T) {
	got := WebhookURL("https://example.com/base/", "webhooks/telegram")
	want := "https://example.com/base/webhooks/telegram"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestPollerUsesBotPath(t *testing.T) {
	sawPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	poller := NewPoller("abc", 1, 0)
	poller.apiBaseURL = srv.URL
	if err := poller.deleteWebhook(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sawPath != fmt.Sprintf("/bot%s/deleteWebhook", "abc") {
		t.Fatalf("unexpected path: %s", sawPath)
	}
}
