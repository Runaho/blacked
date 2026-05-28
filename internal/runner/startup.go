package runner

import (
	"blacked/features/entry_collector"
	"blacked/features/entries/repository"
	"blacked/features/providers"
	"blacked/features/providers/base"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/utils"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Types
type StartupAction string

const (
	StartupSkip      StartupAction = "skip"
	StartupRestore   StartupAction = "restore"
	StartupFullFetch StartupAction = "full_fetch"
)

type ProviderStartupDecision struct {
	ProviderName     string
	DBPopulated      bool
	StoredFilesExist bool
	StoredFilesFresh bool
	Action           StartupAction
	DBEntryCount     int
	StoredFileAge    time.Duration
	Reason           string
}

// EvaluateStartupState — per-provider decision engine
func EvaluateStartupState(ctx context.Context, providers []base.Provider) ([]ProviderStartupDecision, error) {
	if len(providers) == 0 {
		return nil, errors.New("no providers specified for startup evaluation")
	}

	rwDB, err := db.GetWriteDB()
	if err != nil {
		return nil, err
	}
	repo := repository.NewSQLiteRepository(rwDB)

	var decisions []ProviderStartupDecision

	for _, provider := range providers {
		providerName := provider.GetName()
		dbCount, err := repo.StreamEntriesCountBySource(ctx, providerName)
		if err != nil {
			log.Warn().Err(err).Str("provider", providerName).Msg("Failed to evaluate DB state for provider, assuming empty")
			dbCount = 0
		}
		dbPopulated := dbCount > 0

		decision := evaluateProvider(provider, dbPopulated, dbCount)
		decisions = append(decisions, decision)
	}

	return decisions, nil
}

func evaluateProvider(provider base.Provider, dbPopulated bool, dbCount int) ProviderStartupDecision {
	providerName := provider.GetName()
	cfg := config.GetConfig()
	storePath := cfg.Collector.StorePath

	decision := ProviderStartupDecision{
		ProviderName: providerName,
		DBPopulated:  dbPopulated,
	}

	dataFileName, metaFileName := utils.GenerateFilenames(storePath, providerName)

	_, dataErr := os.Stat(dataFileName)
	_, metaErr := os.Stat(metaFileName)
	decision.StoredFilesExist = dataErr == nil && metaErr == nil

	if decision.StoredFilesExist {
		age, fresh := checkStoredFileFreshness(metaFileName, provider.GetCronSchedule())
		decision.StoredFileAge = age
		decision.StoredFilesFresh = fresh
	}

	decision.DBEntryCount = dbCount

	switch {
	case dbPopulated && decision.StoredFilesExist && decision.StoredFilesFresh:
		decision.Action = StartupSkip
		decision.Reason = "DB populated and stored file fresh; skipping fetch, cron will handle updates"
	case !dbPopulated && decision.StoredFilesExist && decision.StoredFilesFresh:
		decision.Action = StartupRestore
		decision.Reason = "DB empty but stored file fresh; restoring from stored response"
	case !decision.StoredFilesExist || !decision.StoredFilesFresh:
		decision.Action = StartupFullFetch
		if !decision.StoredFilesExist {
			decision.Reason = "no stored file found; performing full fetch"
		} else {
			decision.Reason = "stored file stale; performing full fetch"
		}
	}

	log.Info().
		Str("provider", providerName).
		Bool("db_populated", decision.DBPopulated).
		Bool("stored_exist", decision.StoredFilesExist).
		Bool("stored_fresh", decision.StoredFilesFresh).
		Dur("stored_age", decision.StoredFileAge).
		Str("action", string(decision.Action)).
		Str("reason", decision.Reason).
		Msg("startup decision")

	return decision
}

// checkStoredFileFreshness reads the metadata file and returns age + whether it's fresh.
func checkStoredFileFreshness(metaFileName string, cronSchedule string) (time.Duration, bool) {
	metaFile, err := os.Open(metaFileName)
	if err != nil {
		log.Warn().Str("file", metaFileName).Msg("failed to open stored metadata file")
		return 0, false
	}
	defer metaFile.Close()

	var metadata utils.ResponseMetadata
	if err := json.NewDecoder(metaFile).Decode(&metadata); err != nil {
		log.Warn().Str("file", metaFileName).Msg("failed to decode stored metadata")
		return 0, false
	}

	age := time.Since(metadata.CreatedAt)
	ttl := utils.ParseTTLFromCron(cronSchedule)

	isFresh := age <= ttl
	log.Debug().
		Str("file", metaFileName).
		Time("created", metadata.CreatedAt).
		Dur("age", age).
		Dur("ttl", ttl).
		Bool("fresh", isFresh).
		Msg("stored file freshness check")

	return age, isFresh
}

// ExecuteStartupActions runs the decisions.
func ExecuteStartupActions(ctx context.Context, decisions []ProviderStartupDecision, providers []base.Provider) error {
	if len(decisions) == 0 {
		return nil
	}

	log.Info().Int("decisions", len(decisions)).Msg("executing startup actions")

	providerMap := make(map[string]base.Provider)
	for _, p := range providers {
		providerMap[p.GetName()] = p
	}

	for _, decision := range decisions {
		provider, ok := providerMap[decision.ProviderName]
		if !ok {
			log.Warn().Str("provider", decision.ProviderName).Msg("provider not found in registry")
			continue
		}

		switch decision.Action {
		case StartupSkip:
			log.Info().Str("provider", decision.ProviderName).Msg("startup: skipping fetch, relying on cron")

		case StartupRestore:
			log.Info().Str("provider", decision.ProviderName).Msg("startup: restoring from stored file")
			if err := restoreProviderFromStoredFile(ctx, provider); err != nil {
				log.Err(err).Str("provider", decision.ProviderName).Msg("startup: restore failed, falling back to full fetch")
				if err := runFullFetchForProvider(ctx, provider); err != nil {
					log.Err(err).Str("provider", decision.ProviderName).Msg("startup: full fetch also failed")
				}
			}

		case StartupFullFetch:
			log.Info().Str("provider", decision.ProviderName).Msg("startup: performing full fetch")
			if err := runFullFetchForProvider(ctx, provider); err != nil {
				log.Err(err).Str("provider", decision.ProviderName).Msg("startup: full fetch failed")
			}
		}
	}

	return nil
}

// restoreProviderFromStoredFile restores entries from a stored .dat file (no HTTP fetch).
func restoreProviderFromStoredFile(ctx context.Context, provider base.Provider) error {
	providerName := provider.GetName()
	cfg := config.GetConfig()
	storePath := cfg.Collector.StorePath

	dataFileName, _ := utils.GenerateFilenames(storePath, providerName)

	file, err := os.Open(dataFileName)
	if err != nil {
		return err
	}
	defer file.Close()

	log.Info().Str("provider", providerName).Str("file", dataFileName).Msg("reading stored response for restore")

	// Set repository so provider.Parse() can write to DB
	rwDB, err := db.GetWriteDB()
	if err != nil {
		return err
	}
	repo := repository.NewSQLiteRepository(rwDB)
	provider.SetRepository(repo)

	if err := provider.Parse(file); err != nil {
		return err
	}

	collector := entry_collector.GetPondCollector()
	if collector != nil {
		collector.ScheduleCacheSync(true)
		collector.WaitForCacheSyncCompletion()
	}

	log.Info().Str("provider", providerName).Msg("restore from stored file completed")
	return nil
}

// runFullFetchForProvider fetches data from the provider via HTTP and processes it.
func runFullFetchForProvider(ctx context.Context, provider base.Provider) error {
	providersList := providers.Providers{provider}
	return providersList.Process(ctx, providers.ProcessOptions{
		UpdateCacheMode: providers.UpdateCacheDeferred,
		TrackMetrics:    true,
	})
}

// RunStartupProvidersAsync evaluates startup state and executes actions asynchronously (non-blocking).
// This allows the server to start serving requests while provider startup processing continues in the background.
// The startup process is observable via /provider/process/status/:processID endpoints.
func RunStartupProvidersAsync(ctx context.Context, providerList []base.Provider) {
	if len(providerList) == 0 {
		log.Warn().Msg("no providers for startup evaluation")
		return
	}

	// Get decisions first (fast, synchronous) - this does DB lookups
	decisions, err := EvaluateStartupState(ctx, providerList)
	if err != nil {
		log.Err(err).Msg("startup evaluation failed, proceeding with fallback")
		// Fall back to simple full fetch for all providers
		decisions = make([]ProviderStartupDecision, len(providerList))
		for i, p := range providerList {
			decisions[i] = ProviderStartupDecision{
				ProviderName: p.GetName(),
				Action:       StartupFullFetch,
				Reason:       "evaluation failed, fallback to full fetch",
			}
		}
	}

	// Get provider names for process tracking
	providerNames := make([]string, len(providerList))
	for i, p := range providerList {
		providerNames[i] = p.GetName()
	}

	// Try to acquire process lock via ProcessManager
	pm := providers.GetProcessManager()
	processID, err := pm.TryStartProcess(ctx, "startup", providerNames, nil)
	if err != nil {
		log.Warn().Err(err).Msg("Cannot run startup providers - another process is running")
		return
	}

	log.Info().
		Str("process_id", processID).
		Int("providers", len(providerList)).
		Str("decisions", StartupSummary(decisions)).
		Msg("Starting async startup provider processing")

	// Run in background goroutine - this is the async part that doesn't block server startup
	go func() {
		var processErr error
		defer func() {
			pm.FinishProcess(processID, processErr)
		}()

		if err := ExecuteStartupActions(context.Background(), decisions, providerList); err != nil {
			processErr = err
			log.Err(err).Msg("Async startup provider execution failed")
		} else {
			log.Info().Msg("Async startup provider execution completed")
		}
	}()
}

// RunStartupProviders evaluates startup state and executes the decided actions.
// This is the synchronous version - prefer RunStartupProvidersAsync for non-blocking operation.
func RunStartupProviders(ctx context.Context, providers []base.Provider) error {
	if len(providers) == 0 {
		log.Warn().Msg("no providers for startup evaluation")
		return nil
	}

	log.Info().Int("providers", len(providers)).Msg("evaluating startup state for providers")

	decisions, err := EvaluateStartupState(ctx, providers)
	if err != nil {
		log.Err(err).Msg("startup evaluation failed, proceeding with full fetch for all providers")
		for _, provider := range providers {
			if err := runFullFetchForProvider(ctx, provider); err != nil {
				log.Err(err).Str("provider", provider.GetName()).Msg("startup fallback fetch failed")
			}
		}
		return err
	}

	return ExecuteStartupActions(ctx, decisions, providers)
}

// StartupSummary returns a comma-separated summary of decisions.
func StartupSummary(decisions []ProviderStartupDecision) string {
	var parts []string
	for _, d := range decisions {
		parts = append(parts, d.ProviderName+": "+string(d.Action))
	}
	return strings.Join(parts, ", ")
}
