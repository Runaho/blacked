// phishtank is not implemented yet
package phishtank

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"encoding/json"
	"io"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type PhishTankEntry struct {
	URL        string `json:"url"`
	Verified   bool   `json:"verified"`
	VerifyDate string `json:"verification_time"`
}

func NewPhishTankProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
	const (
		providerName = "PHISHTANK"
		providerURL  = "https://data.phishtank.com/data/online-valid.json"
		cronSchedule = "45 */6 * * *" // Every 6 hours at 45 minutes past the hour
	)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		var phishEntries []PhishTankEntry
		id := uuid.New().String()

		// Parse JSON
		decoder := json.NewDecoder(data)
		if err := decoder.Decode(&phishEntries); err != nil {
			log.Error().Err(err).Msg("error decoding PhishTank JSON")
			return err
		}

		// Process each entry
		for _, phishEntry := range phishEntries {
			// Skip unverified entries
			if !phishEntry.Verified {
				continue
			}

			// Create a new entry
			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(id).
				WithCategory("phishing")

			// SetURL may fail, so handle it separately
			if err := entry.SetURL(phishEntry.URL); err != nil {
				log.Error().Err(err).Msgf("error setting URL: %s", phishEntry.URL)
				continue
			}

			collector.Submit(*entry)

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
