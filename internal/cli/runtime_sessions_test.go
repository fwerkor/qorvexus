package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"qorvexus/internal/session"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
)

func TestListSavedSessionsRequiresOwnerOrSystemContext(t *testing.T) {
	app := &appRuntime{sessions: session.NewStore(t.TempDir())}
	if _, err := app.ListSavedSessions(context.Background(), 10, "", ""); err == nil {
		t.Fatal("expected permission error")
	}
}

func TestListSavedSessionsAndGetSessionView(t *testing.T) {
	root := t.TempDir()
	store := session.NewStore(root)
	now := time.Now().UTC()
	if err := store.Save(&session.State{
		ID:    "telegram-1",
		Model: "primary",
		Context: types.ConversationContext{
			Channel:    "telegram",
			ThreadID:   "chat-1",
			SenderID:   "alice",
			SenderName: "Alice",
			Trust:      types.TrustExternal,
		},
		CreatedAt: now,
		UpdatedAt: now,
		Messages: []types.Message{
			{Role: types.RoleSystem, Content: "system prompt"},
			{Role: types.RoleUser, Content: "hello from telegram"},
			{Role: types.RoleAssistant, Content: "reply from telegram"},
			{Role: types.RoleTool, Content: strings.Repeat("tool output ", 400)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&session.State{
		ID:    "discord-1",
		Model: "primary",
		Context: types.ConversationContext{
			Channel:  "discord",
			ThreadID: "chan-1",
			SenderID: "bob",
			Trust:    types.TrustTrusted,
		},
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "hello from discord"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	app := &appRuntime{sessions: store}
	ctx := tool.WithConversationContext(context.Background(), types.ConversationContext{
		Channel:  "web",
		SenderID: "owner",
		Trust:    types.TrustOwner,
		IsOwner:  true,
	})

	listRaw, err := app.ListSavedSessions(ctx, 10, "telegram", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listRaw, "telegram-1") || strings.Contains(listRaw, "discord-1") {
		t.Fatalf("unexpected session list output: %s", listRaw)
	}
	if !strings.Contains(listRaw, "reply from telegram") {
		t.Fatalf("expected preview in session list, got %s", listRaw)
	}

	viewRaw, err := app.GetSessionView(ctx, "telegram-1", 10, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(viewRaw, "system prompt") {
		t.Fatalf("expected system messages to be hidden by default, got %s", viewRaw)
	}
	if strings.Contains(viewRaw, "tool output") {
		t.Fatalf("expected tool messages to be hidden by default, got %s", viewRaw)
	}
	if !strings.Contains(viewRaw, "hello from telegram") || !strings.Contains(viewRaw, "reply from telegram") {
		t.Fatalf("expected user and assistant messages in session view, got %s", viewRaw)
	}

	fullRaw, err := app.GetSessionView(ctx, "telegram-1", 10, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fullRaw, "system prompt") {
		t.Fatalf("expected system prompt in expanded session view, got %s", fullRaw)
	}
	if !strings.Contains(fullRaw, "tool output") {
		t.Fatalf("expected tool output in expanded session view, got %s", fullRaw)
	}
}
