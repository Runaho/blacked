package provider_processor

import (
	"blacked/features/entries/providers"
	"fmt"

	"github.com/rs/zerolog/log"
)

// RemoveProviders removes specified providers from the provider list.
func RemoveProviders(providersList *providers.Providers, providersToRemove []string) error {
	if len(providersToRemove) == 0 {
		return nil
	}

	log.Info().Msgf("Removing providers: %v", providersToRemove)
	for _, providerName := range providersToRemove {
		provider, err := providersList.FindProviderByName(providerName)
		if err != nil {
			return fmt.Errorf("failed to find provider '%s': %w", providerName, err)
		}
		providers.RemoveProvider(provider)
	}
	log.Info().Msgf("Providers after removing: %v", providersList.Names())
	return nil
}
