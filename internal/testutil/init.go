package testutil

import (
	"blacked/features/entry_collector"
	ic "blacked/internal/colly"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/logger"
	"context"
	"database/sql"
	"testing"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
)

func boolPtr(b bool) *bool { return &b }

func defaultProviderConfigs() map[string]*config.ProviderOptions {
	return map[string]*config.ProviderOptions{
		"oisd-big": {
			Enabled:         boolPtr(true),
			SourceURL:       "https://big.oisd.nl/domainswild2",
			Cron:            "0 6 * * *",
			Category:        "blocklist",
			ParserWorkers:   4,
			ParserBatchSize: 1000,
		},
		"oisd-nsfw": {
			Enabled:         boolPtr(true),
			SourceURL:       "https://nsfw.oisd.nl/domainswild",
			Cron:            "22 6 * * *",
			Category:        "nsfw",
			ParserWorkers:   4,
			ParserBatchSize: 1000,
		},
		"urlhaus-online": {
			Enabled:         boolPtr(true),
			SourceURL:       "https://urlhaus.abuse.ch/downloads/text/",
			Cron:            "15 */2 * * *",
			Category:        "malware",
			ParserWorkers:   4,
			ParserBatchSize: 1000,
		},
		"openphish-feed": {
			Enabled:         boolPtr(false),
			SourceURL:       "https://openphish.com/feed.txt",
			Cron:            "30 */4 * * *",
			Category:        "phishing",
			ParserWorkers:   4,
			ParserBatchSize: 1000,
		},
		"phishtank-online-valid": {
			Enabled:         boolPtr(false),
			SourceURL:       "https://data.phishtank.com/data/{api_key}/online-valid.json",
			Cron:            "45 */6 * * *",
			Category:        "phishing",
			ParserWorkers:   4,
			ParserBatchSize: 1000,
		},
		"threatfox-online": {
			Enabled:         boolPtr(true),
			SourceURL:       "https://threatfox-api.abuse.ch/v2/files/exports/{token}/recent.json",
			DumpSourceURL:   "https://threatfox-api.abuse.ch/v2/files/exports/{token}/full.json.zip",
			Cron:            "0 */2 * * *",
			Category:        "threat_intel",
			ParserWorkers:   4,
			ParserBatchSize: 1000,
		},
	}
}

func EnsureProviderConfig(t *testing.T, name string) {
	cfg := config.GetConfig()
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*config.ProviderOptions)
	}
	defaults := defaultProviderConfigs()
	if opts, ok := defaults[name]; ok {
		cfg.Providers[name] = opts
	}
}

func Initialize(t *testing.T) (ctx context.Context, _db *sql.DB, cc *colly.Collector, err error) {
	logger.InitializeLogger()
	err = config.InitConfig()
	assert.NoError(t, err, "Should initialize config without error")

	_db, err = db.GetTestDB()
	assert.NoError(t, err, "Expected no error while obtaining DB")

	db.EnsureDBSchemaExists(db.WithTesting(true))

	// Initialize the pond collector so provider tests don't fail with "Entry collector not set"
	ctx = context.Background()
	entry_collector.InitPondCollector(ctx, _db)

	cc, err = ic.InitCollyClient()
	assert.NoError(t, err, "Expected no error while initializing colly client")

	return
}
