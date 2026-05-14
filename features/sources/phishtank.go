package sources

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"blacked/internal/config"
	"compress/bzip2"
	"encoding/json"
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

// Parse processes PhishTank NDJSON (one JSON object per line) OR bz2.
func (p *PhishTankJSONParser) Parse(data io.Reader, collector entry_collector.Collector, sourceID, processID string) error {
	// The endpoint returns JSON (or bz2). We try bz2 first, then plain JSON.
	// For simplicity, PhishTank's online-valid.json.bz2 is bz2-compressed.
	bz := bzip2.NewReader(data)
	return ParseLinesParallel(bz, collector, sourceID, processID, p.workers, p.batchSize, func(line, sid, pid string) (*entries.Entry, error) {
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
		WithProcessID(processID).
		WithCategory("phishing")

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
