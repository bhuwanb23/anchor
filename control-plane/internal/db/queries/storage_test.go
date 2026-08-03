package queries

import (
	"testing"
)

func TestEstimateDaysUntilFull(t *testing.T) {
	history := []BackupStorageHistoryEntry{
		{SizeBytes: 100, RecordedAt: "2026-01-01"},
		{SizeBytes: 200, RecordedAt: "2026-01-02"},
		{SizeBytes: 300, RecordedAt: "2026-01-03"},
	}
	// growth 200 over 2 days = 100/day; remaining 700 → 7 days
	days := EstimateDaysUntilFull(history, 300, 1000)
	if days != 7 {
		t.Errorf("EstimateDaysUntilFull = %d, want 7", days)
	}
}

func TestEstimateDaysUntilFull_AlreadyFull(t *testing.T) {
	days := EstimateDaysUntilFull(nil, 1000, 1000)
	if days != 0 {
		t.Errorf("got %d, want 0", days)
	}
}

func TestEstimateDaysUntilFull_NoGrowth(t *testing.T) {
	history := []BackupStorageHistoryEntry{
		{SizeBytes: 500, RecordedAt: "a"},
		{SizeBytes: 500, RecordedAt: "b"},
	}
	days := EstimateDaysUntilFull(history, 500, 1000)
	if days != -1 {
		t.Errorf("got %d, want -1", days)
	}
}
