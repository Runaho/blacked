package providers

import (
	"blacked/features/providers/base"
	"slices"
	"sync"
)

var (
	once sync.Once
	p    Providers
)

func InitProviders() (providers Providers, err error) {
	once.Do(func() {
		providers, err = NewProviders()
		p = base.GetRegisteredProviders()
	})
	return p, err
}

func GetProviders() *Providers {
	return &p
}

func AppendProvider(provider base.Provider) {
	providers := p
	providers = append(providers, provider)
	p = providers
}

func RemoveProvider(provider base.Provider) {
	providers := p
	for i, p := range providers {
		if p == provider {
			providers = slices.Delete(providers, i, i+1)
			break
		}
	}
	p = providers
}
