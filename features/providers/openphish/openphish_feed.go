package openphish

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

func NewOpenPhishFeedProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
	const (
		providerName = "OPENPHISH"
		providerURL  = "https://openphish.com/feed.txt"
		cronSchedule = "30 */4 * * *" // Every 4 hours (30 minutes past the hour
	)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		return base.ParseLinesParallel(data, collector, providerName, settings.ParserWorkers, settings.ParserBatchSize, func(line, processID string) (*entries.Entry, error) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				return nil, nil // Skip empty lines and comments
			}

			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(processID).
				WithCategory("phishing")

			if err := entry.SetURL(line); err != nil {
				log.Error().Err(err).Msgf("error setting URL: %s", line)
				return nil, nil // Skip invalid URLs
			}

			return entry, nil
		})
	}

	provider := base.NewBaseProvider(
		providerName,
		providerURL,
		settings,
		collyClient,
		parseFunc,
	)

	provider.
		SetCronSchedule(cronSchedule).
		Register()

	return provider
}
