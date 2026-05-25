package entry_collector

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"blacked/features/bloom"
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/collector"
	"blacked/internal/config"

	"github.com/alitto/pond/v2"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ErrMissingBloomStreamCh is returned when the bloom initialization stream channel is nil.
var ErrMissingBloomStreamCh = errors.New("bloom stream channel is nil")

const (
	PeriodicFlushInterval     = 5 * time.Second
	MaxProvidersInMemory      = 1000
	InactiveProviderThreshold = 1 * time.Hour // Cleanup providers inactive for more than 1 hour
)

var (
	// Global singleton instance
	globalCollector *PondCollector
	once            sync.Once
	batchSlicePool  = sync.Pool{
		New: func() any {
			return make([]*entries.Entry, 0, config.GetConfig().Collector.BatchSize)
		},
	}
)

type PondCollector struct {
	pool          pond.Pool
	repo          repository.BlacklistRepository
	bloomMgr      *bloom.BloomManager
	batchSize     int
	buffer        []*entries.Entry
	bufferMu      sync.Mutex
	providerStats map[string]*ProviderStats
	statsMu       sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc

	// Cache sync state management
	cacheSyncState     CacheSyncState
	cacheSyncMutex     sync.Mutex
	cacheSyncWaitGroup sync.WaitGroup

	// Single-threaded database writer
	dbWriteChan chan []*entries.Entry
	dbWriteWg   sync.WaitGroup
}

// InitPondCollector initializes the global pond collector
func InitPondCollector(
	ctx context.Context,
	db *sql.DB,
) *PondCollector {
	once.Do(func() {
		// Create a child context that we can cancel
		ctxWithCancel, cancel := context.WithCancel(ctx)

		collectorConfig := config.GetConfig().Collector

		// Create a new pond with specified concurrency for processing work
		// This pool is for non-DB operations (parsing, validation, etc.)
		pool := pond.NewPool(collectorConfig.Concurrency)

		// Initialize bloom manager for new entries table
		bloomMgr := bloom.NewBloomManager(1_000_000)

		globalCollector = &PondCollector{
			pool:           pool,
			repo:           repository.NewSQLiteRepository(db),
			bloomMgr:       bloomMgr,
			batchSize:      collectorConfig.BatchSize,
			buffer:         make([]*entries.Entry, 0, collectorConfig.BatchSize),
			providerStats:  make(map[string]*ProviderStats),
			ctx:            ctxWithCancel,
			cancel:         cancel,
			cacheSyncState: CacheSyncStateIdle,
			dbWriteChan:    make(chan []*entries.Entry, 100), // Buffered channel for batches
		}

		// Start a single goroutine for ALL database writes (single-threaded writer)
		globalCollector.dbWriteWg.Add(1)
		go globalCollector.singleThreadedDBWriter()

		// Start a goroutine to flush buffer periodically or on context done
		go globalCollector.periodicFlush()

		// Bootstrap bloom manager from existing DB entries on startup.
		// Without this, a fresh server has an empty bloom filter and every
		// URL returns 204 until the provider pipeline runs.  For 630K entries
		// this takes ~2-3s in background.
		go func() {
			log.Info().Msg("Starting bloom bootstrap from existing database entries")
			start := time.Now()

			// Direct SQL query — faster than StreamEntries which returns EntryStream
			// (source_url + id only, no domain/host/path fields).
			rows, err := db.QueryContext(ctx,
				`SELECT source, domain, host, path, raw_query
				 FROM entries WHERE deleted_at IS NULL`)
			if err != nil {
				log.Error().Err(err).Msg("Bloom bootstrap: query failed")
				return
			}
			defer rows.Close()

			added := 0
			for rows.Next() {
				var source, domain, host, path, rawQuery string
				if err := rows.Scan(&source, &domain, &host, &path, &rawQuery); err != nil {
					log.Error().Err(err).Msg("Bloom bootstrap: scan failed")
					continue
				}
				e := &entries.Entry{
					Source:   source,
					Domain:   domain,
					Host:     host,
					Path:     path,
					RawQuery: rawQuery,
				}
				keys := entryToURLKeys(e)
				globalCollector.bloomMgr.PopulateEntry(e.Source, keys)
				added++
			}

			log.Info().
				Int("entries_loaded", added).
				Dur("duration", time.Since(start)).
				Msg("Bloom bootstrap completed — manager ready for queries")
		}()

		log.Info().
			Int("concurrency", collectorConfig.Concurrency).
			Int("batch_size", collectorConfig.BatchSize).
			Msg("Global pond collector initialized with single-threaded DB writer")
	})
	return globalCollector
}

