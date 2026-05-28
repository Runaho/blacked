package providers

import (
	"blacked/features/entries/repository"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/collector"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/tracing"
	"blacked/internal/utils"

	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Process error variables
var (
	ErrProcessingProvider   = errors.New("error processing provider")
	ErrProviderNotFound     = errors.New("provider not found")
	ErrCreateRepository     = errors.New("failed to create repository")
	ErrUpdateCache          = errors.New("failed to update cache")
	ErrNoProvidersSpecified = errors.New("no providers specified for processing")
	ErrCollectorNotFound    = errors.New("pond collector not found")
)

type UpdateCacheMode string

const (
	UpdateCacheImmediate UpdateCacheMode = "immediate"
	UpdateCacheDeferred  UpdateCacheMode = "deferred"
	UpdateCacheNone      UpdateCacheMode = "none"
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
	if len(p) == 0 {
		return ErrNoProvidersSpecified
	}

	options := DefaultProcessOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	// Generate a unique process ID for this run
	processID := uuid.New().String()

	// Start execution trace if enabled (captures all providers in one trace file)
	if tracing.ShouldStartExecTrace("providers") {
		stopTrace := tracing.StartExecTrace("providers", processID)
		defer stopTrace()
	}

	// Check if pond collector exists - we expect it to be initialized elsewhere
	pondCollector := entry_collector.GetPondCollector()
	if pondCollector == nil {
		return ErrCollectorNotFound
	}

	log.Info().
		Int("providers", len(p)).
		Str("cache_mode", string(options.UpdateCacheMode)).
		Msg("Processing providers")

	// Get write database connection (used for provider repository)
	rwDB, err := db.GetWriteDB()
	if err != nil {
		log.Err(err).Msg("Failed to open read-write database")
		return ErrCreateRepository
	}

	// Create repository
	repo := repository.NewSQLiteRepository(rwDB)

	var wg sync.WaitGroup
	errChan := make(chan error, len(p))

	// Get max concurrent providers from config (0 = unlimited)
	// NOTE: MaxConcurrentProviders moved to per-provider config; default to all providers concurrently
	maxConcurrent := len(p)
	if maxConcurrent <= 0 {
		maxConcurrent = len(p) // No limit, process all concurrently
	}

	// Create a semaphore to limit concurrent provider processing
	semaphore := make(chan struct{}, maxConcurrent)

	log.Info().
		Int("max_concurrent", maxConcurrent).
		Int("total_providers", len(p)).
		Msg("Starting provider processing with concurrency control")

	// Process providers concurrently with optional limit
	for _, provider := range p {
		wg.Add(1)
		go func(prov base.Provider) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Process the provider
			p.processProvider(ctx, prov, repo, pondCollector, options.TrackMetrics, nil, errChan)
		}(provider)
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

	// Handle cache updates based on mode using the integrated cache sync mechanism
	switch options.UpdateCacheMode {
	case UpdateCacheImmediate:
		log.Info().Msg("Performing immediate cache sync after provider processing")
		success := pondCollector.ScheduleCacheSync(true)
		if !success {
			log.Warn().Msg("Could not perform immediate cache sync - another sync is in progress")
			return ErrUpdateCache
		}

	case UpdateCacheDeferred:
		log.Info().Msg("Scheduling deferred cache sync")
		success := pondCollector.ScheduleCacheSync(false)
		if !success {
			log.Debug().Msg("Deferred cache sync not scheduled - sync queue is full")
		}

	case UpdateCacheNone:
		log.Info().Msg("Skipping cache sync as requested")

	default:
		log.Warn().
			Str("update_mode", string(options.UpdateCacheMode)).
			Msg("Unknown cache update mode, defaulting to deferred")
		pondCollector.ScheduleCacheSync(false)
	}

	if aggregatedError != nil {
		log.Err(aggregatedError).Msg("Errors during provider processing")
		return ErrProcessingProvider
	}

	log.Info().
		Int("providers_completed", len(p)).
		Msg("All providers processed successfully")

	return nil
}

// processProvider processes a single provider with metrics tracking
func (p Providers) processProvider(
	ctx context.Context,
	provider base.Provider,
	repo repository.BlacklistRepository,
	pondCollector entry_collector.Collector,
	trackMetrics bool,
	wg *sync.WaitGroup,
	errChan chan error,
) {
	if wg != nil {
		defer wg.Done()
	}

	name := provider.GetName()
	source := provider.Source()
	processID := uuid.New()
	startedAt := time.Now()
	strProcessID := processID.String()

	// Start tracing span
	tracer := otel.Tracer("blacked/providers")
	ctx, span := tracer.Start(ctx, "provider.process",
		trace.WithAttributes(
			attribute.String("provider.name", name),
			attribute.String("provider.source", source),
			attribute.String("process.id", strProcessID),
		),
	)
	defer span.End()

	// Track metrics if enabled in Prometheus
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

// Fetch data with provider-specific TTL derived from cron schedule
	cronSchedule := provider.GetCronSchedule()
	ttl := utils.ParseTTLFromCron(cronSchedule)

	// Track fetch start time for duration metric
	fetchStart := time.Now()

	// Detect multi-page provider and use per-page persistence path
	if mpp, ok := provider.(base.MultiPageProvider); ok {
		p.processMultiPageProvider(ctx, mpp, pondCollector, trackMetrics, errChan)
		return
	}

	// Single-page provider — existing GetResponseReader path
	fetchSpan := trace.SpanFromContext(ctx)
	fetchSpan.AddEvent("fetching data from source")

	// Wrap FetchWithContext to match GetResponseReader's expected signature
	// FetchWithContext includes: timeout override (default 30s), retry with exponential backoff, circuit breaker
	fetchFunc := func() (io.Reader, error) {
		return provider.FetchWithContext(ctx)
	}

	reader, meta, err := utils.GetResponseReader(source, fetchFunc, name, strProcessID, ttl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch data")
		providerLogger.
			Err(err).
			Str("source", source).
			Str("provider", name).
			Msg("Error fetching data")

		// Update per-provider metrics on failure
		if trackMetrics {
			mc, _ := collector.GetMetricsCollector()
			if mc != nil {
				mc.RecordProviderRequest(name, "failure")
				mc.RecordProviderFetchDuration(name, time.Since(fetchStart))
				mc.SetSyncFailed(name, err, time.Since(startedAt))
			}
		}

		errChan <- err
		return
	}
	span.AddEvent("data fetched successfully")

	// Record success + bytes + duration for the HTTP fetch
	if trackMetrics {
		mc, _ := collector.GetMetricsCollector()
		if mc != nil {
			mc.RecordProviderRequest(name, "success")
			if meta != nil && meta.Bytes > 0 {
				mc.RecordProviderBytesTransferred(name, meta.Bytes)
			}
			mc.RecordProviderFetchDuration(name, time.Since(fetchStart))
		}
	}

	// Metadata is only used for caching - processID should NEVER be reused from cache
	// Each run must have a unique processID to properly track which entries belong to it
	// Reusing processID from cache causes RemoveOlderInsertions to delete wrong entries
	if meta != nil {
		// Log but ignore the cached processID - use the current run's locally generated processID
		providerLogger.Info().
			Str("cached_process_id", meta.ProcessID).
			Str("current_process_id", strProcessID).
			Msg("Ignoring cached processID, using fresh UUID for this run")
	}

	// Set the repository for the provider
	provider.SetRepository(repo)

	// Start tracking provider metrics in the pond collector
	pondCollector.StartProviderProcessing(name, strProcessID)

	// Parse the data - this delegates to the provider's implementation
	span.AddEvent("parsing provider data")
	if err := provider.Parse(reader); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse data")
		providerLogger.
			Err(err).
			Str("source", source).
			Str("provider", name).
			Msg("Error parsing data")

		// Finish tracking in the collector
		pondCollector.FinishProviderProcessing(name, strProcessID)

		// Update Prometheus metrics on failure
		if trackMetrics {
			mc, _ := collector.GetMetricsCollector()
			if mc != nil {
				mc.SetSyncFailed(name, err, time.Since(startedAt))
			}
		}

		errChan <- err
		return
	}
	span.AddEvent("parsing completed")

	// Finish tracking provider metrics in the pond collector
	entriesProcessed, processingTime, _ := pondCollector.FinishProviderProcessing(name, strProcessID)
	span.AddEvent("provider processing finished")

	// Remove stale entries and sync bloom filter after provider finishes
	// This ensures entries not present in the latest run are soft-deleted
	// and removed from the bloom filter to prevent false positives
	if err := pondCollector.RemoveStaleEntriesAndSyncBloom(context.Background(), name, strProcessID); err != nil {
		providerLogger.Err(err).
			Str("provider", name).
			Str("process_id", strProcessID).
			Msg("Failed to remove stale entries and sync bloom")
		// Don't return error - this is cleanup, not critical
	}

	// Cleanup if needed
	cfg := config.GetConfig()
	if cfg.APP.Environment == "development" {
		utils.RemoveStoredResponse(name)
	}

	// Update Prometheus metrics on success
	if trackMetrics {
		mc, _ := collector.GetMetricsCollector()
		if mc != nil {
			mc.SetSyncSuccess(name, time.Since(startedAt))
		}
	}

	// Calculate entries per second
	var entriesPerSecond float64
	if processingTime.Seconds() > 0 {
		entriesPerSecond = float64(entriesProcessed) / processingTime.Seconds()
	}

	providerLogger.Info().
		TimeDiff("duration", time.Now(), startedAt).
		Int("entries_processed", entriesProcessed).
		Float64("entries_per_second", entriesPerSecond).
		Msg("Finished processing provider")
}

