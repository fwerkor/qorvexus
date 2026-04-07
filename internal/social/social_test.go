package social

import (
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

func TestGatewayClassifyOwnerTrustedExternal(t *testing.T) {
	gateway := NewGateway(config.SocialConfig{}, config.IdentityConfig{
		OwnerIDs:     []string{"owner-1"},
		OwnerAliases: []string{"Boss"},
		TrustedIDs:   []string{"trusted-1"},
	}, nil)

	owner := gateway.Classify("telegram", "thread-a", "owner-1", "Someone")
	if !owner.IsOwner || owner.Trust != types.TrustOwner {
		t.Fatalf("expected owner classification, got %+v", owner)
	}

	aliasOwner := gateway.Classify("telegram", "thread-a", "", "boss")
	if !aliasOwner.IsOwner || aliasOwner.Trust != types.TrustOwner {
		t.Fatalf("expected alias owner classification, got %+v", aliasOwner)
	}

	trusted := gateway.Classify("telegram", "thread-a", "trusted-1", "Colleague")
	if trusted.Trust != types.TrustTrusted || !trusted.WorkingForUser {
		t.Fatalf("expected trusted classification, got %+v", trusted)
	}

	external := gateway.Classify("telegram", "thread-a", "external-1", "Prospect")
	if external.Trust != types.TrustExternal || !external.WorkingForUser || external.IsOwner {
		t.Fatalf("expected external classification, got %+v", external)
	}
}
