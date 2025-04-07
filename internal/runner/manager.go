package runner

import (
	"blacked/features/providers/base"
	"context"
	"errors"
	"sync"

	"github.com/rs/zerolog/log"
)

// Error variables for runner manager
var (
	ErrRunnerCreate     = errors.New("failed to create runner")
	ErrProviderRegister = errors.New("failed to register providers")
	ErrProviderNotFound = errors.New("provider not found")
	ErrRunnerNotInit    = errors.New("runner not initialized")
)

var (
	globalRunner *Runner
	initOnce     sync.Once
	initError    error
)

// InitializeRunner creates and configures the global runner with all providers
func InitializeRunner(providers []base.Provider) (*Runner, error) {
	initOnce.Do(func() {
		// Create runner
		_globalRunner, err := NewRunner()
		if err != nil {
			log.Err(err).Msg("Failed to create runner")
			initError = ErrRunnerCreate
			return
		}

		// Register all providers
		if err := registerAllProviders(_globalRunner, providers); err != nil {
			log.Err(err).Msg("Failed to register providers")
			initError = ErrProviderRegister
			return
		}

		// Start the scheduler
		globalRunner = _globalRunner
		globalRunner.Start()
		log.Info().Msg("Global scheduler runner initialized and started")
	})

	return globalRunner, initError
}

// registerAllProviders adds all providers to the runner
func registerAllProviders(runner *Runner, providers []base.Provider) error {
	for _, p := range providers {
		if err := runner.RegisterProvider(p, p.GetCronSchedule()); err != nil {
			log.Err(err).Str("provider", p.GetName()).Msg("Failed to register provider")
			return errors.Join(ErrProviderRegister, err)
		}
	}

	return nil
}

// GetRunner returns the global runner instance
func GetRunner() (*Runner, error) {
	if globalRunner == nil {
		log.Error().Msg("Runner not initialized")
		return nil, ErrRunnerNotInit
	}
	return globalRunner, nil
}

// ShutdownRunner stops the global runner
func ShutdownRunner(ctx context.Context) error {
	if globalRunner == nil {
		return nil
	}
	return globalRunner.Stop(ctx)
}
