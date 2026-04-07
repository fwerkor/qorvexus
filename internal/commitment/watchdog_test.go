package commitment

import (
	"testing"
	"time"
)

func TestShouldMarkOverdueByDueHint(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	entry := Entry{
		Status:    StatusOpen,
		DueHint:   "tomorrow",
		CreatedAt: now.Add(-49 * time.Hour),
		UpdatedAt: now.Add(-49 * time.Hour),
	}
	if !ShouldMarkOverdue(entry, now) {
		t.Fatalf("expected tomorrow commitment to become overdue")
	}
}

func TestScanSeparatesOverdueAndStale(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	result := Scan([]Entry{
		{
			ID:        "overdue-1",
			Status:    StatusOpen,
			DueHint:   "today",
			CreatedAt: now.Add(-25 * time.Hour),
			UpdatedAt: now.Add(-25 * time.Hour),
		},
		{
			ID:        "stale-1",
			Status:    StatusOpen,
			CreatedAt: now.Add(-8 * 24 * time.Hour),
			UpdatedAt: now.Add(-8 * 24 * time.Hour),
		},
		{
			ID:        "done-1",
			Status:    StatusCompleted,
			CreatedAt: now.Add(-30 * 24 * time.Hour),
			UpdatedAt: now.Add(-2 * time.Hour),
		},
	}, now)

	if len(result.Overdue) != 1 || result.Overdue[0].ID != "overdue-1" {
		t.Fatalf("unexpected overdue result: %+v", result.Overdue)
	}
	if len(result.Stale) != 1 || result.Stale[0].ID != "stale-1" {
		t.Fatalf("unexpected stale result: %+v", result.Stale)
	}
}

func TestShouldQueueReviewUsesCooldownByStatus(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)

	overdue := Entry{
		Status:       StatusOverdue,
		LastReviewAt: now.Add(-23 * time.Hour),
	}
	if ShouldQueueReview(overdue, now) {
		t.Fatalf("expected overdue commitment to respect 24h cooldown")
	}

	stale := Entry{
		Status:       StatusOpen,
		DueHint:      "next week",
		LastReviewAt: now.Add(-49 * time.Hour),
	}
	if !ShouldQueueReview(stale, now) {
		t.Fatalf("expected due-hint commitment to allow requeue after cooldown")
	}
}

func TestNextEscalationLevelIncreasesForLongOverdue(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	entry := Entry{
		Status:          StatusOverdue,
		EscalationLevel: 1,
		LastReviewAt:    now.Add(-80 * time.Hour),
	}
	if got := NextEscalationLevel(entry, now); got != 2 {
		t.Fatalf("expected escalation level 2, got %d", got)
	}
}
