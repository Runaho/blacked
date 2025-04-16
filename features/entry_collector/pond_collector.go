package entry_collector

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/collector"
	"blacked/internal/config"
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/rs/zerolog/log"
)

const (
	PeriodicFlushInterval = 5 * time.Second
	MaxProvidersInMemory  = 1000
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

		// Create a new pond with specified concurrency
		pool := pond.NewPool(collectorConfig.Concurrency)

		globalCollector = &PondCollector{
			pool:           pool,
			repo:           repository.NewSQLiteRepository(db),
			batchSize:      collectorConfig.BatchSize,
			buffer:         make([]*entries.Entry, 0, collectorConfig.BatchSize),
			providerStats:  make(map[string]*ProviderStats),
			ctx:            ctxWithCancel,
			cancel:         cancel,
			cacheSyncState: CacheSyncStateIdle,
		}

		// Start a goroutine to flush buffer periodically or on context done
		go globalCollector.periodicFlush()

		log.Info().
			Int("concurrency", collectorConfig.Concurrency).
			Int("batch_size", collectorConfig.BatchSize).
			Msg("Global pond collector initialized")
	})
	return globalCollector
}

// GetPondCollector returns the global pond collector instance
func GetPondCollector() *PondCollector {
	return globalCollector
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
	// Group entries by source for more efficient processing
	entriesBySource := make(map[string][]*entries.Entry)
	for _, entry := range batch {
		entriesBySource[entry.Source] = append(entriesBySource[entry.Source], entry)
	}

	var wg sync.WaitGroup
	for source, sourceEntries := range entriesBySource {
		localEntries := sourceEntries
		wg.Add(1)
		c.pool.Submit(func() {
			defer wg.Done()
			defer func() {
				c.statsMu.RLock()
				if stats, exists := c.providerStats[source]; exists {
					for range localEntries {
						stats.pendingOperations.Done()
					}
				}
				c.statsMu.RUnlock()
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := c.repo.BatchSaveEntries(ctx, localEntries); err != nil {
				log.Error().Err(err).
					Int("batch_size", len(localEntries)).
					Str("source", source).
					Msg("Failed to save batch")
				return
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
		})
	}

	// Wait for all goroutines to finish before returning batch to pool
	wg.Wait()
	batch = batch[:0]
	batchSlicePool.Put(batch)
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

// Wait waits for all submitted entries to be processed
func (c *PondCollector) Wait() {
	// Flush any remaining entries in buffer
	c.flushBuffer()

	// Wait for pond tasks to complete
	c.pool.StopAndWait()
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
func (c *PondCollector) FinishProviderProcessing(providerName, processID string) {
	// Lock to get the stats and check processID
	c.statsMu.Lock()
	stats, exists := c.providerStats[providerName]
	if !exists || stats.processID != processID {
		c.statsMu.Unlock()
		log.Warn().
			Str("provider", providerName).
			Str("processID", processID).
			Msg("Attempted to finish provider processing but stats not found")
		return
	}
	c.statsMu.Unlock()

	// Wait for all pending operations to complete
	stats.pendingOperations.Wait()

	// Now it's safe to mark as inactive and delete
	c.statsMu.Lock()
	stats.active = false
	count := stats.processedCount
	duration := time.Since(stats.startTime)

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
}

// GetStatsMapSize returns the current size of the provider stats map
// Useful for monitoring
func (c *PondCollector) GetStatsMapSize() int {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	return len(c.providerStats)
}
