package providers

import (
	"time"

	"blacked/internal/config"
	"blacked/internal/resilience"
)

// ProviderConfig holds provider-specific configuration including resilience settings
type ProviderConfig struct {
	// Name is the provider identifier
	Name string
	// Timeout is the maximum time to wait for fetch/parse operations
	Timeout time.Duration
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int
	// EnableCircuitBreaker enables circuit breaker protection
	EnableCircuitBreaker bool
	// EnableRetry enables retry with exponential backoff
	EnableRetry bool
}

// DefaultProviderConfig returns a sensible default configuration for a provider
func DefaultProviderConfig(name string) ProviderConfig {
	return ProviderConfig{
		Name:                name,
		Timeout:            30 * time.Second,
		MaxRetries:         3,
		EnableCircuitBreaker: true,
		EnableRetry:         true,
	}
}

// GetProviderConfig creates ProviderConfig from config.ProviderOptions
func GetProviderConfig(name string, opts *config.ProviderOptions) ProviderConfig {
	cfg := DefaultProviderConfig(name)

	if opts == nil {
		return cfg
	}

	// Override timeout if specified
	if opts.Timeout != nil {
		cfg.Timeout = *opts.Timeout
	}

	// Note: MaxRetries, EnableCircuitBreaker, and EnableRetry would need
	// to be added to config.ProviderOptions if custom configuration per provider is desired

	return cfg
}

// ToResilienceConfig converts ProviderConfig to resilience.ProviderResilienceConfig
func (pc ProviderConfig) ToResilienceConfig() resilience.ProviderResilienceConfig {
	retryCfg := resilience.DefaultRetryConfig()
	retryCfg.MaxRetries = pc.MaxRetries

	cbCfg := resilience.DefaultCircuitBreakerConfig(pc.Name)

	return resilience.ProviderResilienceConfig{
		Timeout:              pc.Timeout,
		Retry:                retryCfg,
		CircuitBreaker:       cbCfg,
		EnableCircuitBreaker: pc.EnableCircuitBreaker,
		EnableRetry:         pc.EnableRetry,
	}
}