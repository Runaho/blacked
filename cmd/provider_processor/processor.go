package provider_processor

import (
	"blacked/features/providers"
	"blacked/features/providers/services"
	"blacked/internal/config"
	"context"
	"errors"
	"os"
	"runtime/trace"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	ErrProviderProcessNotInitialized     = errors.New("provider process not initialized")
	ErrProviderProcessAlreadyRunning     = errors.New("another provider process is already running")
	ErrProviderProcessStartFailed        = errors.New("failed to start provider process")
	ErrProviderProcessCheckFailed        = errors.New("failed to check provider process status")
	ErrProviderProcessForceFailed        = errors.New("failed to force start provider process")
	ErrProviderProcessServiceFailed      = errors.New("failed to initialize provider process service")
	ErrProviderProcessServiceStartFailed = errors.New("failed to start provider process via service")

	traceFile *os.File
)

func Process(selectedProviders, providersToRemove []string, force bool) error {
	providersList := providers.GetProviders()
	if providersList == nil {
		log.Error().Msg("Providers not initialized")
		return ErrProviderProcessNotInitialized
	}

	providerProcessService, err := services.NewProviderProcessService()
	if err != nil {
		log.Err(err).
			Strs("selectedProviders", selectedProviders).
			Strs("providersToRemove", providersToRemove).
			Bool("force", force).
			Msg("failed to initialize provider process service")

		return ErrProviderProcessServiceFailed
	}

	ctx := context.Background()

	isRunning, err := providerProcessService.IsProcessRunning(ctx)
	if err != nil {
		log.Err(err).
			Strs("selectedProviders", selectedProviders).
			Strs("providersToRemove", providersToRemove).
			Bool("force", force).
			Msg("failed to check process status")

		return ErrProviderProcessCheckFailed
	}
	if isRunning {
		if !force {
			log.Error().
				Strs("selectedProviders", selectedProviders).
				Strs("providersToRemove", providersToRemove).
				Bool("force", force).
				Msg("another process is already running")

			return ErrProviderProcessAlreadyRunning
		} else {
			log.Warn().Msg("Forcing process to start even though another process is running after 5 seconds")
			time.Sleep(5 * time.Second)
		}
	}

	if config.IsDevMode() {
		if traceFile, err := os.Create("start-process-trace.out"); err == nil {
			defer traceFile.Close()
			if err := trace.Start(traceFile); err == nil {
				defer trace.Stop()
				log.Info().Msg("Trace started for provider processing")
			} else {
				log.Warn().Err(err).Msg("Failed to start trace")
			}
		}
	}

	processID, err := providerProcessService.StartProcessAsync(ctx, selectedProviders, providersToRemove) // Start process via service
	if err != nil {
		log.Err(err).
			Strs("selectedProviders", selectedProviders).
			Strs("providersToRemove", providersToRemove).
			Bool("force", force).
			Msg("failed to start process via service")

		return ErrProviderProcessStartFailed
	}

	log.Info().Str("process_id", processID).Msg("Provider processing initiated via service from CLI.")

	return nil
}
