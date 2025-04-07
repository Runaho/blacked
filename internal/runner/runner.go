package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"blacked/features/providers"
	"blacked/features/providers/base"

	"github.com/go-co-op/gocron/v2"
	"github.com/rs/zerolog/log"
)

// Error variables for runner package
var (
	ErrFailedToCreateScheduler = errors.New("failed to create scheduler")
	ErrProviderAlreadyExists   = errors.New("provider already registered")
	ErrFailedToCreateJob       = errors.New("failed to create job")
	ErrFailedToGetNextRun      = errors.New("failed to get next run time")
	ErrNoProvidersSpecified    = errors.New("no providers specified for scheduling")
	ErrRunnerNotInitialized    = errors.New("cron runner not initialized")
	ErrInvalidCronSchedule     = errors.New("invalid cron schedule")
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
		// Use the provided cron schedule
		gocron.CronJob(
			cronSchedule,
			false,
		),

		// Use the provider's execution function
		gocron.NewTask(
			r.executeProvider,
			providerName,
		),

		// Set job description and tags
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
	defer r.mu.RUnlock()

	provider, exists := r.providers[providerName]

	if !exists {
		log.Error().Str("provider", providerName).Msg("Provider not found in registry")
		return
	}

	log.Info().
		Str("provider", providerName).
		Msg("Starting scheduled execution of provider")

	// Execute the provider with immediate cache updates
	if err := ExecuteProvider(context.Background(), provider, providers.ProcessOptions{
		UpdateCacheMode: providers.UpdateCacheImmediate, // Use immediate updates for scheduled runs
		TrackMetrics:    true,
	}); err != nil {
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

// GetNextRunTime returns the next scheduled run for a provider
func (r *Runner) GetNextRunTime(providerName string) (time.Time, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	job, exists := r.jobs[providerName]

	if !exists {
		log.Error().Str("provider", providerName).Msg("No job found for provider")
		return time.Time{}, ErrRunnerNotInitialized
	}

	return job.NextRun()
}

func (r *Runner) runJob(providerName string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, exists := r.jobs[providerName]

	if !exists {
		log.Error().Str("provider", providerName).Msg("Job not found")
		return
	}

	if err := job.RunNow(); err != nil {
		log.Error().Err(err).Str("provider", providerName).Msg("Error running job")
	}
}

func (r *Runner) RunProviderJobsNow() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providersList := make([]base.Provider, 0, len(r.providers))

	// Collect all providers first
	for _, provider := range r.providers {
		providersList = append(providersList, provider)
	}

	if len(providersList) == 0 {
		log.Info().Msg("No providers to run at startup")
		return
	}

	log.Info().Int("count", len(providersList)).Msg("Running all providers at startup in bulk mode")

	// Execute all providers in bulk with a single cache update at the end
	if err := ExecuteProviders(context.Background(), providersList); err != nil {
		log.Error().Err(err).Msg("Error executing providers in bulk at startup")
	} else {
		log.Info().Msg("Successfully executed all providers at startup")
	}
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
