package openphish

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

func NewOpenPhishFeedProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
	const (
		providerName = "OPENPHISH"
		providerURL  = "https://openphish.com/feed.txt"
		cronSchedule = "30 */4 * * *" // Every 4 hours (30 minutes past the hour
	)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		scanner := bufio.NewScanner(data)
		id := uuid.New().String()

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(id).
				WithCategory("phishing")

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
