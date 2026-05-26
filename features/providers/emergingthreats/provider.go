package emergingthreats

import (
	"bufio"
	"context"
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
	providerName     = "emerging-threats"
	providerCategory = "compromised"
	defaultSourceURL = "https://rules.emergingthreats.net/blockrules/compromised-ips.txt"
	defaultCron      = "0 */12 * * *"
)

func NewEmergingThreatsProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
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

	client := base.BuildCollyClientForProvider(collyClient, opts)

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
		return parseIPList(data, collector, providerName, sourceURL, processID)
	}

	bp := base.NewBaseProvider(providerName, sourceURL, category, client, parseFunc)
	bp.SetCronSchedule(cron)

	// Override Fetch to use HTTPFetcher (plain text, no Colly overhead needed).
	p := &emergingThreatsProvider{
		BaseProvider: bp,
		fetcher:      base.NewHTTPFetcher(0, "", 0),
	}
	p.Register()
	return p
}

// emergingThreatsProvider wraps BaseProvider with an HTTPFetcher override.
type emergingThreatsProvider struct {
	*base.BaseProvider
	fetcher *base.HTTPFetcher
}

// Register wraps the base Register so the registry stores emergingThreatsProvider.
func (p *emergingThreatsProvider) Register() *base.BaseProvider {
	base.RegisterProvider(p)
	return p.BaseProvider
}

// Fetch uses HTTPFetcher instead of Colly for a plain-text feed.
func (p *emergingThreatsProvider) Fetch() (io.Reader, error) {
	return p.FetchWithContext(context.Background())
}

// FetchWithContext uses HTTPFetcher to fetch plain-text data with context support.
func (p *emergingThreatsProvider) FetchWithContext(ctx context.Context) (io.Reader, error) {
	source := p.Source()
	log.Info().Str("provider", providerName).Str("url", source).Msg("Fetching IP list via HTTPFetcher")

	rc, err := p.fetcher.Fetch(source)
	if err != nil {
		return nil, err
	}
	// Read all into memory (~6.6 KB for 476 IPs — negligible).
	body, readErr := io.ReadAll(rc)
	rc.Close()
	if readErr != nil {
		return nil, readErr
	}
	return strings.NewReader(string(body)), nil
}

// parseIPList reads plain IPv4 lines and submits each as an Entry.
// Lines that fail net.ParseIP are silently skipped.
func parseIPList(data io.Reader, collector entry_collector.Collector, source, sourceURL, processID string) error {
	scanner := bufio.NewScanner(data)

	var parsed, skipped int
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Skip comments (ET has none, but be defensive).
		if strings.HasPrefix(line, "#") {
			continue
		}

		ip := net.ParseIP(line)
		if ip == nil {
			log.Debug().Str("line", line).Msg("invalid IP — skipping")
			skipped++
			continue
		}
		// Skip IPv6 — ET list is IPv4 only, but be safe.
		if ip.To4() == nil {
			log.Debug().Str("ip", line).Msg("IPv6 address — skipping")
			skipped++
			continue
		}

		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory(providerCategory)

		entry.Host = ip.String()
		entry.Domain = ip.String()
		entry.SourceURL = sourceURL

		collector.Submit(entry)
		parsed++
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	log.Info().
		Str("provider", source).
		Int("parsed", parsed).
		Int("skipped", skipped).
		Msg("emerging threats parse complete")

	return nil
}
