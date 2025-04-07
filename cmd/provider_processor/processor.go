package provider_processor

import (
	"blacked/features/providers"
	"blacked/features/providers/services"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

func Process(selectedProviders, providersToRemove []string, force bool) error {
	providersList := providers.GetProviders()
	if providersList == nil {
		log.Error().Msg("Providers not initialized")
		return errors.New("providers not initialized")
	}

	providerProcessService, err := services.NewProviderProcessService()
	if err != nil {
		log.Err(err).
			Strs("selectedProviders", selectedProviders).
			Strs("providersToRemove", providersToRemove).
			Bool("force", force).
			Msg("failed to initialize provider process service")
		return err
	}

	ctx := context.Background()

	isRunning, err := providerProcessService.IsProcessRunning(ctx)
	if err != nil {
		log.Err(err).
			Strs("selectedProviders", selectedProviders).
			Strs("providersToRemove", providersToRemove).
			Bool("force", force).
			Msg("failed to check process status")

		return err
	}
	if isRunning {
		if !force {
			log.Error().
				Strs("selectedProviders", selectedProviders).
				Strs("providersToRemove", providersToRemove).
				Bool("force", force).
				Msg("another process is already running")

			return errors.New("another process is already running. Please wait for it to complete")
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

	return nil
}
