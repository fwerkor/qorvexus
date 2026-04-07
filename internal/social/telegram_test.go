package social

import "testing"

func TestTelegramEnvelope(t *testing.T) {
	env, ok := TelegramEnvelope(TelegramUpdate{
		UpdateID: 100,
		Message: &TelegramMessage{
			MessageID: 10,
			Text:      "hello",
			Chat: TelegramChat{
				ID:   12345,
				Type: "private",
			},
			From: &TelegramUser{
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
