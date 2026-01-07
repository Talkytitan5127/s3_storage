package retry

import (
	"context"
	"fmt"
	"time"
)

const (
	// DefaultMaxRetries is the default maximum number of retries
	DefaultMaxRetries = 3
	// DefaultInitialBackoff is the default initial backoff duration
	DefaultInitialBackoff = 1 * time.Second
	// DefaultMaxBackoff is the default maximum backoff duration
	DefaultMaxBackoff = 8 * time.Second
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:     DefaultMaxRetries,
		InitialBackoff: DefaultInitialBackoff,
		MaxBackoff:     DefaultMaxBackoff,
	}
}

// IsRetryable determines if an error is retryable
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for common retryable errors
	errStr := err.Error()

	// Network errors
	if contains(errStr, "connection refused") ||
		contains(errStr, "connection reset") ||
		contains(errStr, "broken pipe") ||
		contains(errStr, "timeout") ||
		contains(errStr, "deadline exceeded") ||
		contains(errStr, "temporary failure") ||
		contains(errStr, "unavailable") {
		return true
	}

	return false
}

// Do executes a function with exponential backoff retry logic
func Do(ctx context.Context, config *RetryConfig, fn func() error) error {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var lastErr error
	backoff := config.InitialBackoff

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Execute function
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !IsRetryable(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		// Don't sleep after last attempt
		if attempt == config.MaxRetries {
			break
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(backoff):
			// Exponential backoff with cap
			backoff *= 2
			if backoff > config.MaxBackoff {
				backoff = config.MaxBackoff
			}
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", config.MaxRetries, lastErr)
}

// DoWithResult executes a function with retry logic and returns a result
func DoWithResult[T any](ctx context.Context, config *RetryConfig, fn func() (T, error)) (T, error) {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var result T
	var lastErr error
	backoff := config.InitialBackoff

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Execute function
		res, err := fn()
		if err == nil {
			return res, nil
		}

		lastErr = err

		// Check if error is retryable
		if !IsRetryable(err) {
			return result, fmt.Errorf("non-retryable error: %w", err)
		}

		// Don't sleep after last attempt
		if attempt == config.MaxRetries {
			break
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(backoff):
			// Exponential backoff with cap
			backoff *= 2
			if backoff > config.MaxBackoff {
				backoff = config.MaxBackoff
			}
		}
	}

	return result, fmt.Errorf("max retries (%d) exceeded: %w", config.MaxRetries, lastErr)
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