// GetPondCollector returns the global pond collector instance
func GetPondCollector() *PondCollector {
	return globalCollector
}

// GetBloomManager returns the single *bloom.BloomManager shared across the application.
func (c *PondCollector) GetBloomManager() *bloom.BloomManager {
	return c.bloomMgr
}

// StartProviderProcessing initializes tracking for a provider process
func (c *PondCollector) StartProviderProcessing(providerName, processID string) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	// Emergency cleanup check
	if len(c.providerStats) > MaxProvidersInMemory {
		log.Warn().Int("map_size", len(c.providerStats)).Msg("Provider stats map too large")
		for provider, stats := range c.providerStats {
			if !stats.active {
				delete(c.providerStats, provider)
			}
		}
	}

	c.providerStats[providerName] = &ProviderStats{
		processedCount:   0,
		startTime:        time.Now(),
		processID:        processID,
		active:           true,
		lastActivityTime: time.Now(),
	}

	log.Info().
		Str("provider", providerName).
		Str("processID", processID).
		Msg("Started provider processing")
}

// Submit adds an entry to the collector's buffer
func (c *PondCollector) Submit(entry *entries.Entry) {
	// First, mark that we have a pending operation for this provider
	c.statsMu.RLock()
	stats, exists := c.providerStats[entry.Source]
	if exists && stats.active {
		stats.pendingOperations.Add(1)
	}
	c.statsMu.RUnlock()

	// Now add to buffer as usual
	c.bufferMu.Lock()
	c.buffer = append(c.buffer, entry)

	// If buffer is full, submit a flush task
	if len(c.buffer) >= c.batchSize {
		batch := make([]*entries.Entry, len(c.buffer))
		copy(batch, c.buffer)
		c.buffer = c.buffer[:0]
		c.bufferMu.Unlock()

		c.submitFlush(batch)
	} else {
		c.bufferMu.Unlock()
	}
}
func (c *PondCollector) submitFlush(batch []*entries.Entry) {
	// Simply send the batch to the single-threaded DB writer channel
	// The single writer goroutine will handle all database operations sequentially
	select {
	case c.dbWriteChan <- batch:
		// Batch queued successfully
	case <-c.ctx.Done():
		// Context cancelled, drop the batch
		log.Warn().
			Int("batch_size", len(batch)).
			Msg("Context cancelled, dropping batch")
	}
}

// singleThreadedDBWriter is the ONLY goroutine that writes to the database
// This eliminates all database lock contention issues
func (c *PondCollector) singleThreadedDBWriter() {
	defer c.dbWriteWg.Done()

	log.Info().Msg("Single-threaded database writer started")

	for {
		select {
		case batch := <-c.dbWriteChan:
			// Group entries by source for more efficient processing
			entriesBySource := make(map[string][]*entries.Entry)
			for _, entry := range batch {
				entriesBySource[entry.Source] = append(entriesBySource[entry.Source], entry)
			}

			// Process each source's entries
			for source, sourceEntries := range entriesBySource {
				c.processBatch(source, sourceEntries)
			}

			// Return batch slice to pool
			batch = batch[:0]
			batchSlicePool.Put(batch)

		case <-c.ctx.Done():
			log.Info().Msg("Single-threaded database writer shutting down")
			// Drain remaining batches
			for {
				select {
				case batch := <-c.dbWriteChan:
					entriesBySource := make(map[string][]*entries.Entry)
					for _, entry := range batch {
						entriesBySource[entry.Source] = append(entriesBySource[entry.Source], entry)
					}
					for source, sourceEntries := range entriesBySource {
						c.processBatch(source, sourceEntries)
					}
					batch = batch[:0]
					batchSlicePool.Put(batch)
				default:
					return
				}
			}
		}
	}
}

