package rtbh

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
	providerName     = "rtbh-turkey"
	defaultSourceURL = "https://list.rtbh.com.tr/output.txt"
	defaultCategory  = "government-feed"
	defaultCron      = "*/30 * * * *"
)

// NewRTBHTurkeyProvider creates a new RTBH Turkey provider from config.
func NewRTBHTurkeyProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
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
		category = defaultCategory
	}

	workers := opts.ParserWorkers
	if workers <= 0 {
		workers = 4
	}
	batchSize := opts.ParserBatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		return base.ParseLinesParallel(data, collector, providerName, workers, batchSize, func(line, processID string) (*entries.Entry, error) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				return nil, nil
			}

			ip := net.ParseIP(line)
			if ip == nil {
				return nil, nil // skip non-IP lines
			}
			if ip.To4() == nil {
				return nil, nil // skip IPv6
			}

		entry := entries.NewEntry().
			WithSource(providerName).
			WithProcessID(processID).
			WithCategory(category).
			WithIP(ip.String())

		return entry, nil
		})
	}

	provider := base.NewBaseProvider(
		providerName,
		sourceURL,
		category,
		client,
		parseFunc,
	)

	provider.
		SetCronSchedule(cron).
		Register()

	return provider
}
