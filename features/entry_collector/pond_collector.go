package entry_collector

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
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
	PeriodicFlushInterval = 5 * time.Second
	MaxProvidersInMemory  = 1000
	
	// DB Write Channel capacity
	DBWriteChannelSize = 100
	
	// Backpressure thresholds (as fraction of channel capacity)
	BackpressureHighThreshold = 0.8  // 80% - signal backpressure
	BackpressureLowThreshold  = 0.5  // 50% - signal normal operation
)

// Channel state for backpressure signaling
const (
	ChannelStateNormal = iota
	ChannelStateBackpressure
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

	// Backpressure management
	channelState         int32 // atomic: ChannelStateNormal or ChannelStateBackpressure
	queueHighWatermark   int32 // atomic: peak queue depth since startup
	backpressureNotifyCh chan struct{} // closed when backpressure clears
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
			pool:                pool,
			repo:                repository.NewSQLiteRepository(db),
			bloomMgr:            bloomMgr,
			batchSize:           collectorConfig.BatchSize,
			buffer:              make([]*entries.Entry, 0, collectorConfig.BatchSize),
			providerStats:       make(map[string]*ProviderStats),
			ctx:                 ctxWithCancel,
			cancel:              cancel,
			cacheSyncState:      CacheSyncStateIdle,
			dbWriteChan:         make(chan []*entries.Entry, DBWriteChannelSize),
			channelState:        ChannelStateNormal,
			queueHighWatermark:  0,
			backpressureNotifyCh: make(chan struct{}),
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
		processedCount: 0,
		startTime:      time.Now(),
		processID:      processID,
		active:         true,
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
	// Check current queue depth and update metrics
	queueDepth := len(c.dbWriteChan)
	
	// Update metrics
	if mc, err := collector.GetMetricsCollector(); err == nil && mc != nil {
		mc.UpdateQueueDepth(queueDepth)
		
		// Track high watermark
		if int32(queueDepth) > atomic.LoadInt32(&c.queueHighWatermark) {
			atomic.StoreInt32(&c.queueHighWatermark, int32(queueDepth))
			mc.UpdateQueueHighWatermark(queueDepth)
		}
	}
	
	// Check for backpressure threshold
	highThreshold := int(float64(DBWriteChannelSize) * BackpressureHighThreshold)
	lowThreshold := int(float64(DBWriteChannelSize) * BackpressureLowThreshold)
	
	if queueDepth >= highThreshold {
		// Transition to backpressure state
		if atomic.CompareAndSwapInt32(&c.channelState, ChannelStateNormal, ChannelStateBackpressure) {
			log.Warn().
				Int("queue_depth", queueDepth).
				Int("capacity", DBWriteChannelSize).
				Float64("threshold_pct", BackpressureHighThreshold*100).
				Msg("DB write channel approaching saturation - backpressure activated")
			
			// Record backpressure event
			if mc, err := collector.GetMetricsCollector(); err == nil && mc != nil {
				mc.RecordBackpressureEvent()
			}
		}
	}
	
	// Try to send the batch with timeout for graceful handling
	select {
	case c.dbWriteChan <- batch:
		// Batch queued successfully - check if we can clear backpressure
		newDepth := len(c.dbWriteChan)
		if newDepth < lowThreshold {
			// Signal that backpressure has cleared
			if atomic.CompareAndSwapInt32(&c.channelState, ChannelStateBackpressure, ChannelStateNormal) {
				log.Info().
					Int("queue_depth", newDepth).
					Msg("DB write channel recovered - backpressure cleared")
				
				// Close and recreate the notification channel
				select {
				case <-c.backpressureNotifyCh:
					// Already closed, create new one
				default:
					// Not closed yet, close it now
					close(c.backpressureNotifyCh)
				}
				c.backpressureNotifyCh = make(chan struct{})
			}
		}
	case <-c.ctx.Done():
		// Context cancelled, drop the batch
		log.Warn().
			Int("batch_size", len(batch)).
			Msg("Context cancelled, dropping batch")
		if mc, err := collector.GetMetricsCollector(); err == nil && mc != nil {
			mc.RecordDroppedBatch(len(batch))
		}
	default:
		// Channel is full - this shouldn't happen often with backpressure
		// but handle it gracefully
		log.Error().
			Int("batch_size", len(batch)).
			Int("queue_depth", queueDepth).
			Int("capacity", DBWriteChannelSize).
			Msg("DB write channel full - dropping batch")
		
		// Record dropped batch
		if mc, err := collector.GetMetricsCollector(); err == nil && mc != nil {
			mc.RecordDroppedBatch(len(batch))
		}
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
		case <-c.ctx.Done():
			c.flushBuffer()
			return
		}
	}
}

