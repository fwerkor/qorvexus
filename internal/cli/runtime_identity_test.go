package cli

import (
	"context"
	"path/filepath"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/social"
	"qorvexus/internal/tool"
	"qorvexus/internal/types"
)

func TestGrantOwnerIdentityRequiresOwnerContext(t *testing.T) {
	app := &appRuntime{
		social: social.NewGateway(config.SocialConfig{}, config.IdentityConfig{}, social.NewIdentityStore(filepath.Join(t.TempDir(), "identity_state.json")), nil),
	}
	if _, err := app.GrantOwnerIdentity(context.Background(), "telegram", "alice", "Alice"); err == nil {
		t.Fatal("expected permission error")
	}
}

func TestGrantOwnerIdentityPersistsClassification(t *testing.T) {
	root := t.TempDir()
	store := social.NewIdentityStore(filepath.Join(root, "identity_state.json"))
	gateway := social.NewGateway(config.SocialConfig{}, config.IdentityConfig{
		OwnerIDs: []string{"primary-owner"},
	}, store, nil)
	app := &appRuntime{social: gateway}

	ctx := tool.WithConversationContext(context.Background(), types.ConversationContext{
		Channel:    "web",
		SenderID:   "primary-owner",
		SenderName: "Primary",
		Trust:      types.TrustOwner,
		IsOwner:    true,
	})
	if _, err := app.GrantOwnerIdentity(ctx, "telegram", "alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	classified := gateway.Classify("telegram", "thread-a", "alice", "Alice")
	if !classified.IsOwner || classified.Trust != types.TrustOwner {
		t.Fatalf("expected granted identity to classify as owner, got %+v", classified)
	}
}
