package threatfox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"blacked/internal/db"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

const providerName = "threatfox-online"

// RepositoryProvider abstracts the subset of BlacklistRepository that
// shouldUseDump needs, making it mockable in unit tests.
type RepositoryProvider interface {
	StreamEntriesCountBySource(ctx context.Context, source string) (int, error)
	GetMaxCreatedAtBySource(ctx context.Context, source string) (int64, error)
}

// --- IOC types from the ThreatFox API ---

type threatFoxIOC struct {
	IOCValue         string `json:"ioc_value"`
	IOCType          string `json:"ioc_type"`
	ThreatType       string `json:"threat_type"`
	MalwarePrintable string `json:"malware_printable,omitempty"`
	ConfidenceLevel  int    `json:"confidence_level"`
	FirstSeenUTC     string `json:"first_seen_utc,omitempty"`
	Reporter         string `json:"reporter,omitempty"`
}

// threatFoxProvider wraps BaseProvider with dynamic URL resolution so
// that gap detection is re-evaluated on every Fetch() call, not frozen
// at construction time. It also overrides Source() for accurate logging.
type threatFoxProvider struct {
	*base.BaseProvider
	recentURL string
	dumpURL   string
	apiKey    string
}

// --- Public constructor ---

func NewThreatFoxProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
	opts, ok := cfg.Providers[providerName]
	if !ok || opts == nil {
		opts = &config.ProviderOptions{}
	}
	if opts.Enabled != nil && !*opts.Enabled {
		log.Info().Str("provider", providerName).Msg("provider disabled — skipping")
		return nil
	}

	recentURL := opts.SourceURL
	if recentURL == "" {
		recentURL = "https://threatfox-api.abuse.ch/v2/files/exports/{token}/recent.json"
	}
	dumpURL := opts.DumpSourceURL
	if dumpURL == "" {
		dumpURL = "https://threatfox-api.abuse.ch/v2/files/exports/{token}/full.json.zip"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "0 */2 * * *"
	}
	category := opts.Category
	if category == "" {
		category = "threat_intel"
	}

	client := base.BuildCollyClientForProvider(collyClient, opts)
	// ThreatFox responses routinely exceed 1MB (recent ~2MB, dump ~50MB).
	if client != nil {
		client.MaxBodySize = 100 * 1024 * 1024 // 100 MB
	}

	parseFunc := func(data io.Reader, collector entry_collector.Collector, processID string) error {
		raw, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("read threatfox data: %w", err)
		}
		return parseThreatFoxResponse(raw, collector, providerName, processID)
	}

	bp := base.NewBaseProvider(providerName, recentURL, category, client, parseFunc)
	bp.SetCronSchedule(cron)

	p := &threatFoxProvider{
		BaseProvider: bp,
		recentURL:    recentURL,
		dumpURL:      dumpURL,
		apiKey:       opts.APIKey,
	}
	log.Info().Str("provider", providerName).Str("apiKey", opts.APIKey).Msg("ThreatFox provider constructed with API key")
	p.Register()
	return p
}

// Register wraps the base Register so the registry stores threatFoxProvider
// (with overridden Fetch/Source), not the embedded BaseProvider.
func (p *threatFoxProvider) Register() *base.BaseProvider {
	base.RegisterProvider(p)
	return p.BaseProvider
}

// FetchWithContext delegates to Fetch for context-aware timeout handling.
// Required because process.go calls FetchWithContext(), not Fetch().
// Without this override, BaseProvider.FetchWithContext() would use CollyClient.Clone()
// which doesn't have proper URL resolution for {token} replacement.
func (p *threatFoxProvider) FetchWithContext(ctx context.Context) (io.Reader, error) {
	return p.Fetch()
}

// Fetch re-evaluates gap detection on every call, so the URL is not
// frozen at construction time — subsequent cron runs can switch between
// dump and recent feed as needed.
func (p *threatFoxProvider) Fetch() (io.Reader, error) {
	targetURL := resolveThreatFoxURL(p.recentURL, p.apiKey)
	if shouldUseDump(providerName, nil) {
		log.Info().Str("provider", providerName).
			Msg("gap or empty DB — using full dump")
		targetURL = resolveThreatFoxURL(p.dumpURL, p.apiKey)
	}

	c := p.CollyClient.Clone()
	if c == nil {
		c = colly.NewCollector()
	}
	c.MaxBodySize = 100 * 1024 * 1024

	var body []byte
	var fetchErr error
	c.OnResponse(func(r *colly.Response) {
		body = r.Body
		log.Info().Str("source", targetURL).Int("bytes", len(body)).
			Msg("Fetched data from source")
	})
	c.OnError(func(r *colly.Response, err error) {
		fetchErr = fmt.Errorf("colly error for %s (status %d): %w", targetURL, r.StatusCode, err)
		log.Err(err).Str("url", targetURL).Int("code", r.StatusCode).
			Msg("Colly error when fetching data")
	})

	log.Info().Msgf("Fetching %s", targetURL)
	if err := c.Visit(targetURL); err != nil {
		return nil, fmt.Errorf("visit %s: %w", targetURL, err)
	}
	c.Wait()

	if fetchErr != nil {
		return nil, fetchErr
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response from %s", targetURL)
	}
	return bytes.NewReader(body), nil
}

// Source returns the dynamically resolved fetch URL for accurate logging.
func (p *threatFoxProvider) Source() string {
	if shouldUseDump(providerName, nil) {
		return resolveThreatFoxURL(p.dumpURL, p.apiKey)
	}
	return resolveThreatFoxURL(p.recentURL, p.apiKey)
}