func (c *PondCollector) flushBuffer() {
	c.bufferMu.Lock()
	if len(c.buffer) > 0 {
		// Copy entries to a new slice allocated from the pool.
		// The pooled slice is used for the copy destination; once submitFlush
		// takes ownership we must not retain a reference to it (sync.Pool
		// backing arrays are re-used and would race with the DB writer).
		batch := batchSlicePool.Get().([]*entries.Entry)
		batch = batch[:0]
		batch = append(batch, c.buffer...)

		c.buffer = c.buffer[:0]
		c.bufferMu.Unlock()

		// submitFlush takes ownership; the batch slice we handed off must not
		// be used again.  The DB writer will return the slice it received
		// (after zeroing its length) back to the pool, so we do NOT put the
		// original slice back here — that would be a double-put and would also
		// create a window where the pool hands the same backing array to
		// another caller while the writer is still writing through it.
		c.submitFlush(batch)
	} else {
		c.bufferMu.Unlock()
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

	// Now it's safe to mark as inactive and delete
	c.statsMu.Lock()
	stats.active = false
	count = stats.processedCount
	duration = time.Since(stats.startTime)

	delete(c.providerStats, providerName)
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

// GetQueueDepth returns the current number of batches waiting in the DB write queue
func (c *PondCollector) GetQueueDepth() int {
	return len(c.dbWriteChan)
}

// GetQueueHighWatermark returns the peak queue depth since startup
func (c *PondCollector) GetQueueHighWatermark() int {
	return int(atomic.LoadInt32(&c.queueHighWatermark))
}

// IsBackpressureActive returns true if the DB write queue is in backpressure state
func (c *PondCollector) IsBackpressureActive() bool {
	return atomic.LoadInt32(&c.channelState) == ChannelStateBackpressure
}

// GetBackpressureNotifyCh returns a channel that is closed when backpressure clears
// Callers can use this to wait for the queue to drain during backpressure
func (c *PondCollector) GetBackpressureNotifyCh() <-chan struct{} {
	return c.backpressureNotifyCh
}

// WaitForQueueSpace waits until the queue has space or the context is cancelled.
// This is useful for providers generating large volumes of entries.
// Returns true if space is available, false if context was cancelled.
func (c *PondCollector) WaitForQueueSpace(ctx context.Context) bool {
	// Check if already below threshold
	if len(c.dbWriteChan) < int(float64(DBWriteChannelSize)*BackpressureHighThreshold) {
		return true
	}
	
	// Wait for backpressure to clear
	select {
	case <-c.backpressureNotifyCh:
		// Backpressure channel was closed - we may need to recreate
		// Actually, this means backpressure WAS active and is now cleared
		return true
	case <-ctx.Done():
		return false
	}
}

// RemoveStaleEntriesAndSyncBloom removes old entries for a provider and rebuilds the bloom filter.
// This is called after a provider finishes processing to soft-delete stale entries
// that were not present in the latest provider run.
func (c *PondCollector) RemoveStaleEntriesAndSyncBloom(ctx context.Context, providerName, processID string) error {
	log.Info().
		Str("provider", providerName).
		Str("process_id", processID).
		Msg("Starting stale entry removal and bloom sync")

	// First, wait for all pending operations for this provider
	c.statsMu.RLock()
	stats, exists := c.providerStats[providerName]
	c.statsMu.RUnlock()

	if exists && stats != nil {
		stats.pendingOperations.Wait()
	}

	// Soft delete entries from previous runs
	if err := c.repo.RemoveOlderInsertions(ctx, providerName, processID); err != nil {
		log.Error().Err(err).
			Str("provider", providerName).
			Str("process_id", processID).
			Msg("Failed to remove older insertions")
		return err
	}

	log.Info().
		Str("provider", providerName).
		Str("process_id", processID).
		Msg("Stale entries removed, rebuilding bloom filter")

	// Rebuild the bloom filter for this source
	if c.bloomMgr != nil {
		if err := c.bloomMgr.RebuildSource(ctx, providerName, &repositoryEntryStream{repo: c.repo, source: providerName}, GetAllBloomTypes()); err != nil {
			log.Error().Err(err).
				Str("provider", providerName).
				Msg("Failed to rebuild bloom filter after stale entry removal")
			return err
		}
	}

	log.Info().
		Str("provider", providerName).
		Str("process_id", processID).
		Msg("Bloom filter rebuild completed")

	return nil
}

// repositoryEntryStream implements bloom.SourceEntryStream using the repository
type repositoryEntryStream struct {
	repo   repository.BlacklistRepository
	source string
}

func (r *repositoryEntryStream) StreamEntriesBySource(ctx context.Context, sourceID string) ([]bloom.Entry, error) {
	dbEntries, err := r.repo.GetEntriesBySource(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	entries := make([]bloom.Entry, 0, len(dbEntries))
	for _, e := range dbEntries {
		entries = append(entries, bloom.Entry{
			SourceID: e.Source,
			Domain:   e.Domain,
			Host:     e.Host,
			Path:     e.Path,
			File:     extractFileFromPath(e.Path),
			Query:    e.RawQuery,
			Login:    "",
			IP:       extractIPFromHost(e.Scheme, e.Host),
		})
	}
	return entries, nil
}

// GetAllBloomTypes returns all bloom types that need to be rebuilt
func GetAllBloomTypes() []bloom.BloomType {
	return []bloom.BloomType{
		bloom.BloomDomain, bloom.BloomHost, bloom.BloomHostPath,
		bloom.BloomFile, bloom.BloomFullURL, bloom.BloomLogin, bloom.BloomIP,
	}
}

// extractFileFromPath extracts the filename from a path if it has an extension
func extractFileFromPath(path string) string {
	if path == "" || path == "/" {
		return ""
	}
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}
	if extIdx := strings.LastIndex(base, "."); extIdx > 0 && extIdx < len(base)-1 {
		return base
	}
	return ""
}

// extractIPFromHost extracts IP address from host if applicable
func extractIPFromHost(scheme, host string) string {
	if scheme != "" {
		return "" // Not a raw IP
	}
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}
	// Check if host part is valid IP (with optional port)
	if h, _, err := net.SplitHostPort(trimmed); err == nil {
		if net.ParseIP(h) != nil {
			return trimmed
		}
	} else {
		if net.ParseIP(trimmed) != nil {
			return trimmed
		}
	}
	return ""
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
	// Only put pure IPs (no scheme) into IP bloom
	// Scheme-prefixed URLs (http://185.234.72.15, ftp://10.0.0.1) go to full_url bloom, not IP bloom
	// Port is preserved in bloom key: "1.2.3.4:8080" stays as-is
	if e.Scheme == "" {
		trimmed := strings.TrimSpace(e.Host)
		if trimmed == "" {
			return &bloom.URLKeys{
				Domain:   e.Domain,
				Host:     e.Host,
				HostPath: hp,
				Path:     e.Path,
				File:     file,
				Query:    e.RawQuery,
				IP:       "",
			}
		}
		// Check if it looks like IP (with optional port)
		if h, _, err := net.SplitHostPort(trimmed); err != nil {
			// No port — check if pure IP
			if net.ParseIP(trimmed) != nil {
				ip = trimmed
			}
		} else {
			// Has port — verify host part is valid IP, keep port in bloom key
			if net.ParseIP(h) != nil {
				ip = trimmed // "1.2.3.4:8080" preserved
			}
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
