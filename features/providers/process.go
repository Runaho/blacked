package providers

import (
	"blacked/features/cache"
	"blacked/features/entries/repository"
	"blacked/features/providers/base"
	"blacked/internal/collector"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/utils"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type UpdateCacheMode string

const (
	UpdateCacheImmediate UpdateCacheMode = "immediate"
	UpdateCacheDeferred                  = "deferred"
	UpdateCacheNone                      = "none"
)

type ProcessOptions struct {
	UpdateCacheMode UpdateCacheMode
	TrackMetrics    bool
}

// DefaultProcessOptions provides sensible defaults
var DefaultProcessOptions = ProcessOptions{
	UpdateCacheMode: UpdateCacheDeferred,
	TrackMetrics:    true,
}

// Process processes all providers with the specified options
// This is the central method for all provider processing operations
// and should be the entrypoint for all provider execution.
func (p Providers) Process(ctx context.Context, opts ...ProcessOptions) error {
	options := DefaultProcessOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	log.Info().
		Int("providers", len(p)).
		Str("cache_mode", string(options.UpdateCacheMode)).
		Msg("Processing providers")

	rwDB, err := db.GetDB()
	if err != nil {
		return fmt.Errorf("failed to open read-write database: %w", err)
	}

	repo := repository.NewSQLiteRepository(rwDB)

	var wg sync.WaitGroup
	errChan := make(chan error, len(p))

	// Process providers concurrently, but don't update cache yet
	for _, provider := range p {
		wg.Add(1)
		go p.processProvider(provider, repo, options.TrackMetrics, &wg, errChan)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var aggregatedError error
	for err := range errChan {
		if err != nil {
			aggregatedError = errors.Join(aggregatedError, err)
		}
	}

	// Handle cache updates based on mode
	switch options.UpdateCacheMode {
	case UpdateCacheImmediate:
		log.Info().Msg("Performing immediate cache sync after provider processing")
		if err := cache.SyncBlacklistsToBadger(ctx); err != nil {
			return fmt.Errorf("failed to sync cache after provider processing: %w", err)
		}

	case UpdateCacheDeferred:
		log.Info().Msg("Scheduling deferred cache sync")
		cache.FireAndForgetSync()

	case UpdateCacheNone:
		log.Info().Msg("Skipping cache sync as requested")

	default:
		log.Warn().
			Str("update_mode", string(options.UpdateCacheMode)).
			Msg("Unknown cache update mode, defaulting to deferred")
		cache.FireAndForgetSync()
	}

	if aggregatedError != nil {
		return fmt.Errorf("errors during provider processing: %w", aggregatedError)
	}

	return nil
}

// processProvider updated to support metrics tracking
func (p Providers) processProvider(provider base.Provider, repo repository.BlacklistRepository,
	trackMetrics bool, wg *sync.WaitGroup, errChan chan error) {
	defer wg.Done()

	name := provider.GetName()
	source := provider.Source()
	processID := uuid.New()
	startedAt := time.Now()
	strProcessID := processID.String()

	// Track metrics if enabled
	if trackMetrics {
		mc, err := collector.GetMetricsCollector()
		if err == nil && mc != nil {
			mc.SetSyncRunning(name)
		}
	}

	providerLogger := log.With().
		Str("process_id", strProcessID).
		Str("source", source).
		Str("provider", name).
		Logger()

	providerLogger.Info().Time("starts", startedAt).Msg("Processing provider")

	provider.SetProcessID(processID)

	// Fetch data
	reader, meta, err := utils.GetResponseReader(source, provider.Fetch, name, strProcessID)
	if err != nil {
		providerLogger.
			Err(err).
			Str("source", source).
			Str("provider", name).
			Msg("Error fetching data")

		// Update metrics on failure
		if trackMetrics {
			mc, _ := collector.GetMetricsCollector()
			if mc != nil {
				mc.SetSyncFailed(name, err, time.Since(startedAt))
			}
		}

		errChan <- err
		return
	}

	// Handle metadata if present
	if meta != nil {
		strProcessID = meta.ProcessID
		providerLogger.Info().
			Str("new_process_id", strProcessID).
			Msg("Found metadata, changing process ID")
		provider.SetProcessID(uuid.MustParse(strProcessID))
	}

	// Parse data
	provider.SetRepository(repo)
	if err := provider.Parse(reader); err != nil {
		providerLogger.
			Err(err).
			Str("source", source).
			Str("provider", name).
			Msg("Error parsing data")

		// Update metrics on failure
		if trackMetrics {
			mc, _ := collector.GetMetricsCollector()
			if mc != nil {
				mc.SetSyncFailed(name, err, time.Since(startedAt))
			}
		}

		errChan <- err
		return
	}

	// Cleanup if needed
	cfg := config.GetConfig()
	if cfg.APP.Environtment != "development" {
		utils.RemoveStoredResponse(name)
	}

	// Update metrics on success
	if trackMetrics {
		mc, _ := collector.GetMetricsCollector()
		if mc != nil {
			mc.SetSyncSuccess(name, time.Since(startedAt))
		}
	}

	providerLogger.Info().
		TimeDiff("duration", time.Now(), startedAt).
		Msg("Finished processing provider")
}
