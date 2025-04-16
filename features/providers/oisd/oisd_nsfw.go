package oisd

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"bufio"
	"io"
	"strings"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// NewOISDNSFWProvider creates a new OISD NSFW provider
func NewOISDNSFWProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
	const (
		providerName = "OISD_NSFW"
		providerURL  = "https://nsfw.oisd.nl/domainswild"
		cronSchedule = "22 6 * * *" // Run at 6:22 AM every day
	)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
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
				WithProcessID(id).
				WithCategory("nsfw")

			// SetURL may fail, so handle it separately
			if err := entry.SetURL(line); err != nil {
				log.Error().Err(err).Msgf("error setting URL: %s", line)
				continue
			}

			collector.Submit(entry)
		}

		if err := scanner.Err(); err != nil {
			return err
		}

		return nil
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
