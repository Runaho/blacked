package services

import (
	"blacked/features/providers"
	"blacked/features/providers/repository"
	"blacked/internal/db"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
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
	conn, err := db.GetDB()
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
	isRunning, err := s.IsProcessRunning(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to check if process is running")
		return "", err
	}
	if isRunning {
		log.Info().Msg("Another process is already running")
		return "", ErrProcessRunning
	}

	processUUID := uuid.New()
	processIDStr := processUUID.String()

	status := &providers.ProcessStatus{
		ID:                 processIDStr,
		Status:             "running",
		StartTime:          time.Now(),
		ProvidersProcessed: providersToProcess,
		ProvidersRemoved:   providersToRemove,
	}

	if err := s.repo.InsertProcess(ctx, status); err != nil {
		log.Err(err).Msg("Failed to insert process status")
		return "", ErrInsertProcess
	}

	go func() {
		err := providers.GetProviders().Processor(providersToProcess, providersToRemove)
		if err != nil {
			status.Status = "failed"
			status.EndTime = time.Now()
			status.Error = err.Error()
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
	isRunning, err := s.IsProcessRunning(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to check if process is running")
		return "", err
	}
	if isRunning {
		log.Info().Msg("Another process is already running")
		return "", ErrProcessRunning
	}

	processUUID := uuid.New()
	processIDStr := processUUID.String()

	status := &providers.ProcessStatus{
		ID:                 processIDStr,
		Status:             "running",
		StartTime:          time.Now(),
		ProvidersProcessed: providersToProcess,
		ProvidersRemoved:   providersToRemove,
	}

	if err := s.repo.InsertProcess(ctx, status); err != nil {
		log.Err(err).Msg("Failed to insert process status")
		return "", ErrInsertProcess
	}

	// Get the providers
	allProviders := providers.GetProviders()
	err = allProviders.Processor(providersToProcess, providersToRemove)

	if err != nil {
		status.Status = "failed"
		status.EndTime = time.Now()
		status.Error = err.Error()
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
	statuses, err := s.repo.ListProcesses(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to list processes")
		return nil, ErrListProcesses
	}
	return statuses, nil
}

func (s *ProviderProcessService) IsProcessRunning(ctx context.Context) (bool, error) {
	running, err := s.repo.IsProcessRunning(ctx, s.processDeadlineDuration)
	if err != nil {
		log.Err(err).Msg("Failed to check if process is running")
		return false, ErrCheckRunningProcess
	}
	return running, nil
}
