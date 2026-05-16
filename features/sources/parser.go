package sources

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"io"
	"sync"

	"github.com/rs/zerolog/log"
)

// Parser defines the contract for converting raw data into entries.
type Parser interface {
	Parse(data io.Reader, collector entry_collector.Collector, sourceID, processID string) error
}

// LineParserFunc processes a single line and returns an entry.
// Returning nil, nil skips the line (e.g. comments, invalid data).
type LineParserFunc func(line, sourceID, processID string) (*entries.Entry, error)

// FlatListParser parses flat list (one URL per line) sources.
type FlatListParser struct {
	ParserWorkers int
	BatchSize     int
}

// NewFlatListParser creates a parser for flat list sources.
func NewFlatListParser(workers, batchSize int) *FlatListParser {
	return &FlatListParser{
		ParserWorkers: workers,
		BatchSize:     batchSize,
	}
}

// Parse reads lines and delegates to line workers for parallel processing.
func (p *FlatListParser) Parse(data io.Reader, collector entry_collector.Collector, sourceID, processID string) error {
	return ParseLinesParallel(data, collector, sourceID, processID, p.ParserWorkers, p.BatchSize, p.lineProcessor)
}

func (p *FlatListParser) lineProcessor(line, sourceID, processID string) (*entries.Entry, error) {
	return ParseURLLine(line, sourceID, processID)
}

// JSONParserFunc parses JSON data into entries.
type JSONParserFunc func(data io.Reader, collector entry_collector.Collector, sourceID, processID string) error

// AdaptJSONParser wraps a function into the Parser interface.
func AdaptJSONParser(fn JSONParserFunc) Parser {
	return &jsonParserAdapter{fn: fn}
}

type jsonParserAdapter struct {
	fn JSONParserFunc
}

func (j *jsonParserAdapter) Parse(data io.Reader, collector entry_collector.Collector, sourceID, processID string) error {
	return j.fn(data, collector, sourceID, processID)
}

// ParseURLLine is a shared utility that creates an Entry from a raw URL string.
// Returns nil, nil for lines that should be skipped.
func ParseURLLine(line, sourceID, processID string) (*entries.Entry, error) {
	if line == "" || line[0] == '#' {
		return nil, nil
	}

	entry := entries.NewEntry().
		WithSource(sourceID).
		WithProcessID(processID)

	if err := entry.SetURL(line); err != nil {
		log.Debug().Err(err).Str("url", line).Str("source", sourceID).Msg("Skipping invalid URL")
		return nil, nil
	}

	return entry, nil
}

// ParseLinesParallel processes lines in parallel using a worker pool.
// This is the core parallel parsing implementation extracted from base/parallel_parser.go
// so that sources don't depend on the old providers/base package.
func ParseLinesParallel(
	data io.Reader,
	collector entry_collector.Collector,
	sourceID string,
	processID string,
	numWorkers int,
	batchSize int,
	processor LineParserFunc,
) error {
	if numWorkers <= 0 {
		numWorkers = 4
	}
	if batchSize <= 0 {
		batchSize = 1000
	}

	log.Debug().
		Str("source", sourceID).
		Str("process_id", processID).
		Int("workers", numWorkers).
		Int("batch_size", batchSize).
		Msg("Starting parallel line parsing")

	lineCh := make(chan string, numWorkers*2)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Go(func() {
			for line := range lineCh {
				entry, err := processor(line, sourceID, processID)
				if err != nil {
					log.Debug().Err(err).Str("source", sourceID).Msg("Line processor error")
					continue
				}
				if entry != nil {
					collector.Submit(entry)
				}
			}
		})
	}

	// Feed lines
	if err := feedLines(data, lineCh); err != nil {
		close(lineCh)
		wg.Wait()
		return err
	}

	close(lineCh)
	wg.Wait()

	log.Info().
		Str("source", sourceID).
		Int("workers", numWorkers).
		Msg("Parallel parsing completed")

	return nil
}