// processBatch handles the actual database write for a batch of entries
func (c *PondCollector) processBatch(source string, localEntries []*entries.Entry) {
	// Mark pending operations as done
	defer func() {
		c.statsMu.RLock()
		if stats, exists := c.providerStats[source]; exists {
			for range localEntries {
				stats.pendingOperations.Done()
			}
		}
		c.statsMu.RUnlock()
	}()

	// Create span for batch save operation
	tracer := otel.Tracer("blacked/collector")
	_, span := tracer.Start(context.Background(), "collector.batch_save",
		trace.WithAttributes(
			attribute.String("source", source),
			attribute.Int("batch_size", len(localEntries)),
		),
	)
	defer span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.repo.BatchSaveEntries(ctx, localEntries); err != nil {
		span.RecordError(err)
		log.Error().Err(err).
			Int("batch_size", len(localEntries)).
			Str("source", source).
			Msg("Failed to save batch")
		return
	}
	span.AddEvent("batch saved to database")

	// Populate the shared BloomManager so the API can check against live data
	if c.bloomMgr != nil {
		for _, e := range localEntries {
			keys := entryToURLKeys(e)
			c.bloomMgr.PopulateEntry(e.Source, keys)
		}
	}

	batchSize := len(localEntries)

	c.statsMu.RLock()
	if stats, exists := c.providerStats[source]; exists && stats.active {
		stats.processedCount += batchSize
		count := stats.processedCount
		c.statsMu.RUnlock()

		mc, err := collector.GetMetricsCollector()
		if err != nil || mc == nil {
			log.Error().Err(err).Msg("Failed to get metrics collector")
		}

		if mc != nil {
			log.Trace().Str("provider", source).Int("batch_size", batchSize).Msg("Incrementing saved count")
			mc.IncrementSavedCount(source, batchSize)
		}

		if log.Info().Enabled() && count%100000 == 0 {
			log.Info().
				Int("processed_count", count).
				Str("source", source).
				Msg("Processing milestone reached")
		}
	} else {
		c.statsMu.RUnlock()
		log.Debug().
			Int("batch_size", batchSize).
			Str("source", source).
			Msg("Processed batch for inactive provider")
	}
}

func (c *PondCollector) periodicFlush() {
	ticker := time.NewTicker(PeriodicFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.flushBuffer()
			c.cleanupInactiveProviders()
		case <-c.ctx.Done():
			c.flushBuffer()
			return
		}
	}
}

func (c *PondCollector) flushBuffer() {
	c.bufferMu.Lock()
	if len(c.buffer) > 0 {
		batch := batchSlicePool.Get().([]*entries.Entry)
		batch = batch[:0]
		batch = append(batch, c.buffer...)

		c.buffer = c.buffer[:0]
		c.bufferMu.Unlock()

		c.submitFlush(batch)

		batch = batch[:0]
		batchSlicePool.Put(batch)
	} else {
		c.bufferMu.Unlock()
	}
}

// cleanupInactiveProviders removes provider stats entries that have been inactive
// for longer than InactiveProviderThreshold. This prevents memory leaks from
// abandoned provider processes.
func (c *PondCollector) cleanupInactiveProviders() {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	now := time.Now()
	for provider, stats := range c.providerStats {
		if !stats.active && now.Sub(stats.lastActivityTime) > InactiveProviderThreshold {
			delete(c.providerStats, provider)
			log.Info().
				Str("provider", provider).
				Time("last_activity", stats.lastActivityTime).
				Msg("Cleaned up inactive provider stats")
		}
	}
}

