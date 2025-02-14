package repository

import (
	"blacked/features/providers"
	"context"
	"time"
)

// ProviderProcessRepository interface defines data access methods for provider processes.
type ProviderProcessRepository interface {
	InsertProcess(ctx context.Context, status *providers.ProcessStatus) error
	UpdateProcessStatus(ctx context.Context, status *providers.ProcessStatus) error
	GetProcessByID(ctx context.Context, processID string) (*providers.ProcessStatus, error)
	ListProcesses(ctx context.Context) ([]*providers.ProcessStatus, error)
	IsProcessRunning(ctx context.Context, processDeadlineDuration time.Duration) (bool, error)
}
