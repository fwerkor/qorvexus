package socialinsight

import (
	"testing"

	"qorvexus/internal/social"
	"qorvexus/internal/types"
)

func TestAnalyzerCapturesMemoryAndFollowUpForExternalWorkInquiry(t *testing.T) {
	analyzer := NewAnalyzer()
	result := analyzer.Analyze(social.Envelope{
		Channel:    "telegram",
		ThreadID:   "thread-1",
		SenderID:   "lead-1",
		SenderName: "Prospect",
		Text:       "Hello, can we discuss a collaboration next week and schedule a call?",
		Context: types.ConversationContext{
			Trust: types.TrustExternal,
		},
	}, "Sure, I can help coordinate that.")

	if len(result.Memories) != 1 {
		t.Fatalf("expected 1 memory note, got %d", len(result.Memories))
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 follow-up task, got %d", len(result.Tasks))
	}
	if len(result.FollowUps) != 1 {
		t.Fatalf("expected 1 follow-up strategy, got %d", len(result.FollowUps))
	}
	if len(result.Commitments) != 1 {
		t.Fatalf("expected 1 commitment, got %d", len(result.Commitments))
	}
	if result.Commitments[0].DueHint != "next week" {
		t.Fatalf("expected next week due hint, got %+v", result.Commitments[0])
	}
	if result.FollowUps[0].RecommendedAction != "coordinate_meeting" {
		t.Fatalf("expected meeting follow-up action, got %+v", result.FollowUps[0])
	}
}

func TestAnalyzerSkipsFollowUpForOwnerSmallTalk(t *testing.T) {
	analyzer := NewAnalyzer()
	result := analyzer.Analyze(social.Envelope{
		Channel: "telegram",
		Text:    "Good morning",
		Context: types.ConversationContext{
			Trust:   types.TrustOwner,
			IsOwner: true,
		},
	}, "Good morning.")

	if len(result.Memories) != 0 || len(result.Tasks) != 0 {
		t.Fatalf("expected no social automation artifacts, got %+v", result)
	}
}
