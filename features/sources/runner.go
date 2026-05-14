package sources

import (
	"blacked/features/entry_collector"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Runner executes sources on their scheduled intervals.
// It replaces the old global registration model with a per-source model.
type Runner struct {
	mu             sync.RWMutex
	jobs           map[string]*Job // source_id -> job
	registry       *Registry
	collector      entry_collector.Collector
	processManager ProcessManager
}

// Job represents a scheduled source execution.
type Job struct {
	SourceID     string
	CronSchedule string
	LastRun      time.Time
	NextRun      time.Time
	Running      bool
}

// ProcessManager tracks running source processes.
type ProcessManager interface {
	StartProcess(sourceID, processID string)
	FinishProcess(sourceID, processID string) (count int, duration time.Duration, ok bool)
}

// NewRunner creates a source runner backed by a registry.
func NewRunner(registry *Registry, collector entry_collector.Collector) *Runner {
	if registry == nil {
		registry = DefaultRegistry
	}
	return &Runner{
		registry:  registry,
		collector: collector,
		jobs:      make(map[string]*Job),
	}
}

// RegisterJob schedules a source for execution.
func (r *Runner) RegisterJob(sourceID, cronSchedule string) error {
	src := r.registry.Get(sourceID)
	if src == nil {
		return fmt.Errorf("source not found: %s", sourceID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.jobs[sourceID]; exists {
		log.Warn().Str("source_id", sourceID).Msg("Job already registered, skipping")
		return nil
	}

	r.jobs[sourceID] = &Job{
		SourceID:     sourceID,
		CronSchedule: cronSchedule,
	}

	log.Info().
		Str("source_id", sourceID).
		Str("cron", cronSchedule).
		Msg("Source job registered")

	return nil
}

// Execute immediately fetches and parses a single source.
func (r *Runner) Execute(ctx context.Context, sourceID string, opts ...ExecuteOption) error {
	src := r.registry.Get(sourceID)
	if src == nil {
		return fmt.Errorf("source not found: %s", sourceID)
	}

	// Merge options
	opt := ExecuteOptions{UpdateCache: false}
	for _, o := range opts {
		o(&opt)
	}

	r.mu.Lock()
	job, exists := r.jobs[sourceID]
	if !exists {
		job = &Job{SourceID: sourceID}
		r.jobs[sourceID] = job
	}
	if job.Running {
		r.mu.Unlock()
		return fmt.Errorf("source %s is already running", sourceID)
	}
	job.Running = true
	job.LastRun = time.Now()
	processID := fmt.Sprintf("%s-%d", sourceID, time.Now().Unix())
	r.mu.Unlock()

	// Start tracking
	if r.processManager != nil {
		r.processManager.StartProcess(sourceID, processID)
	}
	r.collector.StartProviderProcessing(sourceID, processID)

	startedAt := time.Now()
	var entryCount int

	err := r.runSource(ctx, src, processID)

	duration := time.Since(startedAt)

	// Finish tracking
	count, _, ok := r.collector.FinishProviderProcessing(sourceID, processID)
	if ok {
		entryCount = count
	}
	if r.processManager != nil {
		r.processManager.FinishProcess(sourceID, processID)
	}

	r.mu.Lock()
	job.Running = false
	r.mu.Unlock()

	if err != nil {
		log.Error().Err(err).
			Str("source_id", sourceID).
			Dur("duration", duration).
			Msg("Source execution failed")
		return err
	}

	log.Info().
		Str("source_id", sourceID).
		Int("entries", entryCount).
		Dur("duration", duration).
		Msg("Source execution completed")

	if opt.UpdateCache {
		// TODO: Phase 3 — per-source bloom rebuild
		log.Debug().Str("source_id", sourceID).Msg("Cache update requested (not yet implemented)")
	}

	return nil
}

// runSource performs the actual fetch + parse for a single source.
func (r *Runner) runSource(ctx context.Context, src *Source, processID string) error {
	if src.Fetcher == nil {
		return fmt.Errorf("source %s has no fetcher", src.ID)
	}
	if src.Parser == nil {
		return fmt.Errorf("source %s has no parser", src.ID)
	}

	reader, err := src.Fetcher.Fetch(src.SourceURL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	if err := src.Parser.Parse(reader, r.collector, src.ID, processID); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	return nil
}

// ExecuteOptions configures a single execution.
type ExecuteOptions struct {
	UpdateCache bool
}

// ExecuteOption mutates ExecuteOptions.
type ExecuteOption func(*ExecuteOptions)

// WithCacheUpdate enables cache update after execution.
func WithCacheUpdate() ExecuteOption {
	return func(o *ExecuteOptions) {
		o.UpdateCache = true
	}
}

// FilterEnabled returns only jobs for enabled sources.
func (r *Runner) FilterEnabled() []*Source {
	return r.registry.FilterEnabled()
}

// Jobs returns a snapshot of all registered jobs.
func (r *Runner) Jobs() map[string]*Job {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]*Job, len(r.jobs))
	for k, v := range r.jobs {
		cp := *v
		out[k] = &cp
	}
	return out
}
