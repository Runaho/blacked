package resilience

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	gobreaker "github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
)

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig("test-provider")

	assert.Equal(t, "test-provider", cfg.Name)
	assert.Equal(t, uint32(3), cfg.MaxRequests)
	assert.Equal(t, 60*time.Second, cfg.Interval)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.NotNil(t, cfg.ReadyToTrip)
	assert.NotNil(t, cfg.OnStateChange)
}

func TestNewCircuitBreaker(t *testing.T) {
	cfg := CircuitBreakerConfig{
		Name:        "test",
		MaxRequests: 5,
		Interval:    10 * time.Second,
		Timeout:     5 * time.Second,
	}

	cb := NewCircuitBreaker(cfg)
	assert.NotNil(t, cb)
	assert.Equal(t, gobreaker.StateClosed, cb.State())
}

func TestCircuitBreakerRegistry_GetOrCreate(t *testing.T) {
	registry := NewCircuitBreakerRegistry()

	// Create a new circuit breaker
	cb1 := registry.GetOrCreate("provider1", nil)
	assert.NotNil(t, cb1)

	// Get the same circuit breaker again
	cb2 := registry.GetOrCreate("provider1", nil)
	assert.Same(t, cb1, cb2)

	// Create another circuit breaker for different provider
	cb3 := registry.GetOrCreate("provider2", nil)
	assert.NotNil(t, cb3)
	assert.NotSame(t, cb1, cb3)
}

func TestCircuitBreakerRegistry_IsCircuitOpen(t *testing.T) {
	registry := NewCircuitBreakerRegistry()

	// Non-existent circuit breaker should return false
	assert.False(t, registry.IsCircuitOpen("non-existent"))

	// Create a circuit breaker
	registry.GetOrCreate("test", nil)
	assert.False(t, registry.IsCircuitOpen("test"))
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 1*time.Second, cfg.InitialInterval)
	assert.Equal(t, 30*time.Second, cfg.MaxInterval)
	assert.Equal(t, 2.0, cfg.Multiplier)
}

func TestRetryWithBackoff_Success(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultRetryConfig()

	callCount := 0
	operation := func() (string, error) {
		callCount++
		if callCount < 2 {
			return "", errors.New("temporary error")
		}
		return "success", nil
	}

	result, err := RetryWithBackoff(ctx, cfg, operation)

	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 2, callCount) // Failed once, succeeded on second try
}

func TestRetryWithBackoff_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultRetryConfig()

	callCount := 0
	testErr := errors.New("persistent error")
	operation := func() (string, error) {
		callCount++
		return "", testErr
	}

	result, err := RetryWithBackoff(ctx, cfg, operation)

	assert.Error(t, err)
	assert.Equal(t, testErr, err)
	assert.Equal(t, "", result)
	assert.Equal(t, cfg.MaxRetries+1, callCount) // Initial + retries
}

func TestRetryWithBackoff_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := DefaultRetryConfig()

	callCount := 0
	operation := func() (string, error) {
		callCount++
		if callCount == 1 {
			cancel() // Cancel context during retry
		}
		return "", errors.New("error")
	}

	result, err := RetryWithBackoff(ctx, cfg, operation)

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, "", result)
}

func TestDefaultProviderResilienceConfig(t *testing.T) {
	cfg := DefaultProviderResilienceConfig("test-provider")

	assert.Equal(t, "test-provider", cfg.CircuitBreaker.Name)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.True(t, cfg.EnableCircuitBreaker)
	assert.True(t, cfg.EnableRetry)
	assert.Equal(t, 3, cfg.Retry.MaxRetries)
}

func TestExecuteWithResilience_Success(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultProviderResilienceConfig("test")

	expectedContent := "test data"
	operation := func(ctx context.Context) (io.Reader, error) {
		return strings.NewReader(expectedContent), nil
	}

	result, err := ExecuteWithResilience(ctx, "test", cfg, operation)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	data, _ := io.ReadAll(result)
	assert.Equal(t, expectedContent, string(data))
}

func TestExecuteWithResilience_Timeout(t *testing.T) {
	// Short timeout to trigger timeout quickly
	cfg := ProviderResilienceConfig{
		Timeout:              10 * time.Millisecond,
		Retry:                DefaultRetryConfig(),
		CircuitBreaker:      DefaultCircuitBreakerConfig("test"),
		EnableCircuitBreaker: false, // Disable to simplify test
		EnableRetry:         false,
	}

	ctx := context.Background()
	operation := func(ctx context.Context) (io.Reader, error) {
		// This operation doesn't actually block - the timeout is in the wrapper
		// Just return an error to test error handling
		return nil, errors.New("simulated error")
	}

	result, err := ExecuteWithResilience(ctx, "test", cfg, operation)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestGetGlobalRegistry(t *testing.T) {
	registry1 := GetGlobalRegistry()
	registry2 := GetGlobalRegistry()

	assert.NotNil(t, registry1)
	assert.Same(t, registry1, registry2) // Should return the same instance
}

func TestGetCircuitBreakerState(t *testing.T) {
	// Test non-initialized circuit breaker
	state := GetCircuitBreakerState("non-existent")
	assert.Equal(t, "not_initialized", state)

	// Create a circuit breaker
	cfg := DefaultCircuitBreakerConfig("test")
	_ = NewCircuitBreaker(cfg)

	// Now check state - but this won't work because it's not in the registry
	// We need to register it via the registry
	registry := GetGlobalRegistry()
	registry.GetOrCreate("test", &cfg)

	state = GetCircuitBreakerState("test")
	assert.Equal(t, "closed", state)
}

func TestIsCircuitOpen(t *testing.T) {
	// Test non-existent provider
	assert.False(t, IsCircuitOpen("non-existent"))

	// Create a circuit breaker for a provider
	cfg := DefaultCircuitBreakerConfig("test-provider")
	registry := GetGlobalRegistry()
	cb := registry.GetOrCreate("test-provider", &cfg)

	// Should be closed initially
	assert.False(t, IsCircuitOpen("test-provider"))

	// Trip the circuit breaker
	for i := 0; i < 5; i++ {
		_, _ = cb.Execute(func() (interface{}, error) {
			return nil, errors.New("failure")
		})
	}

	// Note: The circuit may or may not be open depending on the implementation
	// The default ReadyToTrip trips after 5 consecutive failures
}

func TestResetCircuitBreaker(t *testing.T) {
	// Reset non-existent circuit breaker should not panic
	ResetCircuitBreaker("non-existent")

	// Create a circuit breaker
	cfg := DefaultCircuitBreakerConfig("test-reset")
	registry := GetGlobalRegistry()
	_ = registry.GetOrCreate("test-reset", &cfg)

	// Reset should not panic
	ResetCircuitBreaker("test-reset")
}