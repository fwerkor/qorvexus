package social

import (
	"context"
	"path/filepath"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

type stubHandler struct{}

func (stubHandler) HandleEnvelope(context.Context, Envelope) (string, error) { return "ok", nil }

func TestGatewayClassifiesOwner(t *testing.T) {
	g := NewGateway(
		config.SocialConfig{InboxFile: filepath.Join(t.TempDir(), "inbox.jsonl")},
		config.IdentityConfig{OwnerIDs: []string{"owner-1"}},
		stubHandler{},
	)
	ctx := g.Classify("telegram", "thread", "owner-1", "Alice")
	if ctx.Trust != types.TrustOwner || !ctx.IsOwner {
		t.Fatalf("expected owner context, got %+v", ctx)
	}
}
