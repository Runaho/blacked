package providers

import (
	"blacked/features/providers/base"
	"errors"

	"github.com/rs/zerolog/log"
)

var (
	ErrProviderFilterFailed = errors.New("failed to filter providers")
	ErrProviderRemoveFailed = errors.New("failed to remove providers")
	ErrFailedToFindProvider = errors.New("failed to find provider")
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
			log.Err(ErrFailedToFindProvider).
				Str("provider", providerName).
				Msg("Failed to find provider to remove")

			return nil, ErrFailedToFindProvider
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
			log.Err(ErrFailedToFindProvider).
				Str("provider", providerName).
				Msg("Failed to find provider to remove")

			return ErrFailedToFindProvider
		}
		RemoveProvider(provider)
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

	log.Err(ErrFailedToFindProvider).
		Str("provider", name).
		Msg("Failed to find provider to remove")

	return nil, ErrFailedToFindProvider
}
