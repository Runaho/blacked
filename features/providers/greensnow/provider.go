package greensnow

import (
	"io"
	"net"
	"strings"

	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

const (
	providerName     = "greensnow"
	providerCategory = "attacker"
	defaultSourceURL = "https://blocklist.greensnow.co/greensnow.txt"
	defaultCron      = "0 */2 * * *"
	defaultWorkers   = 4
	defaultBatchSize = 1000
)

// NewGreenSnowProvider creates a new GreenSnow provider.
// GreenSnow publishes honeypot-verified attacker IPs as plain text, one per line.
func NewGreenSnowProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	opts, ok := cfg.Providers[providerName]
	if !ok || opts == nil {
		opts = &config.ProviderOptions{}
	}
	if opts.Enabled != nil && !*opts.Enabled {
		log.Info().Str("provider", providerName).Msg("provider disabled — skipping")
		return nil
	}

	sourceURL := opts.SourceURL
	if sourceURL == "" {
		sourceURL = defaultSourceURL
	}
	cron := opts.Cron
	if cron == "" {
		cron = defaultCron
	}
	category := opts.Category
	if category == "" {
		category = providerCategory
	}

	workers := opts.ParserWorkers
	if workers <= 0 {
		workers = defaultWorkers
	}
	batchSize := opts.ParserBatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		return base.ParseLinesParallel(data, collector, providerName, workers, batchSize,
			func(line, processID string) (*entries.Entry, error) {
				return parseGreenSnowLine(line, processID)
			})
	}

	provider := base.NewBaseProvider(providerName, sourceURL, category, client, parseFunc)
	provider.SetCronSchedule(cron)
	provider.Register()
	return provider
}

// parseGreenSnowLine parses a single line from the GreenSnow feed.
// Returns nil entry for empty lines, invalid IPs, or IPv6 addresses.
func parseGreenSnowLine(line, processID string) (*entries.Entry, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	ip := net.ParseIP(line)
	if ip == nil {
		log.Debug().Str("ip", line).Msg("greensnow: not a valid IP — skipping")
		return nil, nil
	}

	if ip.To4() == nil {
		log.Debug().Str("ip", line).Msg("greensnow: IPv6 — skipping")
		return nil, nil
	}

	entry := entries.NewEntry().
		WithSource(providerName).
		WithProcessID(processID).
		WithCategory(providerCategory)

	// IP entries: Host and Domain are both the IP string.
	// SetURL is NOT called — no domain extraction needed.
	entry.Host = line
	entry.Domain = line
	entry.SubDomains = nil

	return entry, nil
}
