package social

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelegramPollerConsumesUpdates(t *testing.T) {
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

	poller := NewTelegramPoller("test-token", 1, 10*time.Millisecond)
	poller.apiBaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan Envelope, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- poller.Run(ctx, func(_ context.Context, env Envelope) error {
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

func TestTelegramPollerDeleteWebhookFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"failed"}`))
	}))
	defer srv.Close()

	poller := NewTelegramPoller("test-token", 1, 0)
	poller.apiBaseURL = srv.URL

	err := poller.deleteWebhook(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected deleteWebhook failure, got %v", err)
	}
}

func TestTelegramWebhookURL(t *testing.T) {
	got := TelegramWebhookURL("https://example.com/base/", "webhooks/telegram")
	want := "https://example.com/base/webhooks/telegram"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestTelegramPollerUsesBotPath(t *testing.T) {
	sawPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	poller := NewTelegramPoller("abc", 1, 0)
	poller.apiBaseURL = srv.URL
	if err := poller.deleteWebhook(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sawPath != fmt.Sprintf("/bot%s/deleteWebhook", "abc") {
		t.Fatalf("unexpected path: %s", sawPath)
	}
}
