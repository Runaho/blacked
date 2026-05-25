package resilience

import (
	"context"
	"io"
	"time"

	"blacked/internal/config"

	"github.com/rs/zerolog/log"
	gobreaker "github.com/sony/gobreaker"
)

// ProviderResilienceConfig holds all resilience configuration for a provider
type ProviderResilienceConfig struct {
	// Timeout is the maximum time to wait for a provider fetch/parse operation
	Timeout time.Duration
	// Retry configuration
	Retry RetryConfig
	// CircuitBreaker configuration
	CircuitBreaker CircuitBreakerConfig
	// EnableCircuitBreaker enables circuit breaker protection
	EnableCircuitBreaker bool
	// EnableRetry enables retry logic
	EnableRetry bool
}

// DefaultProviderResilienceConfig returns sensible defaults for a provider
func DefaultProviderResilienceConfig(name string) ProviderResilienceConfig {
	return ProviderResilienceConfig{
		Timeout:              30 * time.Second, // Override default 5-minute colly timeout
		Retry:                DefaultRetryConfig(),
		CircuitBreaker:      DefaultCircuitBreakerConfig(name),
		EnableCircuitBreaker: true,
		EnableRetry:         true,
	}
}

// FetchResult holds the result of a fetch operation
type FetchResult struct {
	Reader io.Reader
	Err    error
}

// ExecuteWithResilience wraps a fetch operation with timeout, retry, and circuit breaker
// This version works with io.Reader return type
func ExecuteWithResilience(
	ctx context.Context,
	providerName string,
	config ProviderResilienceConfig,
	fetchFunc func(context.Context) (io.Reader, error),
) (io.Reader, error) {
	// Check if circuit breaker is open
	registry := GetGlobalRegistry()
	cb := registry.GetOrCreate(providerName, &config.CircuitBreaker)

	// Create operation that respects timeout and uses circuit breaker
	operation := func() (io.Reader, error) {
		// Check circuit breaker state
		if config.EnableCircuitBreaker && cb.State() == gobreaker.StateOpen {
			log.Warn().Str("provider", providerName).Msg("Circuit breaker is open, rejecting request")
			return nil, gobreaker.ErrOpenState
		}

		// Create timeout context
		timeoutCtx, cancel := context.WithTimeout(ctx, config.Timeout)
		defer cancel()

		// Execute within circuit breaker
		result, err := cb.Execute(func() (interface{}, error) {
			return fetchFunc(timeoutCtx)
		})

		if err != nil {
			return nil, err
		}

		if result == nil {
			return nil, nil
		}

		return result.(io.Reader), nil
	}

	// Apply retry logic if enabled
	if config.EnableRetry {
		return RetryWithBackoff(ctx, config.Retry, func() (io.Reader, error) {
			return operation()
		})
	}

	return operation()
}

// ParseWithTimeout wraps a parse operation with timeout
func ParseWithTimeout(
	ctx context.Context,
	timeout time.Duration,
	parseFunc func(context.Context) error,
) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return parseFunc(timeoutCtx)
}

// GetProviderConfigFromOptions creates a ProviderResilienceConfig from config.ProviderOptions
func GetProviderConfigFromOptions(name string, timeout *time.Duration, maxRetries int) ProviderResilienceConfig {
	cfg := DefaultProviderResilienceConfig(name)

	if timeout != nil {
		cfg.Timeout = *timeout
	}

	if maxRetries > 0 {
		cfg.Retry.MaxRetries = maxRetries
	}

	return cfg
}

// GetProviderResilienceConfig creates a full ProviderResilienceConfig from config.ProviderOptions
func GetProviderResilienceConfig(name string, opts *config.ProviderOptions) ProviderResilienceConfig {
	cfg := DefaultProviderResilienceConfig(name)

	if opts == nil {
		return cfg
	}

	// Override timeout if specified
	if opts.Timeout != nil {
		cfg.Timeout = *opts.Timeout
	}

	// Override retry count if specified
	if opts.MaxRetries != nil {
		cfg.Retry.MaxRetries = *opts.MaxRetries
	}

	// Override enable retry flag
	if opts.EnableRetry != nil {
		cfg.EnableRetry = *opts.EnableRetry
	}

	// Override enable circuit breaker flag
	if opts.EnableCircuitBreak != nil {
		cfg.EnableCircuitBreaker = *opts.EnableCircuitBreak
	}

	return cfg
}

// IsCircuitOpen checks if the circuit breaker for a provider is open
func IsCircuitOpen(providerName string) bool {
	return GetGlobalRegistry().IsCircuitOpen(providerName)
}

// ResetCircuitBreaker resets the circuit breaker for a provider
func ResetCircuitBreaker(providerName string) {
	GetGlobalRegistry().ResetCircuit(providerName)
}

// GetCircuitBreakerState returns the current state of a provider's circuit breaker
func GetCircuitBreakerState(providerName string) string {
	cb := GetGlobalRegistry().Get(providerName)
	if cb == nil {
		return "not_initialized"
	}
	return cb.State().String()
}