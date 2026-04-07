package social

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookRegistryAndTelegramAdapter(t *testing.T) {
	registry := NewRegistry()
	adapter := NewTelegramWebhookAdapter("/webhooks/telegram", "secret")
	registry.RegisterWebhook(adapter)

	webhooks := registry.Webhooks()
	if len(webhooks) != 1 || webhooks[0].Path() != "/webhooks/telegram" {
		t.Fatalf("unexpected webhook registry contents: %+v", webhooks)
	}

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
