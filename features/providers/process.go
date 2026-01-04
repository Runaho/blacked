package providers

import (
	"blacked/features/entries/repository"
	"blacked/features/entry_collector"
	"blacked/features/providers/base"
	"blacked/internal/collector"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/utils"

	"context"
	"errors"
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
	providerConfig := config.GetConfig().Provider
	maxConcurrent := providerConfig.MaxConcurrentProviders
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

	// Fetch data
	fetchSpan := trace.SpanFromContext(ctx)
	fetchSpan.AddEvent("fetching data from source")
	reader, meta, err := utils.GetResponseReader(source, provider.Fetch, name, strProcessID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch data")
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
	span.AddEvent("data fetched successfully")

	// Handle metadata if present
	if meta != nil {
		strProcessID = meta.ProcessID
		providerLogger.Info().
			Str("new_process_id", strProcessID).
			Msg("Found metadata, changing process ID")
		provider.SetProcessID(uuid.MustParse(strProcessID))
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

	// Cleanup if needed
	cfg := config.GetConfig()
	if cfg.APP.Environtment == "development" {
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
