package runner

import (
	"blacked/features/cache"
	"blacked/features/providers"
	"blacked/features/providers/base"
	"context"

	"github.com/rs/zerolog/log"
)

func ExecuteProvider(ctx context.Context, provider base.Provider, opts ...providers.ProcessOptions) error {
	options := providers.DefaultProcessOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	// Create a single-provider Providers slice
	providersList := providers.Providers{provider}

	// Use the provider's Process method with our options
	return providersList.Process(ctx, providers.ProcessOptions{
		UpdateCacheMode: options.UpdateCacheMode,
		TrackMetrics:    options.TrackMetrics,
	})
}

// ExecuteProviders is now just a wrapper around the providers' process method
func ExecuteProviders(ctx context.Context, providersList []base.Provider) error {
	// Convert to Providers type
	p := providers.Providers(providersList)

	// Process with bulk update at the end
	err := p.Process(ctx, providers.ProcessOptions{
		UpdateCacheMode: providers.UpdateCacheNone,
		TrackMetrics:    true,
	})

	cache.FireAndForgetSync()

	if err != nil {
		log.Error().Err(err).Msg("Error processing providers")
	}

	return err
}
