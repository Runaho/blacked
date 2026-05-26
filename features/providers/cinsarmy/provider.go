package cinsarmy

import (
	"fmt"
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

const providerName = "cins-army"

// cinsArmyProvider wraps BaseProvider with an HTTPFetcher for plain-text fetch.
type cinsArmyProvider struct {
	*base.BaseProvider
	httpFetcher *base.HTTPFetcher
}

// NewCINSArmyProvider creates the CINS Army provider.
// Returns nil when the provider is disabled in config.
func NewCINSArmyProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
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
		sourceURL = "https://cinsscore.com/list/ci-badguys.txt"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "*/30 * * * *"
	}
	category := opts.Category
	if category == "" {
		category = "scanner"
	}

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
		raw, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("read cins data: %w", err)
		}
		return parseCINSData(raw, collector, providerName, sourceURL, processID)
	}

	fetcher := base.NewHTTPFetcher(0, "", 0)

	bp := base.NewBaseProvider(providerName, sourceURL, category, nil, parseFunc)
	bp.SetCronSchedule(cron)

	p := &cinsArmyProvider{
		BaseProvider: bp,
		httpFetcher:  fetcher,
	}
	p.Register()
	return p
}

// Register wraps the base Register so the registry stores cinsArmyProvider.
func (p *cinsArmyProvider) Register() *base.BaseProvider {
	base.RegisterProvider(p)
	return p.BaseProvider
}

// Fetch uses HTTPFetcher to fetch plain-text data instead of colly.
func (p *cinsArmyProvider) Fetch() (io.Reader, error) {
	rc, err := p.httpFetcher.Fetch(p.SourceURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", p.SourceURL, err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(data) == 0 {
		return nil, base.ErrEmptyResponse
	}

	log.Info().Str("source", p.SourceURL).Int("bytes", len(data)).
		Msg("Fetched data from source")

	return strings.NewReader(string(data)), nil
}

// parseCINSData parses plain-text IPv4 data (one IP per line).
func parseCINSData(data []byte, collector entry_collector.Collector, source, sourceURL, processID string) error {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	var valid, skipped int
	for _, line := range lines {
		ip := strings.TrimSpace(line)
		if ip == "" {
			continue
		}
		if strings.HasPrefix(ip, "#") {
			continue
		}

		parsed := net.ParseIP(ip)
		if parsed == nil {
			skipped++
			continue
		}
		if parsed.To4() == nil {
			// IPv6 — skip
			log.Debug().Str("ip", ip).Msg("skipping IPv6 address")
			skipped++
			continue
		}

		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory("scanner")

		entry.Host = ip
		entry.Domain = ip
		entry.SourceURL = sourceURL

		collector.Submit(entry)
		valid++
	}

	log.Info().
		Int("valid", valid).
		Int("skipped", skipped).
		Msg("cins-army parse complete")

	return nil
}
