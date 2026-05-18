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

type threatFoxIOC struct {
	IOCValue         string `json:"ioc_value"`
	IOCType          string `json:"ioc_type"`
	ThreatType       string `json:"threat_type"`
	MalwarePrintable string `json:"malware_printable,omitempty"`
	ConfidenceLevel  int    `json:"confidence_level"`
	FirstSeenUTC     string `json:"first_seen_utc,omitempty"`
	Reporter         string `json:"reporter,omitempty"`
}

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

	// Dual fetch: full dump only when DB is empty
	apiKey := opts.APIKey
	rwDB, err := db.GetWriteDB()
	if err == nil {
		repo := repository.NewSQLiteRepository(rwDB)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		count, _ := repo.StreamEntriesCountBySource(ctx, providerName)
		cancel()
		if count == 0 && dumpSourceURL != "" {
			log.Info().Str("provider", providerName).Msg("no entries found — using full dump for initial load")
			sourceURL = dumpSourceURL
		}
	}
	fetchURL := resolveThreatFoxURL(sourceURL, apiKey)

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

func resolveThreatFoxURL(template string, apiKey string) string {
	if template == "" {
		return ""
	}
	if apiKey == "" {
		log.Warn().Str("provider", providerName).Msg("API key is empty — feed will likely fail")
		return template
	}
	return strings.ReplaceAll(template, "{token}", apiKey)
}

func parseThreatFoxResponse(data []byte, collector entry_collector.Collector, source string) error {
	if isZip(data) {
		log.Info().Int("bytes", len(data)).Msg("detected zip archive — extracting full.json")
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
		log.Info().Int("json_bytes", len(jsonData)).Msg("extracted full.json from zip")
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

func iocToEntry(ioc *threatFoxIOC, source string) (*entries.Entry, error) {
	entry := entries.NewEntry().
		WithSource(source).
		WithCategory(ioc.ThreatType)

	switch ioc.IOCType {
	case "ip:port":
		host, _, err := net.SplitHostPort(strings.TrimSpace(ioc.IOCValue))
		if err != nil {
			log.Debug().Err(err).Str("value", ioc.IOCValue).Msg("failed to split ip:port")
			return nil, nil
		}
		if net.ParseIP(host) == nil {
			log.Debug().Str("host", host).Msg("not a valid IP after split")
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
		return nil, nil
	}

	return entry, nil
}
