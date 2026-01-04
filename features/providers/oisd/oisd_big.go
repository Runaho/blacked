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

// NewOISDBigProvider creates a new OISD Big provider
func NewOISDBigProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
	const (
		providerName = "OISD_BIG"
		providerURL  = "https://big.oisd.nl/domainswild2"
		cronSchedule = "0 6 * * *" // Everyday at 6:00 AM
	)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		return base.ParseLinesParallel(data, collector, providerName, settings.ParserWorkers, settings.ParserBatchSize, func(line, processID string) (*entries.Entry, error) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				return nil, nil // Skip empty lines and comments
			}

			// Create a new entry
			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(processID)

			// SetURL may fail, so handle it separately
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
