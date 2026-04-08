package social

import (
	"path/filepath"
	"testing"
	"time"

	"qorvexus/internal/types"
)

func TestDraftStoreAppendListAndUpdate(t *testing.T) {
	store := NewDraftStore(filepath.Join(t.TempDir(), "drafts.json"))
	draft, err := store.Append(Draft{
		Channel:      "telegram",
		ThreadID:     "thread-1",
		Recipient:    "lead-1",
		ContactKey:   ContactKey("telegram", "lead-1", "Lead"),
		Counterparty: "Lead",
		Text:         "Here is a proposed follow-up.",
		Reason:       "external draft",
		Hold:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != DraftStatusHeld {
		t.Fatalf("expected held draft, got %s", draft.Status)
	}
	items, err := store.List(10, string(DraftStatusHeld))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(items))
	}
	updated, err := store.Update(draft.ID, func(item *Draft) error {
		item.Status = DraftStatusReady
		item.ReviewedBy = "operator"
		item.ReviewedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != DraftStatusReady || updated.ReviewedBy != "operator" {
		t.Fatalf("unexpected updated draft: %+v", updated)
	}
}

func TestFollowUpStoreUpsertMergesScope(t *testing.T) {
	store := NewFollowUpStore(filepath.Join(t.TempDir(), "followups.json"))
	first, err := store.Upsert(FollowUp{
		ScopeKey:          "telegram|thread-1|lead-1|reply",
		Channel:           "telegram",
		ThreadID:          "thread-1",
		ContactKey:        "telegram:lead-1",
		ContactName:       "Lead",
		Summary:           "Review reply",
		RecommendedAction: "prepare_reply",
		Priority:          "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Upsert(FollowUp{
		ScopeKey:        "telegram|thread-1|lead-1|reply",
		Reason:          "The contact asked a new follow-up question.",
		RelatedOutboxID: "outbox-1",
		Status:          FollowUpStatusHeld,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected upsert to reuse same follow-up id, got %s and %s", first.ID, second.ID)
	}
	if second.RelatedOutboxID != "outbox-1" || second.Status != FollowUpStatusHeld {
		t.Fatalf("unexpected merged follow-up: %+v", second)
	}
}

func TestGraphStoreRecordsInteractionsAndBoundary(t *testing.T) {
	store := NewGraphStore(filepath.Join(t.TempDir(), "graph.json"))
	node, err := store.RecordInteraction(Interaction{
		Kind:        InteractionInbound,
		Channel:     "telegram",
		ThreadID:    "thread-1",
		ContactID:   "trusted-1",
		ContactName: "Colleague",
		Trust:       types.TrustTrusted,
		Message:     "Can you share an update?",
		OccurredAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.InboundCount != 1 || node.InteractionCount != 1 {
		t.Fatalf("unexpected initial node stats: %+v", node)
	}
	for i := 0; i < 2; i++ {
		if _, err := store.RecordInteraction(Interaction{
			Kind:        InteractionOutbound,
			Channel:     "telegram",
			ThreadID:    "thread-1",
			ContactID:   "trusted-1",
			ContactName: "Colleague",
			Trust:       types.TrustTrusted,
			Message:     "Sharing an update.",
			OccurredAt:  time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := store.Get(ContactKey("telegram", "trusted-1", "Colleague"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected stored contact")
	}
	if got.Boundary != BoundaryTrustedAutopilot {
		t.Fatalf("expected trusted contact to reach autopilot boundary, got %s", got.Boundary)
	}
}

func TestDecideOutboundAuthorizationCanHoldHighRiskExternalReply(t *testing.T) {
	decision := DecideOutboundAuthorization(Envelope{
		Text: "Can you send a proposal tomorrow?",
		Context: types.ConversationContext{
			Trust: types.TrustExternal,
		},
	}, "Sure, I will send a proposal tomorrow.", ContactNode{
		Boundary:         BoundaryExternalReview,
		InteractionCount: 1,
	}, true, true)
	if decision.Mode != DeliveryModeHold {
		t.Fatalf("expected external business reply to be held, got %+v", decision)
	}
	if !decision.HighRisk {
		t.Fatalf("expected high risk decision, got %+v", decision)
	}
}

func TestDecideOutboundAuthorizationAllowsSilence(t *testing.T) {
	decision := DecideOutboundAuthorization(Envelope{
		Context: types.ConversationContext{
			Trust: types.TrustExternal,
		},
	}, NoReplySentinel, ContactNode{
		Boundary: BoundaryExternalReview,
	}, true, true)
	if decision.Mode != DeliveryModeSilent {
		t.Fatalf("expected silent delivery mode, got %+v", decision)
	}
}
