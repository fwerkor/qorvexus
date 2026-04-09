package social

import (
	"path/filepath"
	"testing"

	"qorvexus/internal/config"
	"qorvexus/internal/types"
)

func TestGatewayClassifyOwnerTrustedExternal(t *testing.T) {
	gateway := NewGateway(config.SocialConfig{}, config.IdentityConfig{
		OwnerIDs:     []string{"owner-1"},
		OwnerAliases: []string{"Boss"},
		TrustedIDs:   []string{"trusted-1"},
	}, nil, nil)

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

func TestGatewayClaimsFirstOwnerAndPersistsIt(t *testing.T) {
	store := NewIdentityStore(filepath.Join(t.TempDir(), "identity_state.json"))
	gateway := NewGateway(config.SocialConfig{}, config.IdentityConfig{
		OwnerAliases: []string{"owner"},
	}, store, nil)

	first := gateway.Classify("telegram", "thread-a", "owner-chat", "Alice")
	if !first.IsOwner || first.Trust != types.TrustOwner {
		t.Fatalf("expected first social contact to bootstrap owner, got %+v", first)
	}

	again := gateway.Classify("telegram", "thread-b", "owner-chat", "Alice")
	if !again.IsOwner || again.Trust != types.TrustOwner {
		t.Fatalf("expected persisted owner classification, got %+v", again)
	}

	other := gateway.Classify("telegram", "thread-c", "other-chat", "Bob")
	if other.IsOwner || other.Trust != types.TrustExternal {
		t.Fatalf("expected later unknown contact to stay external, got %+v", other)
	}
}

func TestGatewayGrantOwnerIdentity(t *testing.T) {
	store := NewIdentityStore(filepath.Join(t.TempDir(), "identity_state.json"))
	gateway := NewGateway(config.SocialConfig{}, config.IdentityConfig{
		OwnerIDs: []string{"primary-owner"},
	}, store, nil)

	before := gateway.Classify("telegram", "thread-a", "secondary-owner", "Alice")
	if before.IsOwner || before.Trust != types.TrustExternal {
		t.Fatalf("expected secondary contact to start external, got %+v", before)
	}

	if _, err := gateway.GrantOwnerIdentity("telegram", "secondary-owner", "Alice"); err != nil {
		t.Fatal(err)
	}

	after := gateway.Classify("telegram", "thread-a", "secondary-owner", "Alice")
	if !after.IsOwner || after.Trust != types.TrustOwner {
		t.Fatalf("expected granted contact to become owner, got %+v", after)
	}
}