// --- Gap detection (testable pure functions) ---

func determineFetchURL(recentURL, dumpURL, apiKey string, repo RepositoryProvider) string {
	if shouldUseDump(providerName, repo) {
		log.Info().Str("provider", providerName).
			Msg("no entries or gap detected — using full dump for initial load")
		return resolveThreatFoxURL(dumpURL, apiKey)
	}

	log.Info().Str("provider", providerName).
		Msg("entries found and no gap — using recent feed")
	return resolveThreatFoxURL(recentURL, apiKey)
}

func shouldUseDump(source string, repo RepositoryProvider) bool {
	if repo == nil {
		rwDB, err := db.GetWriteDB()
		if err != nil {
			log.Warn().Err(err).Str("provider", source).
				Msg("cannot connect to DB — falling back to recent feed")
			return false
		}
		repo = repository.NewSQLiteRepository(rwDB)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := repo.StreamEntriesCountBySource(ctx, source)
	if err != nil {
		log.Warn().Err(err).Str("provider", source).
			Msg("cannot count entries — falling back to recent feed")
		return false
	}
	if count == 0 {
		return true
	}

	maxCreatedAt, err := repo.GetMaxCreatedAtBySource(ctx, source)
	if err != nil || maxCreatedAt == 0 {
		return false
	}

	age := time.Since(time.Unix(0, maxCreatedAt))
	if age > 48*time.Hour {
		log.Info().Str("provider", source).
			Dur("age", age).
			Msg("gap detected — entries older than 48h, using full dump")
		return true
	}

	return false
}

// --- URL resolution ---

func resolveThreatFoxURL(template string, apiKey string) string {
	if template == "" {
		return ""
	}
	if apiKey == "" {
		log.Warn().Str("provider", providerName).
			Msg("API key is empty — feed will likely fail")
		return template
	}
	return strings.ReplaceAll(template, "{token}", apiKey)
}

// --- Response parsing ---

func parseThreatFoxResponse(data []byte, collector entry_collector.Collector, source, processID string) error {
	if isZip(data) {
		log.Info().Int("bytes", len(data)).
			Msg("detected zip archive — extracting full.json")
		zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("open zip: %w", err)
		}
		var jsonData []byte
		for _, f := range zipReader.File {
			if f.Name == "full.json" {
				rc, err := f.Open()
				if err != nil {
					return fmt.Errorf("open zip entry %s: %w", f.Name, err)
				}
				jsonData, err = io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return fmt.Errorf("read zip entry %s: %w", f.Name, err)
				}
				break
			}
		}
		if jsonData == nil {
			return fmt.Errorf("full.json not found in zip archive")
		}
		log.Info().Int("json_bytes", len(jsonData)).
			Msg("extracted full.json from zip")
		return parseThreatFoxJSON(jsonData, collector, source, processID)
	}
	return parseThreatFoxJSON(data, collector, source, processID)
}

func isZip(data []byte) bool {
	return len(data) >= 4 && data[0] == 0x50 && data[1] == 0x4B &&
		data[2] == 0x03 && data[3] == 0x04
}

func parseThreatFoxJSON(data []byte, collector entry_collector.Collector, source, processID string) error {
	var response map[string][]threatFoxIOC
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("unmarshal threatfox json: %w", err)
	}

	var totalEntries, skippedCount int
	for _, iocs := range response {
		for _, ioc := range iocs {
			entry, err := iocToEntry(&ioc, source, processID)
			if err != nil {
				skippedCount++
				continue
			}
			if entry != nil {
				collector.Submit(entry)
				totalEntries++
			} else {
				skippedCount++
			}
		}
	}

	log.Info().
		Int("entries", totalEntries).
		Int("skipped", skippedCount).
		Msg("threatfox parse complete")

	return nil
}

// --- IOC → Entry mapping ---

func iocToEntry(ioc *threatFoxIOC, source, processID string) (*entries.Entry, error) {
	// Entry category is set to threat_type (e.g. "botnet_cc", "payload_delivery")
	// — this is intentional: ThreatFox IOCs carry their type from the feed.
	entry := entries.NewEntry().
		WithSource(source).
		WithProcessID(processID).
		WithCategory(ioc.ThreatType)

	switch ioc.IOCType {
	case "ip:port":
		host, _, err := net.SplitHostPort(strings.TrimSpace(ioc.IOCValue))
		if err != nil {
			log.Debug().Err(err).Str("value", ioc.IOCValue).
				Msg("failed to split ip:port")
			return nil, nil
		}
		if net.ParseIP(host) == nil {
			log.Debug().Str("host", host).
				Msg("not a valid IP after split")
			return nil, nil
		}
		entry.WithIP(host)

	case "domain":
		if ioc.IOCValue == "" || ioc.IOCValue == "." {
			log.Debug().Msg("empty domain IOC — skipping")
			return nil, nil
		}
		if err := entry.SetURL(ioc.IOCValue); err != nil {
			return nil, nil
		}

	case "url":
		if ioc.IOCValue == "" {
			log.Debug().Msg("empty url IOC — skipping")
			return nil, nil
		}
		if err := entry.SetURL(ioc.IOCValue); err != nil {
			return nil, nil
		}

	default:
		// skip hashes and unknown types
		return nil, nil
	}

	return entry, nil
}
