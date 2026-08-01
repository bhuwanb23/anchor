package caddy

import (
	"context"
	"log/slog"
	"time"
)

const (
	defaultMaxAttempts = 5
	defaultBaseDelay   = 500 * time.Millisecond
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// retryWithBackoff retries fn with exponential backoff.
// Delays: 500ms, 1s, 2s, 4s, 8s (5 attempts, ~15s total).
func retryWithBackoff(ctx context.Context, cfg RetryConfig, fn func() error) error {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = defaultBaseDelay
	}

	var lastErr error
	delay := cfg.BaseDelay

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lastErr = fn()
		if lastErr == nil {
			if attempt > 0 {
				slog.Info("succeeded after retry", "attempt", attempt+1)
			}
			return nil
		}

		if attempt < cfg.MaxAttempts-1 {
			slog.Warn("retrying after failure",
				"attempt", attempt+1,
				"max_attempts", cfg.MaxAttempts,
				"delay", delay,
				"error", lastErr)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}

			delay *= 2
		}
	}

	slog.Error("all retry attempts exhausted",
		"max_attempts", cfg.MaxAttempts,
		"last_error", lastErr)
	return lastErr
}
