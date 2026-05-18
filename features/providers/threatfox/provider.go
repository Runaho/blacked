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
// determineFetchURL needs, making it mockable in unit tests.
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

	sourceURL := opts.SourceURL
	if sourceURL == "" {
		sourceURL = "https://threatfox-api.abuse.ch/v2/files/exports/{token}/recent.json"
	}
	dumpSourceURL := opts.DumpSourceURL
	if dumpSourceURL == "" {
		dumpSourceURL = "https://threatfox-api.abuse.ch/v2/files/exports/{token}/full.json.zip"
	}
	cron := opts.Cron
	if cron == "" {
		cron = "0 */2 * * *"
	}
	category := opts.Category
	if category == "" {
		category = "threat_intel"
	}

	apiKey := opts.APIKey

	// Determine which URL to fetch.
	fetchURL := determineFetchURL(sourceURL, dumpSourceURL, apiKey, nil)
	client := base.BuildCollyClientForProvider(collyClient, opts)

	parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
		raw, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("read threatfox data: %w", err)
		}
		return parseThreatFoxResponse(raw, collector, providerName)
	}

	provider := base.NewBaseProvider(
		providerName,
		fetchURL,
		category,
		client,
		parseFunc,
	)
	provider.
		SetCronSchedule(cron).
		Register()

	return provider
}

// determineFetchURL decides which URL to use. It is a separate function
// so it can be tested in isolation. The first return value is the resolved
// fetch URL; the second is true when the dump URL was selected.
func determineFetchURL(recentURL, dumpURL, apiKey string, repo RepositoryProvider) string {
	// Prefer dump when DB is empty or has a gap.
	if shouldUseDump(apiKey, providerName, repo) {
		log.Info().Str("provider", providerName).
			Msg("no entries or gap detected — using full dump for initial load")
		return resolveThreatFoxURL(dumpURL, apiKey)
	}

	log.Info().Str("provider", providerName).
		Msg("entries found and no gap — using recent feed")
	return resolveThreatFoxURL(recentURL, apiKey)
}

// shouldUseDump returns true when the dump should be used: either the DB
// is empty for this source or a time gap exists between the most recent
// entry in the DB and now. When repo is nil, it opens one from the DB package.
func shouldUseDump(apiKey, source string, repo RepositoryProvider) bool {
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

	// GAP detection: if the newest entry is older than 48 hours,
	// the recent feed won't cover all time, so pull the dump.
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

func parseThreatFoxResponse(data []byte, collector entry_collector.Collector, source string) error {
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
		return parseThreatFoxJSON(jsonData, collector, source)
	}
	return parseThreatFoxJSON(data, collector, source)
}

func isZip(data []byte) bool {
	return len(data) > 4 && data[0] == 0x50 && data[1] == 0x4B &&
		data[2] == 0x03 && data[3] == 0x04
}

func parseThreatFoxJSON(data []byte, collector entry_collector.Collector, source string) error {
	var response map[string][]threatFoxIOC
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("unmarshal threatfox json: %w", err)
	}

	var totalEntries, skippedCount int
	for _, iocs := range response {
		for _, ioc := range iocs {
			entry, err := iocToEntry(&ioc, source)
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

func iocToEntry(ioc *threatFoxIOC, source string) (*entries.Entry, error) {
	entry := entries.NewEntry().
		WithSource(source).
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
		if err := entry.SetURL("//" + host); err != nil {
			return nil, nil
		}

	case "domain":
		if err := entry.SetURL(ioc.IOCValue); err != nil {
			return nil, nil
		}

	case "url":
		if err := entry.SetURL(ioc.IOCValue); err != nil {
			return nil, nil
		}

	default:
		// skip hashes and unknown types
		return nil, nil
	}

	return entry, nil
}
