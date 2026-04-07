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
