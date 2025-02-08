package cmd

import (
	"blacked/features/entries/providers"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

// ProcessCommand processes blacklist entries from specified providers.
var ProcessCommand = &cli.Command{
	Name:  "process",
	Usage: "Process blacklist entries from providers",
	Flags: []cli.Flag{
		&cli.StringSliceFlag{
			Name:    "provider",
			Aliases: []string{"p"},
			Usage:   "Specify providers to process (comma-separated). If omitted, process all providers.",
		},
		&cli.StringSliceFlag{
			Name:    "remove-provider",
			Aliases: []string{"r"},
			Usage:   "Specify providers to remove (comma-separated)",
		},
	},
	Action: processBlacklist,
}

func processBlacklist(c *cli.Context) error {
	providersList := providers.GetProviders()
	if providersList == nil {
		return fmt.Errorf("providers not initialized")
	}

	providersToRemove := c.StringSlice("remove-provider")
	if len(providersToRemove) > 0 {
		log.Info().Msgf("Removing providers: %v", providersToRemove)
		for _, providerName := range providersToRemove {
			provider, err := providersList.FindProviderByName(providerName)
			if err != nil {
				return fmt.Errorf("failed to find provider '%s': %w", providerName, err)
			}
			providers.RemoveProvider(provider)
		}
		log.Info().Msgf("Providers after removing: %v", providersList.Names())
	}

	selectedProviders := c.StringSlice("provider")
	if len(selectedProviders) > 0 {
		log.Info().Msgf("Processing selected providers: %v", selectedProviders)
		filteredProviders, err := providersList.FilterProviders(selectedProviders)
		if err != nil {
			return fmt.Errorf("failed to filter providers: %w", err)
		}
		if err := filteredProviders.Process(); err != nil {
			return fmt.Errorf("failed to process selected providers: %w", err)
		}

	} else {
		log.Info().Msg("Processing all providers...")
		if err := providersList.Process(); err != nil {
			return fmt.Errorf("failed to process blacklist entries: %w", err)
		}
	}

	fmt.Println("Blacklist entries processed successfully.")
	return nil
}
