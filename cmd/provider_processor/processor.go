package provider_processor

import (
	"blacked/features/providers"
	"blacked/features/providers/services"
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

func Process(selectedProviders, providersToRemove []string, force bool) error {
	providersList := providers.GetProviders()
	if providersList == nil {
		return fmt.Errorf("providers not initialized")
	}

	providerProcessService, err := services.NewProviderProcessService()
	if err != nil {
		return fmt.Errorf("failed to initialize provider process service: %w", err)
	}

	ctx := context.Background()

	isRunning, err := providerProcessService.IsProcessRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to check process status: %w", err)
	}
	if isRunning {
		if !force {
			return fmt.Errorf("another process is already running. Please wait for it to complete")
		} else {
			log.Warn().Msg("Forcing process to start even though another process is running after 5 seconds")
			time.Sleep(5 * time.Second)
		}
	}

	processID, err := providerProcessService.StartProcessAsync(ctx, selectedProviders, providersToRemove) // Start process via service
	if err != nil {
		return fmt.Errorf("failed to start provider process via service: %w", err)
	}

	log.Info().Str("process_id", processID).Msg("Provider processing initiated via service from CLI.")
	fmt.Printf("Provider processing initiated in background (process ID: %s). Use process ID to check status via API.\n", processID)

	return nil
}
