package providers

import (
	"sync"
)

var (
	once sync.Once
	p    *Providers
)

func InitProviders() (providers *Providers, err error) {
	once.Do(func() {
		providers, err = NewProviders()
		p = providers
	})
	return p, err
}

func GetProviders() *Providers {
	return p
}

func AppendProvider(provider Provider) {
	providers := *p
	providers = append(providers, provider)
	p = &providers
}

func RemoveProvider(provider Provider) {
	providers := *p
	for i, p := range providers {
		if p == provider {
			providers = append(providers[:i], providers[i+1:]...)
			break
		}
	}
	p = &providers
}
