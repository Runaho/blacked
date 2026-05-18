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

	dbPopulated, dbCount, err := evaluateDBState(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to evaluate DB state, assuming empty")
		dbPopulated = false
		dbCount = 0
	}

	var decisions []ProviderStartupDecision

	for _, provider := range providers {
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

// DB evaluation helpers
func evaluateDBState(ctx context.Context) (bool, int, error) {
	rwDB, err := db.GetWriteDB()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get write DB for startup check")
		return false, 0, err
	}

	repo := repository.NewSQLiteRepository(rwDB)
	count, err := repo.StreamEntriesCount(ctx)
	if err != nil {
		return false, 0, err
	}

	populated := count > 0
	return populated, count, nil
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
	ttl := parseTTLFromCron(cronSchedule)

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

// parseTTLFromCron extracts a sensible TTL from a cron schedule.
func parseTTLFromCron(cronSchedule string) time.Duration {
	if d, err := time.ParseDuration(cronSchedule); err == nil {
		return d
	}

	if cronSchedule == "" {
		log.Debug().Msg("no cron schedule configured, using default 6h TTL")
		return 6 * time.Hour
	}

	scheduleLower := strings.ToLower(cronSchedule)
	if strings.Contains(scheduleLower, "hour") || strings.Contains(scheduleLower, "h") {
		return 1 * time.Hour
	}
	if strings.Contains(scheduleLower, "day") || strings.Contains(scheduleLower, "d") {
		return 24 * time.Hour
	}
	if strings.Contains(scheduleLower, "week") || strings.Contains(scheduleLower, "w") {
		return 7 * 24 * time.Hour
	}

	log.Debug().Str("cron", cronSchedule).Msg("using default 6h TTL for unknown cron format")
	return 6 * time.Hour
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

// RunStartupProviders evaluates startup state and executes the decided actions.
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
