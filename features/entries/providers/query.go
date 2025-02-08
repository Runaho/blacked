package providers

import "fmt"

func (p *Providers) FindProviderByName(name string) (Provider, error) {
	for _, provider := range *p {
		if provider.Name() == name {
			return provider, nil
		}
	}
	return nil, fmt.Errorf("provider '%s' not found", name)
}

func (p *Providers) FilterProviders(providerNames []string) (*Providers, error) {
	var filteredProviders Providers
	for _, providerName := range providerNames {
		provider, err := p.FindProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		filteredProviders = append(filteredProviders, provider)
	}
	return &filteredProviders, nil
}
