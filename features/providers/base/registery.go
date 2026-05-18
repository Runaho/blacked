package base

import (
	"sync"

	"github.com/rs/zerolog/log"
)

var (
	providerRegistry = make(map[string]ProviderRegistery)
	registryMu       sync.RWMutex
)

type ProviderRegistery struct {
	Provider            Provider
	CronSchedule        string
	IsSchedulingEnabled bool
}

// RegisterProvider stores a provider in the registry.
// Enabled/cron checks are the caller's responsibility — this only registers.
func RegisterProvider(provider Provider) {
	name := provider.GetName()

	log.Trace().Str("provider", name).Msg("Registering provider")

	schedule := provider.GetCronSchedule()
	isSchedulingEnabled := schedule != ""

	registryMu.Lock()
	providerRegistry[name] = ProviderRegistery{
		Provider:            provider,
		CronSchedule:        schedule,
		IsSchedulingEnabled: isSchedulingEnabled,
	}
	registryMu.Unlock()

	log.Info().
		Str("provider", name).
		Str("schedule", schedule).
		Bool("scheduling_enabled", isSchedulingEnabled).
		Msg("Provider registered")
}

func GetProvider(name string) (Provider, bool) {
	registryMu.RLock()
	provider, ok := providerRegistry[name]
	registryMu.RUnlock()
	return provider.Provider, ok
}

func GetRegisteredProviders() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	var providers []Provider
	for _, provider := range providerRegistry {
		providers = append(providers, provider.Provider)
	}
	return providers
}

func GetScheduledProviders() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	var providers []Provider
	for _, provider := range providerRegistry {
		if provider.IsSchedulingEnabled {
			providers = append(providers, provider.Provider)
		}
	}
	return providers
}

func GetIsSchedulingEnabled(name string) bool {
	registryMu.RLock()
	provider, ok := providerRegistry[name]
	registryMu.RUnlock()
	if !ok {
		return false
	}
	return provider.IsSchedulingEnabled
}

func GetProviderSchedule(name string) string {
	registryMu.RLock()
	provider, ok := providerRegistry[name]
	registryMu.RUnlock()
	if !ok {
		return ""
	}
	return provider.CronSchedule
}
