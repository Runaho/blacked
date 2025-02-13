package provider_processor

import (
	"blacked/features/entries/providers"
	"fmt"
)

// FilterProviders filters the provider list based on the selected providers.
func FilterProviders(providersList *providers.Providers, selectedProviders []string) (*providers.Providers, error) {
	filteredProviders, err := providersList.FilterProviders(selectedProviders)
	if err != nil {
		return nil, fmt.Errorf("failed to filter providers: %w", err)
	}
	return filteredProviders, nil
}
