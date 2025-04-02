package oisd

import (
	"blacked/features/entries"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"bufio"
	"io"
	"strings"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// NewOISDBigProvider creates a new OISD Big provider
func NewOISDBigProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
	const (
		providerName = "OISD_BIG"
		providerURL  = "https://big.oisd.nl/domainswild2"
		cronSchedule = "0 6 * * *" // Everyday at 6:00 AM
	)

	parseFunc := func(data io.Reader) ([]entries.Entry, error) {
		var result []entries.Entry
		scanner := bufio.NewScanner(data)
		id := uuid.New().String()

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			// Create a new entry
			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(id)

			// SetURL may fail, so handle it separately
			if err := entry.SetURL(line); err != nil {
				log.Error().Err(err).Msgf("error setting URL: %s", line)
				continue
			}

			result = append(result, *entry)
		}

		if err := scanner.Err(); err != nil {
			return nil, err
		}

		return result, nil
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
