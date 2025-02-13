package provider_processor

import (
	"blacked/features/entries/providers"
	"fmt"

	"github.com/rs/zerolog/log"
)

func Process(selectedProviders, providersToRemove []string) error {
	providersList := providers.GetProviders()
	if providersList == nil {
		return fmt.Errorf("providers not initialized")
	}

	if err := RemoveProviders(providersList, providersToRemove); err != nil {
		return err
	}

	if len(selectedProviders) > 0 {
		if err := processSelectedProviders(providersList, selectedProviders); err != nil {
			return err
		}
	} else {
		if err := processAllProviders(providersList); err != nil {
			return err
		}
	}

	fmt.Println("Blacklist entries processed successfully.")
	return nil
}

// processSelectedProviders processes only the specified providers.
func processSelectedProviders(providersList *providers.Providers, selectedProviders []string) error {
	log.Info().Msgf("Processing selected providers: %v", selectedProviders)
	filteredProviders, err := FilterProviders(providersList, selectedProviders)
	if err != nil {
		return err
	}
	if err := filteredProviders.Process(); err != nil {
		return fmt.Errorf("failed to process selected providers: %w", err)
	}
	return nil
}

// processAllProviders processes all available providers.
func processAllProviders(providersList *providers.Providers) error {
	log.Info().Msg("Processing all providers...")
	if err := providersList.Process(); err != nil {
		return fmt.Errorf("failed to process blacklist entries: %w", err)
	}
	return nil
}
