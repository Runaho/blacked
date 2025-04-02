package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"blacked/features/providers/base"

	"github.com/go-co-op/gocron/v2"
	"github.com/rs/zerolog/log"
)

var (
	ErrFailedToCreateScheduler = errors.New("failed to create scheduler")
	ErrProviderAlreadyExists   = errors.New("provider already registered")
	ErrFailedToCreateJob       = errors.New("failed to create job")
	ErrFailedToGetNextRun      = errors.New("failed to get next run time")
)

// Runner manages scheduled provider executions
type Runner struct {
	scheduler gocron.Scheduler
	jobs      map[string]gocron.Job
	providers map[string]base.Provider
	mu        sync.RWMutex
}

// NewRunner creates a new scheduler runner
func NewRunner() (*Runner, error) {
	scheduler, err := gocron.NewScheduler(
		gocron.WithLocation(time.UTC),
		gocron.WithGlobalJobOptions(
			gocron.WithSingletonMode(gocron.LimitModeReschedule),
		),
	)

	if err != nil {
		log.Error().Err(err).Msg("Failed to create scheduler")
		return nil, ErrFailedToCreateScheduler
	}

	return &Runner{
		scheduler: scheduler,
		jobs:      make(map[string]gocron.Job),
		providers: make(map[string]base.Provider),
	}, nil
}

// RegisterProvider adds a provider to the runner with optional cron schedule
func (r *Runner) RegisterProvider(provider base.Provider, cronSchedule string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	providerName := provider.GetName()

	// If provider already registered, return error
	if _, exists := r.providers[providerName]; exists {
		log.Error().Str("provider", providerName).Msg("Provider already registered")
		return ErrProviderAlreadyExists
	}

	// Add provider to registry
	r.providers[providerName] = provider

	// Use provided cron schedule or default from config
	if cronSchedule == "" {
		log.Warn().Str("provider", providerName).Msg("No cron schedule provided, using default")
		return nil
	}

	if !base.GetIsSchedulingEnabled(providerName) {
		log.Warn().Str("provider", providerName).Msg("Scheduling is disabled for this provider")
		return nil
	}

	// Create a job for this provider
	job, err := r.scheduler.NewJob(
		gocron.CronJob(
			cronSchedule,
			false,
		),
		gocron.NewTask(
			r.executeProvider,
			providerName,
		),
		gocron.WithName(strings.Join([]string{"provider", providerName}, "_")),
		gocron.WithTags([]string{"provider", providerName}...),
	)

	if err != nil {
		log.Error().Err(err).Str("provider", providerName).Msg("Failed to schedule job for provider")
		return ErrFailedToCreateJob
	}

	r.jobs[providerName] = job
	nextRun, e := job.NextRun()

	if e != nil {
		log.Error().Err(e).Str("provider", providerName).Msg("Failed to get next run time")
		return ErrFailedToGetNextRun
	}

	log.Info().
		Str("provider", providerName).
		Str("cron", cronSchedule).
		Time("next_run", nextRun).
		Msg("Provider registered with scheduler")

	return nil
}

// executeProvider is the function that gets called on schedule
func (r *Runner) executeProvider(providerName string) {
	r.mu.RLock()
	provider, exists := r.providers[providerName]
	r.mu.RUnlock()

	if !exists {
		log.Error().Str("provider", providerName).Msg("Provider not found in registry")
		return
	}

	log.Info().
		Str("provider", providerName).
		Msg("Starting scheduled execution of provider")

	if err := ExecuteProvider(context.Background(), provider); err != nil {
		log.Error().
			Err(err).
			Str("provider", providerName).
			Msg("Error executing provider")
	}
}

// Start begins the scheduler
func (r *Runner) Start() {
	r.scheduler.Start()
	log.Info().Int("jobs", len(r.jobs)).Msg("Scheduler started")
}

// Stop halts the scheduler
func (r *Runner) Stop(ctx context.Context) error {
	return r.scheduler.Shutdown()
}

// RunProviderImmediately executes a provider right now without waiting for schedule
func (r *Runner) RunProviderImmediately(providerName string) error {
	r.mu.RLock()
	provider, exists := r.providers[providerName]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("provider %s not registered", providerName)
	}

	err := ExecuteProvider(context.Background(), provider)
	if err != nil {
		log.Error().
			Err(err).
			Str("provider", providerName).
			Msg("Error executing provider immediately")
	}

	return err
}

// GetNextRunTime returns the next scheduled run for a provider
func (r *Runner) GetNextRunTime(providerName string) (time.Time, error) {
	r.mu.RLock()
	job, exists := r.jobs[providerName]
	r.mu.RUnlock()

	if !exists {
		return time.Time{}, fmt.Errorf("no job found for provider %s", providerName)
	}

	return job.NextRun()
}

// GetAllNextRunTimes returns all scheduled run times by provider
func (r *Runner) GetAllNextRunTimes() map[string]time.Time {
	result := make(map[string]time.Time)

	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, job := range r.jobs {
		nr, err := job.NextRun()
		if err != nil {
			log.Error().Err(err).Str("provider", name).Msg("Error getting next run time")
			result[name] = time.Time{}
			continue
		}
		result[name] = nr
	}

	return result
}
