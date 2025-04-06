package providers

import (
	"context"

	"github.com/rs/zerolog/log"
)

func (p *Providers) Processor(selectedProviders, providersToRemove []string) error {
	ctx := context.Background()

	if err := p.RemoveProviders(providersToRemove); err != nil {
		return err
	}

	// Determine which providers to process
	var providersToProcess *Providers
	var err error

	if len(selectedProviders) > 0 {
		log.Info().Msgf("Processing selected providers: %v", selectedProviders)
		providersToProcess, err = p.FilterProviders(selectedProviders)
		if err != nil {
			return err
		}
	} else {
		log.Info().Msg("Processing all providers...")
		providersToProcess = p
	}

	// Process with bulk cache update
	return providersToProcess.Process(ctx, ProcessOptions{
		UpdateCacheMode: UpdateCacheImmediate,
		TrackMetrics:    true,
	})
}

// ProcessProvidersData is the actual processing logic that iterates through providers and parses data
func (p *Providers) ProcessProvidersData(ctx context.Context) error {
	return p.Process(ctx) // Re-use the existing Process function in main.go, adjust if needed
}
