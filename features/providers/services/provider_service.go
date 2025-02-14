package services

import (
	"blacked/features/providers"
	"blacked/features/providers/repository"
	"blacked/internal/db"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
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
	dbConn, err := db.GetDB() // Using read-only connection for now, might need RW later
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return &ProviderProcessService{
		repo:                    repository.NewSQLiteProviderProcessRepository(dbConn),
		processDeadlineDuration: 5 * time.Minute,
	}, nil
}

func (s *ProviderProcessService) StartProcess(ctx context.Context, providersToProcess []string, providersToRemove []string) (processID string, err error) {
	isRunning, err := s.IsProcessRunning(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to check if process is running: %w", err)
	}
	if isRunning {
		return "", fmt.Errorf("another process is already running")
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
		return "", fmt.Errorf("failed to insert process status: %w", err)
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
		if updateErr := s.repo.UpdateProcessStatus(context.Background(), status); updateErr != nil { // Use backgroundCtx here
			log.Error().Err(updateErr).Str("process_id", processIDStr).Msg("Failed to update process status after completion")
		}
	}()

	return processIDStr, nil
}

func (s *ProviderProcessService) StartProcessAsync(ctx context.Context, providersToProcess []string, providersToRemove []string) (processID string, err error) {
	isRunning, err := s.IsProcessRunning(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to check if process is running: %w", err)
	}
	if isRunning {
		return "", fmt.Errorf("another process is already running")
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
		return "", fmt.Errorf("failed to insert process status: %w", err)
	}

	err = providers.GetProviders().Processor(providersToProcess, providersToRemove)
	if err != nil {
		status.Status = "failed"
		status.EndTime = time.Now()
		status.Error = err.Error()
	} else {
		status.Status = "completed"
		status.EndTime = time.Now()
	}
	if updateErr := s.repo.UpdateProcessStatus(context.Background(), status); updateErr != nil { // Use backgroundCtx here
		log.Error().Err(updateErr).Str("process_id", processIDStr).Msg("Failed to update process status after completion")
	}

	return processIDStr, nil
}

func (s *ProviderProcessService) GetProcessStatus(ctx context.Context, processID string) (*providers.ProcessStatus, error) {
	status, err := s.repo.GetProcessByID(ctx, processID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("process not found: %s", processID)
		}
		return nil, fmt.Errorf("failed to get process status: %w", err)
	}
	return status, nil
}

func (s *ProviderProcessService) ListProcesses(ctx context.Context) ([]*providers.ProcessStatus, error) {
	statuses, err := s.repo.ListProcesses(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}
	return statuses, nil
}

func (s *ProviderProcessService) IsProcessRunning(ctx context.Context) (bool, error) {
	running, err := s.repo.IsProcessRunning(ctx, s.processDeadlineDuration)
	if err != nil {
		return false, fmt.Errorf("failed to check for running processes: %w", err)
	}
	return running, nil
}

// Convert Providers List to String for DB storage if needed
func providersListToString(providers []string) string {
	if len(providers) == 0 {
		return ""
	}
	jsonData, _ := json.Marshal(providers) // Ignoring error for simplicity, handle as needed
	return string(jsonData)
}

// Convert String from DB to Providers List
func stringToProvidersList(providerStr string) []string {
	if providerStr == "" {
		return nil
	}
	var providers []string
	_ = json.Unmarshal([]byte(providerStr), &providers) // Ignoring error for simplicity, handle as needed
	return providers
}
