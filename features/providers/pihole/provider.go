package pihole

import (
	"fmt"
	"io"
	"strings"

	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

const providerName = "pihole"

// piholeProvider wraps BaseProvider with an HTTPFetcher for plain-text hosts file fetch.
type piholeProvider struct {
	*base.BaseProvider
	httpFetcher *base.HTTPFetcher
}

// NewPiholeProvider creates the Pi-hole Adlist provider.
// Returns nil when the provider is disabled in config.
func NewPiholeProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
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
		sourceURL = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "0 */12 * * *"
	}
	category := opts.Category
	if category == "" {
		category = "adlist"
	}

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
		raw, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("read pihole data: %w", err)
		}
		return parsePiholeData(raw, collector, providerName, sourceURL, processID)
	}

	fetcher := base.NewHTTPFetcher(0, "", 0)

	bp := base.NewBaseProvider(providerName, sourceURL, category, nil, parseFunc)
	bp.SetCronSchedule(cron)

	p := &piholeProvider{
		BaseProvider: bp,
		httpFetcher:  fetcher,
	}
	p.Register()
	return p
}

// Register wraps the base Register so the registry stores piholeProvider.
func (p *piholeProvider) Register() *base.BaseProvider {
	base.RegisterProvider(p)
	return p.BaseProvider
}

// Fetch uses HTTPFetcher to fetch plain-text hosts file data instead of colly.
func (p *piholeProvider) Fetch() (io.Reader, error) {
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

// parsePiholeData parses hosts file format data (0.0.0.0 domain.com or 127.0.0.1 domain.com).
func parsePiholeData(data []byte, collector entry_collector.Collector, source, sourceURL, processID string) error {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	var valid, skipped int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			// Comment line - skip
			skipped++
			continue
		}

		// Parse hosts file format: IP domain
		parts := strings.Fields(line)
		if len(parts) < 2 {
			// Not a valid hosts entry
			skipped++
			continue
		}

		ip := parts[0]
		domain := parts[1]

		// Skip localhost and broadcast addresses
		if domain == "localhost" || domain == "localhost.localdomain" || domain == "local" ||
			domain == "broadcasthost" || strings.HasPrefix(domain, "ip6-") || domain == "ip6-localhost" ||
			domain == "ip6-loopback" || strings.HasPrefix(domain, "ff") {
			skipped++
			continue
		}

		// Skip IPv6 entries
		if ip == "::1" || ip == "::" || strings.Contains(ip, ":") {
			skipped++
			continue
		}

		// Skip 0.0.0.0 and 255.255.255.255 entries
		if ip == "0.0.0.0" || ip == "255.255.255.255" {
			skipped++
			continue
		}

		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory("adlist")

		entry.Host = domain
		entry.Domain = domain
		entry.SourceURL = sourceURL

		collector.Submit(entry)
		valid++
	}

	log.Info().
		Int("valid", valid).
		Int("skipped", skipped).
		Msg("pihole parse complete")

	return nil
}