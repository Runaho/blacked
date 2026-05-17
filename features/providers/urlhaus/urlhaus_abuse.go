package urlhaus

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

// NewURLHausProvider creates a new URLhaus provider
func NewURLHausProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	const providerName = "urlhaus-online"

	opts, ok := cfg.Providers[providerName]
	if !ok || opts == nil {
		log.Info().Str("provider", providerName).Msg("provider not configured in .env.toml — skipping")
		return nil
	}
	if opts.Enabled != nil && !*opts.Enabled {
		log.Info().Str("provider", providerName).Msg("provider disabled — skipping")
		return nil
	}

	sourceURL := opts.SourceURL
	if sourceURL == "" {
		sourceURL = "https://urlhaus.abuse.ch/downloads/text/"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "15 */2 * * *"
	}
	category := opts.Category
	if category == "" {
		category = "malware"
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

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		return base.ParseLinesParallel(data, collector, providerName, workers, batchSize, func(line, processID string) (*entries.Entry, error) {
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
