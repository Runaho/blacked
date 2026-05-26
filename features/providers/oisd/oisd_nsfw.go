package oisd

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"io"
	"strings"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

// NewOISDNSFWProvider creates a new OISD NSFW provider from config.
func NewOISDNSFWProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	const providerName = "oisd-nsfw"

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
		sourceURL = "https://nsfw.oisd.nl/domainswild"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "22 6 * * *"
	}
	category := opts.Category
	if category == "" {
		category = "nsfw"
	}

	workers := opts.ParserWorkers
	if workers <= 0 {
		workers = 4
	}
	batchSize := opts.ParserBatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
		return base.ParseLinesParallel(data, collector, providerName, processID, workers, batchSize, func(line, processID string) (*entries.Entry, error) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				return nil, nil
			}

			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(processID).
				WithCategory(category)

			if err := entry.SetURL(line); err != nil {
				log.Error().Err(err).Msgf("error setting URL: %s", line)
				return nil, nil
			}

			return entry, nil
		})
	}

	provider := base.NewBaseProvider(
		providerName,
		sourceURL,
		category,
		client,
		parseFunc,
	)

	provider.
		SetCronSchedule(cron).
		Register()

	return provider
}
