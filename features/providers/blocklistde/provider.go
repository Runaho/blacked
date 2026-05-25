package blocklistde

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const providerName = "blocklist-de"

const defaultSourceURL = "https://lists.blocklist.de/lists/all.txt"

// sublistPriority defines the priority-ordered sublist mapping.
// Earlier entries win when an IP appears in multiple sublists.
var sublistPriority = []struct {
	category string
	url      string
}{
	{"attacker", "https://lists.blocklist.de/lists/strongips.txt"},
	{"botnet", "https://lists.blocklist.de/lists/bots.txt"},
	{"brute-force", "https://lists.blocklist.de/lists/bruteforcelogin.txt"},
	{"brute-force", "https://lists.blocklist.de/lists/ssh.txt"},
	{"spam", "https://lists.blocklist.de/lists/mail.txt"},
	{"web-attack", "https://lists.blocklist.de/lists/apache.txt"},
	{"brute-force", "https://lists.blocklist.de/lists/imap.txt"},
	{"brute-force", "https://lists.blocklist.de/lists/ftp.txt"},
}

// categoryPriority mirrors sublistPriority but deduplicates categories
// to a unique ordered list for resolveCategory.
var categoryPriority = []string{"attacker", "botnet", "brute-force", "spam", "web-attack"}

// sublistFetcher abstracts HTTP sublist fetching for testability.
type sublistFetcher interface {
	Fetch(url string) ([]byte, error)
}

type defaultSublistFetcher struct {
	fetcher *base.HTTPFetcher
}

func (f *defaultSublistFetcher) Fetch(url string) ([]byte, error) {
	rc, err := f.fetcher.Fetch(url)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// NewBlocklistDeProvider creates the Blocklist.de provider.
// Uses HTTPFetcher for sublist resolution (plain text, no JS/cookies).
func NewBlocklistDeProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
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
		cron = "0 */15 * * *"
	}
	category := opts.Category
	if category == "" {
		category = "attacker"
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)

	processID := uuid.New().String()
	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		raw, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("read blocklistde data: %w", err)
		}

		sf := &defaultSublistFetcher{
			fetcher: base.NewHTTPFetcher(30*time.Second, "", 3),
		}
		return parseBlocklistDeData(raw, collector, sourceURL, providerName, processID, sf)
	}

	bp := base.NewBaseProvider(providerName, sourceURL, category, client, parseFunc)
	bp.SetCronSchedule(cron)
	bp.Register()
	return bp
}

// parseBlocklistDeData parses all.txt and resolves categories via sublists.
// sf can be nil — when nil, all entries use fallback category.
func parseBlocklistDeData(
	data []byte,
	collector entry_collector.Collector,
	sourceURL, source, processID string,
	sf sublistFetcher,
) error {
	var totalEntries, skippedCount int

	// Phase 2: resolve categories from sublists (if fetcher available).
	sets := fetchSublistSets(sf)

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for _, line := range lines {
		ip := strings.TrimSpace(line)
		if ip == "" {
			skippedCount++
			continue
		}
		if strings.HasPrefix(ip, "#") {
			skippedCount++
			continue
		}

		parsed := net.ParseIP(ip)
		if parsed == nil {
			log.Debug().Str("ip", ip).Msg("invalid IP — skipping")
			skippedCount++
			continue
		}
		if parsed.To4() == nil {
			log.Debug().Str("ip", ip).Msg("IPv6 detected — skipping")
			skippedCount++
			continue
		}

		category := resolveCategory(ip, sets)

		entry := entries.NewEntry().
			WithSource(source).
			WithProcessID(processID).
			WithCategory(category)

		entry.Host = ip
		entry.Domain = ip
		entry.SourceURL = sourceURL

		collector.Submit(entry)
		totalEntries++
	}

	log.Info().
		Int("entries", totalEntries).
		Int("skipped", skippedCount).
		Msg("blocklist.de parse complete")

	return nil
}

// fetchSublistSets fetches all 8 sublists in parallel and builds
// a category→IP-set map. Fetcher errors are logged and individual
// sublists are skipped; the caller receives partial data.
func fetchSublistSets(sf sublistFetcher) map[string]map[string]bool {
	if sf == nil {
		return nil
	}

	sets := make(map[string]map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	fetchOne := func(url, category string) {
		defer wg.Done()
		data, err := sf.Fetch(url)
		if err != nil {
			log.Warn().Err(err).Str("url", url).
				Msg("sublist fetch failed — continuing with partial data")
			return
		}
		ips := parseIPsToSet(data)
		if len(ips) == 0 {
			return
		}
		mu.Lock()
		if sets[category] == nil {
			sets[category] = make(map[string]bool)
		}
		for ip := range ips {
			sets[category][ip] = true
		}
		mu.Unlock()
	}

	for _, sl := range sublistPriority {
		wg.Add(1)
		go fetchOne(sl.url, sl.category)
	}
	wg.Wait()

	return sets
}

// parseIPsToSet converts raw sublist data (one IP per line) into a set.
func parseIPsToSet(data []byte) map[string]bool {
	set := make(map[string]bool)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for _, line := range lines {
		ip := strings.TrimSpace(line)
		if ip == "" {
			continue
		}
		if strings.HasPrefix(ip, "#") {
			continue
		}
		if net.ParseIP(ip) == nil {
			continue
		}
		// Only IPv4 — blocklist.de sublists are all IPv4.
		if net.ParseIP(ip).To4() == nil {
			continue
		}
		set[ip] = true
	}
	return set
}

// resolveCategory determines the category for an IP by checking
// sublist sets in priority order. Falls back to "attacker".
func resolveCategory(ip string, sets map[string]map[string]bool) string {
	if sets == nil {
		return "attacker"
	}
	for _, cat := range categoryPriority {
		if sets[cat] != nil && sets[cat][ip] {
			return cat
		}
	}
	return "attacker"
}
