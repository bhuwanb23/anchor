package ws

import (
	"testing"
	"time"
)

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
		{7, 60 * time.Second}, // capped
		{10, 60 * time.Second},
	}
	for _, tt := range tests {
		got := BackoffDuration(1, tt.attempt)
		if got != tt.want {
			t.Errorf("BackoffDuration(1, %d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestApplyJitter_Bounds(t *testing.T) {
	base := 10 * time.Second
	for i := 0; i < 50; i++ {
		got := ApplyJitter(base)
		// ±20% => 8s..12s, with floor of 1s
		if got < 8*time.Second || got > 12*time.Second {
			t.Fatalf("ApplyJitter out of bounds: %v", got)
		}
	}
}

func TestAuthError(t *testing.T) {
	err := &AuthError{StatusCode: 401, Err: errSentinel{}}
	if err.Error() == "" {
		t.Fatal("expected error string")
	}
	if err.Unwrap() == nil {
		t.Fatal("expected unwrap")
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "denied" }
