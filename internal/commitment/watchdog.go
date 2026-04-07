package commitment

import (
	"strings"
	"time"
)

type ScanResult struct {
	Overdue []Entry `json:"overdue"`
	Stale   []Entry `json:"stale"`
}

func Scan(entries []Entry, now time.Time) ScanResult {
	var result ScanResult
	for _, entry := range entries {
		if entry.Status != StatusOpen {
			continue
		}
		if ShouldMarkOverdue(entry, now) {
			result.Overdue = append(result.Overdue, entry)
			continue
		}
		if IsStale(entry, now) {
			result.Stale = append(result.Stale, entry)
		}
	}
	return result
}

func ShouldMarkOverdue(entry Entry, now time.Time) bool {
	if entry.Status != StatusOpen {
		return false
	}
	age := now.Sub(entry.CreatedAt)
	hint := strings.ToLower(strings.TrimSpace(entry.DueHint))
	switch hint {
	case "today":
		return age >= 24*time.Hour
	case "tomorrow":
		return age >= 48*time.Hour
	case "this week":
		return age >= 7*24*time.Hour
	case "next week":
		return age >= 14*24*time.Hour
	case "this month":
		return age >= 31*24*time.Hour
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		return age >= 7*24*time.Hour
	default:
		if hint != "" {
			return age >= 10*24*time.Hour
		}
		return false
	}
}

func IsStale(entry Entry, now time.Time) bool {
	if entry.Status != StatusOpen {
		return false
	}
	if entry.DueHint != "" {
		return now.Sub(entry.UpdatedAt) >= 72*time.Hour
	}
	return now.Sub(entry.UpdatedAt) >= 7*24*time.Hour
}
