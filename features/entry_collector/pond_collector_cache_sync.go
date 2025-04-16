package entry_collector

import (
	"blacked/features/cache"
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/config"
	"blacked/internal/db"
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// ScheduleCacheSync schedules a cache sync operation
// Returns true if the operation was scheduled, false if dropped
// immediate=true will attempt to perform the sync immediately (blocking)
// - Only one sync can run at a time.
// - Only one queued sync is allowed.
// - Further requests are dropped and should be logged.
func (c *PondCollector) ScheduleCacheSync(immediate bool) bool {
	c.cacheSyncMutex.Lock()
	defer c.cacheSyncMutex.Unlock()

	switch c.cacheSyncState {
	case CacheSyncStateIdle:
		// No sync running, start one immediately
		c.cacheSyncState = CacheSyncStateRunning
		c.cacheSyncWaitGroup.Add(1)

		go func() {
			defer c.cacheSyncWaitGroup.Done()
			defer c.completeCacheSync()

			log.Info().Msg("Starting cache synchronization")
			startTime := time.Now()

			ctx := context.Background()
			if err := syncToCache(ctx); err != nil {
				log.Error().Err(err).Msg("Cache sync failed")
			} else {
				duration := time.Since(startTime)
				log.Info().
					Dur("duration", duration).
					Msg("Cache sync completed successfully")
			}
		}()

		// If immediate is true, wait for completion
		if immediate {
			c.cacheSyncMutex.Unlock() // Unlock before waiting
			c.cacheSyncWaitGroup.Wait()
			c.cacheSyncMutex.Lock() // Lock again before returning
		}

		return true

	case CacheSyncStateRunning:
		if immediate {
			// If immediate is requested but another sync is running
			// we return false to indicate we couldn't fulfill the immediate request
			log.Warn().Msg("Immediate cache sync requested but another sync is already running")
			return false
		}

		// A sync is already running, check if we can queue
		if c.cacheSyncState != CacheSyncStateQueued {
			// No sync is queued yet, queue this one
			c.cacheSyncState = CacheSyncStateQueued

			// Start a goroutine that will wait for the current sync to finish
			go func() {
				// Wait for current sync to finish
				c.cacheSyncWaitGroup.Wait()

				// Start a new sync
				c.cacheSyncMutex.Lock()
				if c.cacheSyncState != CacheSyncStateQueued {
					// State changed while we were waiting
					c.cacheSyncMutex.Unlock()
					return
				}

				c.cacheSyncState = CacheSyncStateRunning
				c.cacheSyncWaitGroup.Add(1)
				c.cacheSyncMutex.Unlock()

				defer c.cacheSyncWaitGroup.Done()
				defer c.completeCacheSync()

				log.Info().Msg("Starting queued cache synchronization")
				startTime := time.Now()

				ctx := context.Background()
				if err := syncToCache(ctx); err != nil {
					log.Error().Err(err).Msg("Queued cache sync failed")
				} else {
					duration := time.Since(startTime)
					log.Info().
						Dur("duration", duration).
						Msg("Queued cache sync completed successfully")
				}
			}()

			return true
		}

		// A sync is running and another is already queued, drop this request
		log.Debug().Msg("Cache sync request dropped - queue full")
		return false
	}

	return false
}

// completeCacheSync updates the cache sync state when a sync completes
func (c *PondCollector) completeCacheSync() {
	c.cacheSyncMutex.Lock()
	defer c.cacheSyncMutex.Unlock()

	// If no sync is queued, set state to idle
	// If a sync is queued, the queued sync itself will update the state
	if c.cacheSyncState == CacheSyncStateRunning {
		c.cacheSyncState = CacheSyncStateIdle
	}
}

// WaitForCacheSyncCompletion waits for all cache sync operations to complete
func (c *PondCollector) WaitForCacheSyncCompletion() {
	c.cacheSyncWaitGroup.Wait()
}

func syncToCache(ctx context.Context) error {
	cacheProvider, err := cache.GetCacheProvider()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get cache provider")
		return err
	}

	_db, err := db.GetDB()
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to database")
		return err
	}

	repo := repository.NewSQLiteRepository(_db)

	count := 0
	ch := make(chan entries.EntryStream)

	log.Debug().Msg("Starting to stream entries from repository")

	go func() {
		err := repo.StreamEntries(ctx, ch)
		if err != nil {
			log.Error().Err(err).Msg("Error while streaming entries")
		}
		log.Debug().Msg("Finished streaming entries")
	}()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Int("processed_count", count).Msg("Sync interrupted by context cancellation")
			return ctx.Err()
		case entry, ok := <-ch:
			if !ok {
				log.Debug().Int("processed_count", count).Msg("Finished syncing blacklists to cache")

				if err := cacheProvider.Commit(); err != nil {
					log.Error().Err(err).Msg("Failed to commit cache changes")
					return err
				}
				log.Debug().Msg("Cache changes committed")

				if config.GetConfig().Cache.UseBloom {
					if err := cache.BuildBloomFilterFromCacheProvider(ctx, cacheProvider, count); err != nil {
						log.Error().Err(err).Msg("Failed to build bloom filter")
						return err
					}
				}

				log.Debug().Msg("Bloom filter built successfully")

				return nil
			}

			count++
			if count%100 == 0 {
				log.Trace().Int("processed_count", count).Msg("Processing blacklist entries")
			}

			err = cacheProvider.Set(entry.SourceUrl, entry.IDsRaw)
			if err != nil {
				log.Error().Err(err).Str("key", entry.SourceUrl).Msg("Failed to set entry in cache")
				return err
			}
		}
	}
}
