package runner

import (
	"blacked/features/providers/base"
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
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
		globalRunner, err := NewRunner()
		if err != nil {
			initError = fmt.Errorf("failed to create runner: %w", err)
			return
		}

		// Register all providers
		if err := registerAllProviders(globalRunner, providers); err != nil {
			initError = fmt.Errorf("failed to register providers: %w", err)
			return
		}

		// Start the scheduler
		globalRunner.Start()
		log.Info().Msg("Global scheduler runner initialized and started")

		return
	})

	return globalRunner, initError
}

// registerAllProviders adds all providers to the runner
func registerAllProviders(runner *Runner, providers []base.Provider) error {
	for _, p := range providers {
		if err := runner.RegisterProvider(p, p.GetCronSchedule()); err != nil {
			return fmt.Errorf("failed to register provider %s: %w", p.GetName(), err)
		}
	}

	return nil
}

// GetRunner returns the global runner instance
func GetRunner() (*Runner, error) {
	if globalRunner == nil {
		return nil, fmt.Errorf("runner not initialized")
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

// RunProviderNow provides a convenient way to run a provider on demand
func RunProviderNow(providerName string) error {
	runner, err := GetRunner()
	if err != nil {
		return err
	}
	return runner.RunProviderImmediately(providerName)
}
