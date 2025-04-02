package providers

import (
	"blacked/features/providers/base"
	"fmt"

	"github.com/rs/zerolog/log"
)

// FilterProviders filters the provider list based on the selected providers.
func (p *Providers) FilterProviders(selectedProviders []string) (*Providers, error) {
	filteredProviders := Providers{}
	providerMap := make(map[string]base.Provider)
	for _, prov := range *p {
		providerMap[prov.GetName()] = prov
	}

	for _, providerName := range selectedProviders {
		provider, exists := providerMap[providerName]
		if !exists {
			return nil, fmt.Errorf("provider '%s' not found", providerName)
		}
		filteredProviders = append(filteredProviders, provider)
	}
	return &filteredProviders, nil
}

// RemoveProviders removes specified providers from the provider list.
func (p *Providers) RemoveProviders(providersToRemove []string) error {
	if len(providersToRemove) == 0 {
		return nil
	}

	log.Info().Msgf("Removing providers: %v", providersToRemove)
	for _, providerName := range providersToRemove {
		provider, err := p.FindProviderByName(providerName)
		if err != nil {
			return fmt.Errorf("failed to find provider '%s': %w", providerName, err)
		}
		RemoveProvider(provider) // Use the global RemoveProvider func in init.go to modify global provider list
	}
	log.Info().Msgf("Providers after removing: %v", p.GetNames())
	return nil
}

func (p *Providers) FindProviderByName(name string) (base.Provider, error) {
	for _, provider := range *p {
		if provider.GetName() == name {
			return provider, nil
		}
	}
	return nil, fmt.Errorf("provider '%s' not found", name)
}
