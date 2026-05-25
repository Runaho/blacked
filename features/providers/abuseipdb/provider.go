package abuseipdb

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"blacked/internal/resilience"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

const providerName = "abuseipdb"

// --- AbuseIPDB API Response Types ---

type AbuseIPDBResponse struct {
	Meta struct {
		GeneratedAt string `json:"generatedAt"`
	} `json:"meta"`
	Data []AbuseIPDBEntry `json:"data"`
}

type AbuseIPDBEntry struct {
	IPAddress            string `json:"ipAddress"`
	CountryCode          string `json:"countryCode"`
	AbuseConfidenceScore int    `json:"abuseConfidenceScore"`
	LastReportedAt       string `json:"lastReportedAt"`
}

// NewAbuseIPDBProvider creates a new AbuseIPDB provider
func NewAbuseIPDBProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	opts, ok := cfg.Providers[providerName]
	if !ok || opts == nil {
		opts = &config.ProviderOptions{}
	}
	if opts.Enabled != nil && !*opts.Enabled {
		log.Info().Str("provider", providerName).Msg("provider disabled — skipping")
		return nil
	}

	apiKey := opts.APIKey
	if apiKey == "" {
		log.Warn().Str("provider", providerName).Msg("API key not configured — skipping")
		return nil
	}

	apiURL := opts.URL
	if apiURL == "" {
		apiURL = "https://api.abuseipdb.com/api/v2/blacklist"
	}

	confidenceMinimum := 90
	if v, ok := opts.Extra["confidence_minimum"]; ok {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			confidenceMinimum = i
		}
	}

	limit := 10000
	if v, ok := opts.Extra["limit"]; ok {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			limit = i
		}
	}

	cron := opts.Cron
	if cron == "" {
		cron = "0 0 * * *" // Daily
	}

	category := opts.Category
	if category == "" {
		category = "abuse"
	}

	// Build URL with query params
	sourceURL := apiURL + "?confidenceMinimum=" + strconv.Itoa(confidenceMinimum) + "&limit=" + strconv.Itoa(limit)

	// Resilience config
	resilienceCfg := resilience.DefaultProviderResilienceConfig(providerName)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		var response AbuseIPDBResponse
		if err := json.NewDecoder(data).Decode(&response); err != nil {
			log.Error().Err(err).Str("provider", providerName).Msg("failed to parse JSON response")
			return err
		}

		processID := time.Now().UTC().Format("20060102-150405")

		for _, entry := range response.Data {
			if entry.IPAddress == "" {
				continue
			}

			e := entries.NewEntry().
				WithSource(providerName).
				WithProcessID(processID).
				WithCategory(category)

			e.WithIP(entry.IPAddress)

			if entry.AbuseConfidenceScore > 0 {
				e.WithConfidence(float64(entry.AbuseConfidenceScore) / 100.0)
			}

			collector.Submit(e)
		}

		log.Info().
			Str("provider", providerName).
			Int("count", len(response.Data)).
			Str("generated_at", response.Meta.GeneratedAt).
			Msg("parsed AbuseIPDB response")

		return nil
	}

	// Create base provider with nil CollyClient (will use HTTPClient)
	bp := base.NewBaseProvider(providerName, sourceURL, category, nil, parseFunc)

	// Set HTTP client for API calls
	bp.HTTPClient = &http.Client{
		Timeout: 2 * time.Minute,
	}
	bp.HTTPHeaders = map[string]string{
		"Key":         apiKey,
		"Accept":      "application/json",
		"User-Agent":  "blacked/1.0",
	}

	// Configure resilience settings
	bp.SetResilienceConfig(&resilienceCfg)

	bp.
		SetCronSchedule(cron).
		Register()

	log.Info().
		Str("provider", providerName).
		Int("confidence_minimum", confidenceMinimum).
		Int("limit", limit).
		Str("category", category).
		Msg("provider registered")

	return bp
}