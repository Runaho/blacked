package base

import (
	"sync"

	"blacked/internal/config"

	"github.com/rs/zerolog/log"
)

var (
	providerRegistry = make(map[string]ProviderRegistery)
	registryMu       sync.RWMutex
)

type ProviderRegistery struct {
	Provider            *BaseProvider
	CronSchedule        string
	IsSchedulingEnabled bool
}

func RegisterProvider(provider *BaseProvider) {
	name := provider.GetName()
	config := config.GetConfig()
	log.Trace().Str("provider", name).Msg("Registering provider")

	if !config.Provider.IsProviderEnabled(name) {
		log.Info().Str("provider", name).Msg("Provider is disabled")
		return
	}

	schedule, isExist := config.GetProviderCronSchedule(name)
	if !isExist {
		schedule = provider.CronSchedule
	}

	isSchedulingEnabled := schedule != ""

	registryMu.Lock()
	providerRegistry[name] = ProviderRegistery{
		Provider:            provider,
		CronSchedule:        schedule,
		IsSchedulingEnabled: isSchedulingEnabled,
	}
	registryMu.Unlock()

	if isSchedulingEnabled {
		provider.SetCronSchedule(schedule)
	}

	log.Info().
		Str("provider", name).
		Str("schedule", schedule).
		Bool("scheduling_enabled", isSchedulingEnabled).
		Msg("Provider registered")

	return
}

func GetProvider(name string) (*BaseProvider, bool) {
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
