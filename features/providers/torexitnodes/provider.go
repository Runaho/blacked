package torexitnodes

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

const providerName = "tor-exit-nodes"

// torExitNodesProvider wraps BaseProvider with an HTTPFetcher for plain-text fetch.
type torExitNodesProvider struct {
	*base.BaseProvider
	httpFetcher *base.HTTPFetcher
}

// NewTorExitNodesProvider creates the Tor Exit Nodes provider.
// Returns nil when the provider is disabled in config.
func NewTorExitNodesProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
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
		sourceURL = "https://check.torproject.org/torbulkexitlist"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "*/30 * * * *"
	}
	category := opts.Category
	if category == "" {
		category = "anonymizer"
	}

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
		raw, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("read tor exit nodes: %w", err)
		}
		return parseTorExitNodes(raw, collector, providerName, sourceURL, processID, category)
	}

	fetcher := base.NewHTTPFetcher(0, "", 0)

	bp := base.NewBaseProvider(providerName, sourceURL, category, nil, parseFunc)
	bp.SetCronSchedule(cron)

	p := &torExitNodesProvider{
		BaseProvider: bp,
		httpFetcher:  fetcher,
	}
	p.Register()
	return p
}

// Register wraps the base Register so the registry stores torExitNodesProvider.
func (p *torExitNodesProvider) Register() *base.BaseProvider {
	base.RegisterProvider(p)
	return p.BaseProvider
}

// Fetch uses HTTPFetcher to fetch plain-text data instead of colly.
func (p *torExitNodesProvider) Fetch() (io.Reader, error) {
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

// parseTorExitNodes parses the plain IPv4 list from Tor Project.
// Format: one IPv4 per line, no headers, no comments, no blank lines.
func parseTorExitNodes(data []byte, collector entry_collector.Collector, source, sourceURL, processID, category string) error {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	var valid, skipped int
	for _, line := range lines {
		ip := strings.TrimSpace(line)
		if ip == "" {
			continue
		}

		parsed := net.ParseIP(ip)
		if parsed == nil {
			log.Debug().Str("source", source).Str("ip", ip).Msg("invalid IP — skipping")
			skipped++
			continue
		}
		if parsed.To4() == nil {
			// IPv6 — skip
			log.Debug().Str("source", source).Str("ip", ip).Msg("skipping IPv6 address")
			skipped++
			continue
		}

		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory(category)

		entry.Host = ip
		entry.Domain = ip
		entry.SourceURL = sourceURL

		collector.Submit(entry)
		valid++
	}

	log.Info().
		Str("source", source).
		Int("valid", valid).
		Int("skipped", skipped).
		Msg("tor exit nodes parse complete")

	return nil
}
