package providers

import (
	"fmt"

	"github.com/rs/zerolog/log"
)

// Process orchestrates the blacklist entry processing based on selected providers and providers to remove.
func (p *Providers) Processor(selectedProviders, providersToRemove []string) error {
	if err := p.RemoveProviders(providersToRemove); err != nil { // Use Providers method
		return err
	}

	if len(selectedProviders) > 0 {
		if err := p.processSelectedProviders(selectedProviders); err != nil { // Use Providers method
			return err
		}
	} else {
		if err := p.processAllProviders(); err != nil { // Use Providers method
			return err
		}
	}

	fmt.Println("Blacklist entries processed successfully.")
	return nil
}

// processSelectedProviders processes only the specified providers.
func (p *Providers) processSelectedProviders(selectedProviders []string) error {
	log.Info().Msgf("Processing selected providers: %v", selectedProviders)
	filteredProviders, err := p.FilterProviders(selectedProviders) // Use Providers method
	if err != nil {
		return err
	}
	if err := filteredProviders.ProcessProvidersData(); err != nil {
		return fmt.Errorf("failed to process selected providers: %w", err)
	}
	return nil
}

// processAllProviders processes all available providers.
func (p *Providers) processAllProviders() error {
	log.Info().Msg("Processing all providers...")
	if err := p.ProcessProvidersData(); err != nil { // Call ProcessProvidersData on Providers
		return fmt.Errorf("failed to process blacklist entries: %w", err)
	}
	return nil
}

// ProcessProvidersData is the actual processing logic that iterates through providers and parses data
func (p *Providers) ProcessProvidersData() error {
	return p.Process() // Re-use the existing Process function in main.go, adjust if needed
}
