package abuseipdb

import (
	"testing"

	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
)

func TestAbuseIPDBProviderCreation(t *testing.T) {
	// Create a minimal config
	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"abuseipdb": {
				Enabled:      boolPtr(true),
				SourceURL:   "https://api.abuseipdb.com/api/v2/blacklist",
				APIKey:      "test_key",
				Cron:        "0 0 * * *",
				Category:    "abuse",
				Extra: map[string]string{
					"confidence_minimum": "90",
					"limit":              "10000",
				},
			},
		},
	}

	// Create colly client
	cc := colly.NewCollector()

	// Create provider
	provider := NewAbuseIPDBProvider(cfg, cc)

	// Verify provider is created
	assert.NotNil(t, provider)
	assert.Equal(t, "abuseipdb", provider.GetName())
	assert.Equal(t, "https://api.abuseipdb.com/api/v2/blacklist?confidenceMinimum=90&limit=10000", provider.Source())
}

func TestAbuseIPDBProviderDisabled(t *testing.T) {
	// Create a config with provider disabled
	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"abuseipdb": {
				Enabled:      boolPtr(false),
				SourceURL:   "https://api.abuseipdb.com/api/v2/blacklist",
				APIKey:      "test_key",
				Cron:        "0 0 * * *",
				Category:    "abuse",
			},
		},
	}

	// Create colly client
	cc := colly.NewCollector()

	// Create provider
	provider := NewAbuseIPDBProvider(cfg, cc)

	// Verify provider is nil when disabled
	assert.Nil(t, provider)
}

func TestAbuseIPDBProviderNoAPIKey(t *testing.T) {
	// Create a config without API key
	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"abuseipdb": {
				Enabled:      boolPtr(true),
				SourceURL:   "https://api.abuseipdb.com/api/v2/blacklist",
				APIKey:      "",
				Cron:        "0 0 * * *",
				Category:    "abuse",
			},
		},
	}

	// Create colly client
	cc := colly.NewCollector()

	// Create provider
	provider := NewAbuseIPDBProvider(cfg, cc)

	// Verify provider is nil when no API key
	assert.Nil(t, provider)
}

func boolPtr(b bool) *bool {
	return &b
}