// Wait waits for all submitted entries to be processed
func (c *PondCollector) Wait() {
	// Flush any remaining entries in buffer
	c.flushBuffer()

	// Wait for pond tasks to complete (non-DB work)
	c.pool.StopAndWait()

	// Close the DB write channel and wait for the writer to finish
	close(c.dbWriteChan)
	c.dbWriteWg.Wait()
}

// Close cancels the context and waits for all tasks to complete
func (c *PondCollector) Close() {
	// Cancel context to stop periodic flushing
	c.cancel()

	// Flush any remaining entries & stop
	c.Wait()
}

// GetProcessedCount returns the number of processed entries for a provider
func (c *PondCollector) GetProcessedCount(source string) int {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()

	if stats, exists := c.providerStats[source]; exists {
		return stats.processedCount
	}
	return 0
}

// FinishProviderProcessing logs stats and finalizes metrics for a provider
func (c *PondCollector) FinishProviderProcessing(providerName, processID string) (count int, duration time.Duration, ok bool) {
	// Lock to get the stats and check processID
	c.statsMu.Lock()
	stats, exists := c.providerStats[providerName]
	if !exists || stats.processID != processID {
		c.statsMu.Unlock()
		log.Warn().
			Str("provider", providerName).
			Str("processID", processID).
			Msg("Attempted to finish provider processing but stats not found")
		return 0, 0, false
	}
	c.statsMu.Unlock()

	// Wait for all pending operations to complete
	stats.pendingOperations.Wait()

	// Now it's safe to mark as inactive and set timestamp for cleanup
	c.statsMu.Lock()
	stats.active = false
	stats.lastActivityTime = time.Now() // Set timestamp for cleanup tracking
	count = stats.processedCount
	duration = time.Since(stats.startTime)
	c.statsMu.Unlock()

	if mc, _ := collector.GetMetricsCollector(); mc != nil {
		mc.SetTotalProcessed(providerName, count)
	}

	// Log final stats
	entriesPerSec := float64(count) / duration.Seconds()
	log.Info().
		Str("provider", providerName).
		Int("entries", count).
		Dur("duration", duration).
		Str("processID", processID).
		Float64("entries_per_second", entriesPerSec).
		Msg("Provider processing completed")

	return count, duration, true
}

// GetStatsMapSize returns the current size of the provider stats map
// Useful for monitoring
func (c *PondCollector) GetStatsMapSize() int {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	return len(c.providerStats)
}

// entryToURLKeys converts an entries.Entry into bloom.URLKeys without re-parsing the URL.
// The entry already has Domain, Host, Path, and RawQuery stored from the original provider parse.
// This saves ~310MB of ParseURL alloc during provider sync.
func entryToURLKeys(e *entries.Entry) *bloom.URLKeys {
	file := ""
	if e.Path != "" && e.Path != "/" {
		base := e.Path
		if idx := strings.LastIndex(e.Path, "/"); idx >= 0 {
			base = e.Path[idx+1:]
		}
		if extIdx := strings.LastIndex(base, "."); extIdx > 0 && extIdx < len(base)-1 {
			file = base
		}
	}

	hp := ""
	if e.Host != "" && e.Path != "" && e.Path != "/" {
		hp = e.Host + e.Path
	}

	ip := ""
	if trimmed := strings.TrimSpace(e.Host); trimmed != "" {
		if net.ParseIP(trimmed) != nil {
			ip = trimmed
		}
	}

	return &bloom.URLKeys{
		Domain:   e.Domain,
		Host:     e.Host,
		HostPath: hp,
		Path:     e.Path,
		File:     file,
		Query:    e.RawQuery,
		IP:       ip,
	}
}
