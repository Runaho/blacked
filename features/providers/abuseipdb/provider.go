package abuseipdb

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// AbuseIPDBResponse represents the JSON response structure from AbuseIPDB API
type AbuseIPDBResponse struct {
	Data []AbuseIPDBEntry `json:"data"`
}

// AbuseIPDBEntry represents a single entry from AbuseIPDB API
type AbuseIPDBEntry struct {
	IPAddress            string `json:"ipAddress"`
	AbuseConfidenceScore int    `json:"abuseConfidenceScore"`
	CountryCode          string `json:"countryCode"`
}

// NewAbuseIPDBProvider creates the AbuseIPDB provider
func NewAbuseIPDBProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	const providerName = "abuseipdb"

	opts, ok := cfg.Providers[providerName]
	if !ok || opts == nil {
		opts = &config.ProviderOptions{}
	}
	if opts.Enabled != nil && !*opts.Enabled {
		log.Info().Str("provider", providerName).Msg("provider disabled — skipping")
		return nil
	}

	// Build URL from config
	sourceURL := opts.SourceURL
	if sourceURL == "" {
		// Use url field as fallback
		sourceURL = opts.URL
	}
	if sourceURL == "" {
		sourceURL = "https://api.abuseipdb.com/api/v2/blacklist"
	}

	// Add query parameters from config
	confidenceMin := 90
	limit := 10000

	// Check for custom config values from Extra field
	if opts.Extra != nil {
		if cm, ok := opts.Extra["confidence_minimum"]; ok && cm != "" {
			if cmInt, err := strconv.Atoi(cm); err == nil {
				confidenceMin = cmInt
			}
		}
		if l, ok := opts.Extra["limit"]; ok && l != "" {
			if lInt, err := strconv.Atoi(l); err == nil {
				limit = lInt
			}
		}
	}

	// Only add query params if using the default URL
	if sourceURL == "https://api.abuseipdb.com/api/v2/blacklist" {
		sourceURL = fmt.Sprintf("%s?confidenceMinimum=%d&limit=%d", sourceURL, confidenceMin, limit)
	}

	// If API key is empty, warn and skip
	if opts.APIKey == "" {
		log.Warn().Str("provider", providerName).Msg("AbuseIPDB API key not configured — skipping")
		return nil
	}

	cron := opts.Cron
	if cron == "" {
		cron = "0 0 * * *" // Daily at midnight
	}

	workers := opts.ParserWorkers
	if workers <= 0 {
		workers = 4
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)
	// Set custom headers for AbuseIPDB API
	client.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Key", opts.APIKey)
		r.Headers.Set("Accept", "application/json")
	})

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		var response AbuseIPDBResponse
		id := uuid.New().String()

		decoder := json.NewDecoder(data)
		if err := decoder.Decode(&response); err != nil {
			log.Error().Err(err).Msg("error decoding AbuseIPDB JSON")
			return err
		}

		return base.ProcessEntriesParallel(response.Data, collector, workers, func(abuseEntry AbuseIPDBEntry, processID string) (*entries.Entry, error) {
			entry := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(processID).
				WithCategory("abuse")

			// Set the IP as host
			entry.Host = abuseEntry.IPAddress

			return entry, nil
		}, id)
	}

	provider := base.NewBaseProvider(
		providerName,
		sourceURL,
		"abuse",
		client,
		parseFunc,
	)

	provider.
		SetCronSchedule(cron).
		Register()

	return provider
}