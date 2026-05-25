package resilience

import (
	"time"

	"github.com/rs/zerolog/log"
	gobreaker "github.com/sony/gobreaker"
)

// CircuitBreakerConfig holds configuration for a circuit breaker
type CircuitBreakerConfig struct {
	// Name is the identifier for the circuit breaker (usually provider name)
	Name string
	// MaxRequests is the maximum number of requests allowed in half-open state
	MaxRequests uint32
	// Interval is the time window for the circuit breaker to reset internal counts
	Interval time.Duration
	// Timeout is the time the circuit breaker waits before transitioning to half-open
	Timeout time.Duration
	// ReadyToTrip is called when a failure threshold is reached
	// If nil, defaults to 5 consecutive failures
	ReadyToTrip func(counts gobreaker.Counts) bool
	// OnStateChange is called when the circuit breaker state changes
	OnStateChange func(name string, from gobreaker.State, to gobreaker.State)
}

// DefaultCircuitBreakerConfig returns a sensible default configuration
func DefaultCircuitBreakerConfig(name string) CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Name:        name,
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: DefaultReadyToTrip,
		OnStateChange: DefaultOnStateChange,
	}
}

// DefaultReadyToTrip trips the circuit after 5 consecutive failures
func DefaultReadyToTrip(counts gobreaker.Counts) bool {
	return counts.ConsecutiveFailures >= 5
}

// DefaultOnStateChange logs circuit breaker state transitions
func DefaultOnStateChange(name string, from gobreaker.State, to gobreaker.State) {
	log.Warn().
		Str("circuit_breaker", name).
		Str("from_state", from.String()).
		Str("to_state", to.String()).
		Msg("Circuit breaker state changed")
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration
func NewCircuitBreaker(cfg CircuitBreakerConfig) *gobreaker.CircuitBreaker {
	settings := gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: cfg.ReadyToTrip,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			if cfg.OnStateChange != nil {
				cfg.OnStateChange(name, from, to)
			}
		},
	}

	return gobreaker.NewCircuitBreaker(settings)
}

// CircuitBreakerRegistry manages circuit breakers per provider
type CircuitBreakerRegistry struct {
	breakers map[string]*gobreaker.CircuitBreaker
	configs  map[string]CircuitBreakerConfig
}

// NewCircuitBreakerRegistry creates a new circuit breaker registry
func NewCircuitBreakerRegistry() *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*gobreaker.CircuitBreaker),
		configs:  make(map[string]CircuitBreakerConfig),
	}
}

// GetOrCreate returns a circuit breaker for the given provider name
func (r *CircuitBreakerRegistry) GetOrCreate(name string, cfg *CircuitBreakerConfig) *gobreaker.CircuitBreaker {
	if cb, exists := r.breakers[name]; exists {
		return cb
	}

	var config CircuitBreakerConfig
	if cfg != nil {
		config = *cfg
	} else {
		config = DefaultCircuitBreakerConfig(name)
	}

	if config.Name == "" {
		config.Name = name
	}

	cb := NewCircuitBreaker(config)
	r.breakers[name] = cb
	r.configs[name] = config

	return cb
}

// Get returns an existing circuit breaker or nil if not found
func (r *CircuitBreakerRegistry) Get(name string) *gobreaker.CircuitBreaker {
	return r.breakers[name]
}

// IsCircuitOpen returns true if the circuit breaker is open (not allowing requests)
func (r *CircuitBreakerRegistry) IsCircuitOpen(name string) bool {
	cb := r.Get(name)
	if cb == nil {
		return false
	}
	return cb.State() == gobreaker.StateOpen
}

// ResetCircuit resets the circuit breaker for the given provider
func (r *CircuitBreakerRegistry) ResetCircuit(name string) {
	cb := r.Get(name)
	if cb != nil {
		// Circuit breaker doesn't have a Reset method, but we can create a new one
		cfg, exists := r.configs[name]
		if exists {
			r.breakers[name] = NewCircuitBreaker(cfg)
		}
	}
}

// Global registry for circuit breakers
var globalRegistry *CircuitBreakerRegistry

// GetGlobalRegistry returns the global circuit breaker registry
func GetGlobalRegistry() *CircuitBreakerRegistry {
	if globalRegistry == nil {
		globalRegistry = NewCircuitBreakerRegistry()
	}
	return globalRegistry
}