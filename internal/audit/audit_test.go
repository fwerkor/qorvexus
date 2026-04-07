package audit

import (
	"path/filepath"
	"testing"
)

func TestLoggerPersistsContextFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger := New(path)

	err := logger.Append(Entry{
		Action:  "send_social_message",
		Actor:   "owner-1",
		Channel: "telegram",
		Trust:   "owner",
		Status:  "ok",
		Target:  "thread-1",
	})
	if err != nil {
		t.Fatalf("append audit entry: %v", err)
	}

	items, err := logger.Recent(10)
	if err != nil {
		t.Fatalf("read recent audit entries: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(items))
	}
	if items[0].Channel != "telegram" || items[0].Trust != "owner" || items[0].Actor != "owner-1" {
		t.Fatalf("expected context fields to persist, got %+v", items[0])
	}
}