// processMultiPageProvider handles providers that fetch in pages using per-page persistence.
// Memory usage is bounded by a single page size (~100KB) regardless of total page count.
// Each page is saved to disk as page_NNN.dat immediately after fetch, then parsed.
// On crash, ResumePageNumber scans the directory to resume from the next uncompleted page.
func (p Providers) processMultiPageProvider(
	ctx context.Context,
	provider base.MultiPageProvider,
	pondCollector entry_collector.Collector,
	trackMetrics bool,
	errChan chan error,
) {
	startedAt := time.Now()
	name := provider.(base.Provider).GetName()
	processID := uuid.New()
	strProcessID := processID.String()

	// Get config for store path
	cfg := config.GetConfig()
	storePath := cfg.Collector.StorePath

	// Start tracing span
	tracer := otel.Tracer("blacked/providers")
	ctx, span := tracer.Start(ctx, "provider.process.multipage",
		trace.WithAttributes(
			attribute.String("provider.name", name),
			attribute.String("process.id", strProcessID),
		),
	)
	defer span.End()

	// Track metrics
	if trackMetrics {
		mc, err := collector.GetMetricsCollector()
		if err == nil && mc != nil {
			mc.SetSyncRunning(name)
		}
	}

	providerLogger := log.With().
		Str("process_id", strProcessID).
		Str("provider", name).
		Logger()

	providerLogger.Info().Time("starts", startedAt).Msg("Processing multi-page provider")

	// Start provider processing in collector
	provider.(base.Provider).SetProcessID(processID)
	provider.(base.Provider).SetRepository(nil) // not used in multi-page path
	pondCollector.StartProviderProcessing(name, strProcessID)

	// Fetch pages with per-page persistence
	resultChan, fetchErr := provider.FetchPages(ctx)

	totalIndicators := 0
	pageCount := 0
	var lastErr error
	totalBytes := int64(0)

	for {
		select {
		case <-ctx.Done():
			providerLogger.Warn().Msg("context cancelled — stopping multi-page processing")
			span.SetStatus(codes.Error, "context cancelled")
			break
		case result, ok := <-resultChan:
			if !ok {
				// Channel closed — fetch done (success or terminal error)
				goto done
			}
			pageCount++
			totalIndicators += result.Indicators
			totalBytes += result.Bytes

			// Record per-page metrics
			if trackMetrics {
				mc, _ := collector.GetMetricsCollector()
				if mc != nil {
					mc.RecordProviderPagesFetched(name)
					if result.Bytes > 0 {
						mc.RecordProviderBytesTransferred(name, result.Bytes)
					}
				}
			}

			providerLogger.Info().
				Int("page", result.PageNumber).
				Int("indicators", result.Indicators).
				Int64("bytes", result.Bytes).
				Bool("has_next", result.HasNextPage).
				Msg("Processed page from multi-page provider")

			if !result.HasNextPage {
				// No more pages — signal end
				goto done
			}
		}

		// Check if fetch returned an error
		if fetchErr != nil {
			lastErr = fetchErr
			providerLogger.Err(fetchErr).Msg("FetchPages error")
			span.RecordError(fetchErr)
			break
		}
	}

done:
	entriesProcessed, processingTime, _ := pondCollector.FinishProviderProcessing(name, strProcessID)

	// Remove stale entries and sync bloom after provider finishes
	if err := pondCollector.RemoveStaleEntriesAndSyncBloom(context.Background(), name, strProcessID); err != nil {
		providerLogger.Err(err).
			Str("provider", name).
			Str("process_id", strProcessID).
			Msg("Failed to remove stale entries and sync bloom")
	}

	// Mark fetch complete in meta file
	if err := utils.MarkProviderFetchComplete(storePath, name, totalIndicators); err != nil {
		providerLogger.Warn().Err(err).Msg("Failed to mark fetch complete in meta file")
	}

	// Cleanup per-page data directory in development mode
	if cfg.APP.Environment == "development" {
		// In dev, we can clean up after successful completion
	}

	// Update metrics
	if trackMetrics {
		mc, _ := collector.GetMetricsCollector()
		if mc != nil {
			// Record request count and fetch duration for multi-page provider
			mc.RecordProviderRequest(name, "success")
			mc.RecordProviderFetchDuration(name, time.Since(startedAt))
			if lastErr != nil {
				mc.SetSyncFailed(name, lastErr, time.Since(startedAt))
			} else {
				mc.SetSyncSuccess(name, time.Since(startedAt))
			}
		}
	}

	var entriesPerSecond float64
	if processingTime.Seconds() > 0 {
		entriesPerSecond = float64(entriesProcessed) / processingTime.Seconds()
	}

	if lastErr != nil {
		errChan <- lastErr
	}

	providerLogger.Info().
		TimeDiff("duration", time.Now(), startedAt).
		Int("pages", pageCount).
		Int("entries_processed", entriesProcessed).
		Float64("entries_per_second", entriesPerSecond).
		Msg("Finished processing multi-page provider")
}
