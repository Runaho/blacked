package services

import (
	"blacked/features/providers"
	"blacked/features/providers/repository"
	"blacked/internal/db"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	ErrDatabaseConnection  = errors.New("failed to connect to database")
	ErrProcessRunning      = errors.New("another process is already running")
	ErrInsertProcess       = errors.New("failed to insert process status")
	ErrUpdateProcess       = errors.New("failed to update process status")
	ErrProcessNotFound     = errors.New("process not found")
	ErrGetProcessStatus    = errors.New("failed to get process status")
	ErrListProcesses       = errors.New("failed to list processes")
	ErrCheckRunningProcess = errors.New("failed to check for running processes")
)

type ProviderProcessService struct {
	repo                    repository.ProviderProcessRepository
	processDeadlineDuration time.Duration
}

func SetProcessDeadlineDuration(duration time.Duration) func(*ProviderProcessService) {
	return func(s *ProviderProcessService) {
		s.processDeadlineDuration = duration
	}
}
func NewProviderProcessService() (*ProviderProcessService, error) {
	// Use write connection since this service inserts/updates process records
	conn, err := db.GetWriteDB()
	if err != nil {
		log.Err(err).Msg("Failed to connect to database")
		return nil, ErrDatabaseConnection
	}
	return &ProviderProcessService{
		repo:                    repository.NewSQLiteProviderProcessRepository(conn),
		processDeadlineDuration: 5 * time.Minute,
	}, nil
}

func (s *ProviderProcessService) StartProcess(ctx context.Context, providersToProcess []string, providersToRemove []string) (processID string, err error) {
	// Use the centralized process manager to check and acquire lock
	pm := providers.GetProcessManager()
	processIDStr, err := pm.TryStartProcess(ctx, "api", providersToProcess, providersToRemove)
	if err != nil {
		if err == providers.ErrProcessAlreadyRunning {
			log.Info().Msg("Another process is already running")
			return "", ErrProcessRunning
		}
		log.Err(err).Msg("Failed to start process")
		return "", err
	}

	status := &providers.ProcessStatus{
		ID:                 processIDStr,
		Status:             "running",
		StartTime:          time.Now(),
		ProvidersProcessed: providersToProcess,
		ProvidersRemoved:   providersToRemove,
	}

	// Also persist to database for historical records
	if err := s.repo.InsertProcess(ctx, status); err != nil {
		// Release the process lock since we failed
		pm.FinishProcess(processIDStr, err)
		log.Err(err).Msg("Failed to insert process status")
		return "", ErrInsertProcess
	}

	go func() {
		var processErr error
		defer func() {
			// Release the centralized process lock
			pm.FinishProcess(processIDStr, processErr)
		}()

		processErr = providers.GetProviders().Processor(providersToProcess, providersToRemove)
		if processErr != nil {
			status.Status = "failed"
			status.EndTime = time.Now()
			status.Error = processErr.Error()
		} else {
			status.Status = "completed"
			status.EndTime = time.Now()
		}
		if updateErr := s.repo.UpdateProcessStatus(context.Background(), status); updateErr != nil {
			log.Err(updateErr).
				Str("process_id", processIDStr).
				Msg("Failed to update process status after completion")
		}
	}()

	return processIDStr, nil
}

func (s *ProviderProcessService) StartProcessAsync(ctx context.Context, providersToProcess []string, providersToRemove []string) (processID string, err error) {
	// Use the centralized process manager to check and acquire lock
	pm := providers.GetProcessManager()
	processIDStr, err := pm.TryStartProcess(ctx, "api-sync", providersToProcess, providersToRemove)
	if err != nil {
		if err == providers.ErrProcessAlreadyRunning {
			log.Info().Msg("Another process is already running")
			return "", ErrProcessRunning
		}
		log.Err(err).Msg("Failed to start process")
		return "", err
	}

	status := &providers.ProcessStatus{
		ID:                 processIDStr,
		Status:             "running",
		StartTime:          time.Now(),
		ProvidersProcessed: providersToProcess,
		ProvidersRemoved:   providersToRemove,
	}

	if err := s.repo.InsertProcess(ctx, status); err != nil {
		pm.FinishProcess(processIDStr, err)
		log.Err(err).Msg("Failed to insert process status")
		return "", ErrInsertProcess
	}

	// Get the providers and run synchronously (this method blocks)
	allProviders := providers.GetProviders()
	processErr := allProviders.Processor(providersToProcess, providersToRemove)

	// Finish the process
	pm.FinishProcess(processIDStr, processErr)

	if processErr != nil {
		status.Status = "failed"
		status.EndTime = time.Now()
		status.Error = processErr.Error()
	} else {
		status.Status = "completed"
		status.EndTime = time.Now()
	}

	if updateErr := s.repo.UpdateProcessStatus(ctx, status); updateErr != nil {
		log.Err(updateErr).
			Str("process_id", processIDStr).
			Msg("Failed to update process status after completion")
	}

	return processIDStr, nil
}

func (s *ProviderProcessService) GetProcessStatus(ctx context.Context, processID string) (*providers.ProcessStatus, error) {
	// First check in-memory process manager for current/recent processes
	pm := providers.GetProcessManager()
	if status, err := pm.GetProcessByID(processID); err == nil {
		return status, nil
	}

	// Fall back to database for older records
	status, err := s.repo.GetProcessByID(ctx, processID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Error().
				Str("process_id", processID).
				Msg("Process not found")
			return nil, ErrProcessNotFound
		}

		log.Err(err).
			Str("process_id", processID).
			Msg("Failed to get process status")
		return nil, ErrGetProcessStatus
	}
	return status, nil
}

func (s *ProviderProcessService) ListProcesses(ctx context.Context) ([]*providers.ProcessStatus, error) {
	// Get processes from database
	dbStatuses, err := s.repo.ListProcesses(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to list processes")
		return nil, ErrListProcesses
	}

	// Create a map of DB process IDs for quick lookup
	dbProcessIDs := make(map[string]bool)
	for _, s := range dbStatuses {
		dbProcessIDs[s.ID] = true
	}

	// Get in-memory processes (current + recent history) from ProcessManager
	pm := providers.GetProcessManager()
	inMemoryProcesses := pm.GetRecentProcesses(50) // Get up to 50 recent processes

	// Initialize result as empty slice (not nil) so JSON returns [] instead of null
	result := make([]*providers.ProcessStatus, 0)

	// Merge: add in-memory processes that aren't already in DB
	for _, p := range inMemoryProcesses {
		if !dbProcessIDs[p.ID] {
			result = append(result, p)
		}
	}

	// Append DB processes
	result = append(result, dbStatuses...)

	return result, nil
}

func (s *ProviderProcessService) IsProcessRunning(ctx context.Context) (bool, error) {
	// Use the centralized process manager for real-time status
	pm := providers.GetProcessManager()
	return pm.IsRunning(), nil
}
