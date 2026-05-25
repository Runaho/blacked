package resilience

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/rs/zerolog/log"
)

// RetryConfig holds configuration for retry behavior
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (0 = no retries)
	MaxRetries int
	// InitialInterval is the initial backoff duration
	InitialInterval time.Duration
	// MaxInterval is the maximum backoff duration
	MaxInterval time.Duration
	// Multiplier is the multiplier for exponential backoff
	Multiplier float64
	// RetryableErrors is a list of error types that should trigger retry
	// If nil, all errors will be retried
	RetryableErrors []error
}

// DefaultRetryConfig returns a sensible default configuration for retries
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:      3,
		InitialInterval: 1 * time.Second,
		MaxInterval:    30 * time.Second,
		Multiplier:     2.0,
	}
}

// RetryableOperation represents an operation that can be retried
type RetryableOperation[T any] func() (T, error)

// RetryWithBackoff executes the operation with exponential backoff retry
func RetryWithBackoff[T any](ctx context.Context, config RetryConfig, operation RetryableOperation[T]) (T, error) {
	var result T
	var lastErr error

	if config.MaxRetries <= 0 {
		return operation()
	}

	expBackoff := backoff.NewExponentialBackOff()
	expBackoff.InitialInterval = config.InitialInterval
	expBackoff.MaxInterval = config.MaxInterval
	expBackoff.Multiplier = config.Multiplier
	expBackoff.Reset()

	notify := func(err error, duration time.Duration) {
		log.Warn().
			Err(err).
			Dur("retry_after", duration).
			Msg("Operation failed, retrying with exponential backoff")
	}

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		result, lastErr = operation()
		if lastErr == nil {
			return result, nil
		}

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		// Check if error is retryable
		if !isRetryableError(lastErr, config.RetryableErrors) && len(config.RetryableErrors) > 0 {
			log.Debug().Err(lastErr).Msg("Error is not retryable, giving up")
			return result, lastErr
		}

		// Don't sleep on last attempt
		if attempt < config.MaxRetries {
			nextDelay := expBackoff.NextBackOff()
			notify(lastErr, nextDelay)

			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(nextDelay):
			}
		}
	}

	return result, lastErr
}

// isRetryableError checks if an error should trigger a retry
func isRetryableError(err error, retryableErrors []error) bool {
	if len(retryableErrors) == 0 {
		return true // By default, all errors are retryable
	}

	for _, retryableErr := range retryableErrors {
		if err == retryableErr {
			return true
		}
	}
	return false
}

// NewExponentialBackoff creates a new exponential backoff instance
func NewExponentialBackoff(initialInterval, maxInterval time.Duration, multiplier float64) *backoff.ExponentialBackOff {
	expBackoff := backoff.NewExponentialBackOff()
	expBackoff.InitialInterval = initialInterval
	expBackoff.MaxInterval = maxInterval
	expBackoff.Multiplier = multiplier
	return expBackoff
}