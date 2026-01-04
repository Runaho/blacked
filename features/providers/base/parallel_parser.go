package base

import (
	"blacked/features/entries"
	"blacked/features/entry_collector"
	"bufio"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/trace"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var (
	execTraceMu     sync.Mutex
	execTraceActive bool
)

func shouldStartExecTrace(providerName string) bool {
	if os.Getenv("BLACKEDEXECTRACE") != "1" {
		return false
	}
	if wantedProvider := os.Getenv("BLACKEDEXECTRACE_PROVIDER"); wantedProvider != "" && wantedProvider != providerName {
		return false
	}
	return true
}

func maybeStartExecTrace(providerName, processID string) (stop func()) {
	if !shouldStartExecTrace(providerName) {
		return func() {}
	}

	execTraceMu.Lock()
	if execTraceActive {
		execTraceMu.Unlock()
		return func() {}
	}
	execTraceActive = true
	execTraceMu.Unlock()

	startedAt := time.Now()
	traceDir := filepath.Join(".", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		log.Warn().Err(err).Msg("Failed to create traces directory; skipping exec trace")
		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()
		return func() {}
	}

	fileName := filepath.Join(traceDir, "parallel-parse-"+providerName+"-"+processID+"-"+startedAt.UTC().Format("20060102T150405Z")+".out")
	traceFile, err := os.Create(fileName)
	if err != nil {
		log.Warn().Err(err).Str("file", fileName).Msg("Failed to create exec trace file; skipping exec trace")
		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()
		return func() {}
	}

	if err := trace.Start(traceFile); err != nil {
		_ = traceFile.Close()
		log.Warn().Err(err).Str("file", fileName).Msg("Failed to start exec trace")
		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()
		return func() {}
	}

	log.Info().
		Str("provider", providerName).
		Str("process_id", processID).
		Str("file", fileName).
		Msg("Go exec trace started for parallel parsing")

	return func() {
		trace.Stop()
		_ = traceFile.Close()

		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()

		log.Info().
			Str("provider", providerName).
			Str("process_id", processID).
			Dur("duration", time.Since(startedAt)).
			Str("file", fileName).
			Msg("Go exec trace stopped")
	}
}

// LineProcessor is a function that processes a single line and returns an entry
// Returns nil entry to skip the line (e.g., for comments or invalid data)
type LineProcessor func(line string, processID string) (*entries.Entry, error)

// ParseLinesParallel reads lines from a reader and processes them in parallel
// This is optimized for large files with millions of lines
func ParseLinesParallel(
	data io.Reader,
	collector entry_collector.Collector,
	providerName string,
	numWorkers int,
	batchSize int,
	processor LineProcessor,
) error {
	// Auto-detect optimal worker count if not specified
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	// Default batch size
	if batchSize <= 0 {
		batchSize = 1000
	}

	processID := uuid.New().String()
	stopExecTrace := maybeStartExecTrace(providerName, processID)
	defer stopExecTrace()

	log.Debug().
		Str("provider", providerName).
		Int("workers", numWorkers).
		Int("batch_size", batchSize).
		Msg("Starting parallel line parsing")

	// Channel for batches of lines
	lineBatches := make(chan []string, numWorkers*2)

	// Error channel
	errChan := make(chan error, 1)

	// WaitGroup for workers
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for batch := range lineBatches {
				for _, line := range batch {
					entry, err := processor(line, processID)
					if err != nil {
						// Send error but continue processing
						select {
						case errChan <- err:
						default:
							// Error channel full, log instead
							log.Error().Err(err).Str("provider", providerName).Msg("Error processing line")
						}
						continue
					}

					// Skip nil entries (filtered out by processor)
					if entry != nil {
						collector.Submit(entry)
					}
				}
			}
		}(i)
	}

	// Reader goroutine - reads lines and batches them
	go func() {
		defer close(lineBatches)

		scanner := bufio.NewScanner(data)
		// Increase buffer size for large lines
		const maxCapacity = 1024 * 1024 // 1MB
		buf := make([]byte, maxCapacity)
		scanner.Buffer(buf, maxCapacity)

		batch := make([]string, 0, batchSize)
		lineCount := 0

		for scanner.Scan() {
			line := scanner.Text()
			batch = append(batch, line)
			lineCount++

			if len(batch) >= batchSize {
				// Send full batch
				batchCopy := make([]string, len(batch))
				copy(batchCopy, batch)
				lineBatches <- batchCopy
				batch = batch[:0]
			}
		}

		// Send remaining lines
		if len(batch) > 0 {
			lineBatches <- batch
		}

		if err := scanner.Err(); err != nil {
			errChan <- err
		}

		log.Debug().
			Str("provider", providerName).
			Int("lines_read", lineCount).
			Msg("Finished reading lines")
	}()

	// Wait for all workers to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	if err := <-errChan; err != nil {
		log.Err(err).Str("provider", providerName).Msg("Error during parallel parsing")
		return err
	}

	log.Info().
		Str("provider", providerName).
		Int("workers", numWorkers).
		Msg("Parallel parsing completed successfully")

	return nil
}

// EntryProcessor is a generic function that processes a single item and returns an entry
type EntryProcessor[T any] func(item T, processID string) (*entries.Entry, error)

// ProcessEntriesParallel processes a slice of items in parallel
// This is useful for JSON-based providers that already have the data in memory
func ProcessEntriesParallel[T any](
	items []T,
	collector entry_collector.Collector,
	numWorkers int,
	processor EntryProcessor[T],
	processID string,
) error {
	// Auto-detect optimal worker count if not specified
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	if len(items) == 0 {
		return nil
	}

	log.Debug().
		Int("workers", numWorkers).
		Int("items", len(items)).
		Msg("Starting parallel entry processing")

	// Channel for items to process
	itemChan := make(chan T, numWorkers*2)

	// Error channel
	errChan := make(chan error, 1)

	// WaitGroup for workers
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for item := range itemChan {
				entry, err := processor(item, processID)
				if err != nil {
					// Send error but continue processing
					select {
					case errChan <- err:
					default:
						// Error channel full, log instead
						log.Error().Err(err).Msg("Error processing entry")
					}
					continue
				}

				// Skip nil entries (filtered out by processor)
				if entry != nil {
					collector.Submit(entry)
				}
			}
		}(i)
	}

	// Feed items to workers
	go func() {
		defer close(itemChan)
		for _, item := range items {
			itemChan <- item
		}
	}()

	// Wait for all workers to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	if err := <-errChan; err != nil {
		log.Err(err).Msg("Error during parallel entry processing")
		return err
	}

	log.Debug().
		Int("items_processed", len(items)).
		Int("workers", numWorkers).
		Msg("Parallel entry processing completed")

	return nil
}
