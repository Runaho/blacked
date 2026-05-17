package sources

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/internal/config"
	"encoding/json"
	"fmt"
	"io"

	"github.com/gocolly/colly/v2"
	"github.com/rs/zerolog/log"
)

// NewPhishTankSource creates the PhishTank online-valid.json source.
// Uses CollyFetcher (the JSON endpoint is a direct download but Colly still works).
func NewPhishTankSource(settings *config.CollectorConfig, collyClient *colly.Collector) *Source {
	const (
		providerID = "phishtank"
		sourceID   = "phishtank-online-valid"
		sourceName = "PhishTank Online Valid"
		sourceURL  = "https://data.phishtank.com/data/online-valid.json"
		category   = "phishing"
	)

	s := &Source{
		ID:         sourceID,
		ProviderID: providerID,
		Name:       sourceName,
		SourceURL:  sourceURL,
		SourceType: SourceTypeJSON,
		Category:   category,
		Enabled:    true,
		BloomTypes: []BloomType{BloomHost, BloomHostPath, BloomPath, BloomFile},
	}

	s.Fetcher = NewCollyFetcher(collyClient)
	s.Parser = NewPhishTankJSONParser(settings.ParserWorkers, settings.ParserBatchSize)

	return s
}

// PhishTankJSONParser handles PhishTank's JSON array format.
type PhishTankJSONParser struct {
	workers   int
	batchSize int
}

// NewPhishTankJSONParser creates a new PhishTank JSON parser.
func NewPhishTankJSONParser(workers, batchSize int) *PhishTankJSONParser {
	return &PhishTankJSONParser{
		workers:   workers,
		batchSize: batchSize,
	}
}

type phishTankRecord struct {
	URL      string `json:"url"`
	PhishID  string `json:"phish_id"`
	Verified string `json:"verified"`
	Online   string `json:"online"`
}

// Parse processes PhishTank's JSON array format.
// The endpoint returns a JSON array of objects, not NDJSON.
func (p *PhishTankJSONParser) Parse(data io.Reader, collector entry_collector.Collector, sourceID, processID string) error {
	// online-valid.json returns a JSON array. Decode directly.
	var records []phishTankRecord
	if err := json.NewDecoder(data).Decode(&records); err != nil {
		return fmt.Errorf("phishTank decode: %w", err)
	}

	// Process in parallel via line-compatible adapter.
	// Convert to a pipe that ParseLinesParallel can consume.
	pr, pw := io.Pipe()
	go func() {
		enc := json.NewEncoder(pw)
		for _, rec := range records {
			if err := enc.Encode(rec); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		pw.Close()
	}()

	return ParseLinesParallel(pr, collector, sourceID, processID, p.workers, p.batchSize, func(line, sid, pid string) (*entries.Entry, error) {
		return parsePhishTankLine(line, sid, pid)
	})
}

func parsePhishTankLine(line, sourceID, processID string) (*entries.Entry, error) {
	if len(line) == 0 || line[0] != '{' {
		return nil, nil
	}

	var rec phishTankRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		log.Debug().Err(err).Str("source", sourceID).Str("line", truncate(line)).Msg("Skipping invalid JSON line")
		return nil, nil
	}

	if rec.URL == "" {
		return nil, nil
	}

	entry := entries.NewEntry().
		WithSource(sourceID).
		WithProcessID(processID)

	if err := entry.SetURL(rec.URL); err != nil {
		log.Debug().Err(err).Str("url", rec.URL).Str("source", sourceID).Msg("Skipping invalid URL")
		return nil, nil
	}

	return entry, nil
}

func truncate(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
