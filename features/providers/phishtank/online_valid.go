package phishtank

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"encoding/json"
	"io"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

type PhishTankEntry struct {
	URL        string `json:"url"`
	Verified   bool   `json:"verified"`
	VerifyDate string `json:"verification_time"`
}

func NewPhishTankProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	const providerName = "phishtank-online-valid"

	opts, ok := cfg.Providers[providerName]
	if !ok || opts == nil {
		opts = &config.ProviderOptions{}
	}
	if opts.Enabled != nil && !*opts.Enabled {
		log.Info().Str("provider", providerName).Msg("provider disabled — skipping")
		return nil
	}

	sourceURL := opts.SourceURL
	if sourceURL == "" {
		sourceURL = "https://data.phishtank.com/data/{api_key}/online-valid.json"
	}

	// Replace {api_key} placeholder if an API key is configured
	sourceURL = base.ResolveURL(sourceURL, opts.APIKey)

	// If API key is empty, warn and skip (source URL will still contain {api_key} placeholder)
	if opts.APIKey == "" {
		log.Warn().Str("provider", providerName).Msg("PhishTank API key not configured — skipping")
		return nil
	}

	cron := opts.Cron
	if cron == "" {
		cron = "45 */6 * * *"
	}

	workers := opts.ParserWorkers
	if workers <= 0 {
		workers = 4
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
		var phishEntries []PhishTankEntry

		decoder := json.NewDecoder(data)
		if err := decoder.Decode(&phishEntries); err != nil {
			log.Error().Err(err).Msg("error decoding PhishTank JSON")
			return err
		}

		return base.ProcessEntriesParallel(phishEntries, collector, workers, func(phishEntry PhishTankEntry, processID string) (*entries.Entry, error) {
			if !phishEntry.Verified {
				return nil, nil
			}

			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(processID).
				WithCategory("phishing")

			if err := entry.SetURL(phishEntry.URL); err != nil {
				log.Error().Err(err).Msgf("error setting URL: %s", phishEntry.URL)
				return nil, nil
			}

			return entry, nil
		}, processID)
	}

	provider := base.NewBaseProvider(
		providerName,
		sourceURL,
		"phishing",
		client,
		parseFunc,
	)

	provider.
		SetCronSchedule(cron).
		Register()

	return provider
}
