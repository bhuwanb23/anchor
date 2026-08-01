package caddy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryWithBackoff_SuccessFirstAttempt(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond}
	calls := 0

	err := retryWithBackoff(context.Background(), cfg, func() error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryWithBackoff_SuccessAfterRetry(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond}
	calls := 0

	err := retryWithBackoff(context.Background(), cfg, func() error {
		calls++
		if calls < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryWithBackoff_AllAttemptsFail(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond}
	calls := 0

	err := retryWithBackoff(context.Background(), cfg, func() error {
		calls++
		return errors.New("persistent failure")
	})

	if err == nil {
		t.Fatal("expected error after all attempts")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
	if err.Error() != "persistent failure" {
		t.Errorf("expected last error, got: %v", err)
	}
}

func TestRetryWithBackoff_ContextCancelled(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := retryWithBackoff(ctx, cfg, func() error {
		calls++
		return errors.New("failure")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestRetryWithBackoff_ExponentialDelay(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 4, BaseDelay: 50 * time.Millisecond}
	calls := 0
	start := time.Now()

	err := retryWithBackoff(context.Background(), cfg, func() error {
		calls++
		return errors.New("failure")
	})

	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error")
	}
	// With delays of 50ms, 100ms, 200ms = 350ms total minimum
	if elapsed < 300*time.Millisecond {
		t.Errorf("expected at least 300ms elapsed, got %v", elapsed)
	}
}

func TestRetryWithBackoff_DefaultConfig(t *testing.T) {
	calls := 0

	err := retryWithBackoff(context.Background(), RetryConfig{}, func() error {
		calls++
		if calls < defaultMaxAttempts {
			return errors.New("failure")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != defaultMaxAttempts {
		t.Errorf("expected %d calls with default config, got %d", defaultMaxAttempts, calls)
	}
}
