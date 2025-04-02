package runner

import (
	"blacked/features/cache"
	"blacked/features/entries/repository"
	"blacked/features/providers/base"
	"blacked/internal/db"
	"blacked/internal/utils"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ExecuteProvider runs a single provider with proper repository setup
func ExecuteProvider(ctx context.Context, provider base.Provider) error {
	startedAt := time.Now()
	processID := uuid.New()

	source := provider.Source()
	name := provider.GetName()
	strProcessID := processID.String()

	// Create a logger with context for this provider execution
	providerLogger := log.With().
		Str("process_id", strProcessID).
		Str("source", source).
		Str("provider", name).
		Logger()

	providerLogger.Info().
		Time("starts", startedAt).
		Msg("Start scheduled execution of provider")

	// Set the process ID on the provider
	provider.SetProcessID(processID)

	// Get database connection
	rwDB, err := db.GetDB()
	if err != nil {
		providerLogger.Error().
			Err(err).
			Msg("Failed to get database for provider execution")
		return fmt.Errorf("failed to get database: %w", err)
	}

	// Create repository
	repo := repository.NewSQLiteRepository(rwDB)

	// Set repository on provider
	provider.SetRepository(repo)

	// Fetch the data
	reader, meta, err := utils.GetResponseReader(source, provider.Fetch, name, strProcessID)
	if err != nil {
		providerLogger.Error().
			Err(err).
			Msg("Error fetching data")
		return fmt.Errorf("error fetching data: %w", err)
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
	if err := provider.Parse(reader); err != nil {
		providerLogger.Error().
			Err(err).
			Msg("Error parsing data")
		return fmt.Errorf("error parsing data: %w", err)
	}

	duration := time.Since(startedAt)
	providerLogger.Info().
		Dur("duration", duration).
		Msg("Completed scheduled execution of provider")

	cache.FireAndForgetSync()

	return nil
}
