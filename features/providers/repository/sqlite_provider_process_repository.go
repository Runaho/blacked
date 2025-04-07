package repository

import (
	"blacked/features/providers"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
)

// Repository error variables
var (
	ErrInsertProcess       = errors.New("failed to insert process status")
	ErrUpdateProcess       = errors.New("failed to update process status")
	ErrQueryProcesses      = errors.New("failed to query processes")
	ErrScanProcess         = errors.New("failed to scan process row")
	ErrIterateProcessRows  = errors.New("error iterating process rows")
	ErrFetchProcessRunning = errors.New("failed to fetch start time of running process")
)

// SQLiteProviderProcessRepository is the concrete implementation of ProviderProcessRepository using SQLite.
type SQLiteProviderProcessRepository struct {
	db *sql.DB
}

// NewSQLiteProviderProcessRepository creates a new SQLiteProviderProcessRepository instance.
func NewSQLiteProviderProcessRepository(db *sql.DB) *SQLiteProviderProcessRepository {
	return &SQLiteProviderProcessRepository{db: db}
}

func (r *SQLiteProviderProcessRepository) InsertProcess(ctx context.Context, status *providers.ProcessStatus) error {
	providersProcessedJSON, _ := json.Marshal(status.ProvidersProcessed)
	providersRemovedJSON, _ := json.Marshal(status.ProvidersRemoved)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO provider_processes (
			id, status, start_time, end_time, providers_processed, providers_removed, error
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, status.ID, status.Status, status.StartTime, status.EndTime, providersProcessedJSON, providersRemovedJSON, status.Error)
	if err != nil {
		return ErrInsertProcess
	}
	return nil
}

func (r *SQLiteProviderProcessRepository) UpdateProcessStatus(ctx context.Context, status *providers.ProcessStatus) error {
	providersProcessedJSON, _ := json.Marshal(status.ProvidersProcessed)
	providersRemovedJSON, _ := json.Marshal(status.ProvidersRemoved)

	_, err := r.db.ExecContext(ctx, `
		UPDATE provider_processes
		SET status = ?, end_time = ?, providers_processed = ?, providers_removed = ?, error = ?
		WHERE id = ?
	`, status.Status, status.EndTime, providersProcessedJSON, providersRemovedJSON, status.Error, status.ID)
	if err != nil {
		return ErrUpdateProcess
	}
	return nil
}

func (r *SQLiteProviderProcessRepository) GetProcessByID(ctx context.Context, processID string) (*providers.ProcessStatus, error) {
	row := r.db.QueryRowContext(ctx, "SELECT * FROM provider_processes WHERE id = ?", processID)
	status := &providers.ProcessStatus{}
	var providersProcessedJSON []byte
	var providersRemovedJSON []byte

	err := row.Scan(
		&status.ID, &status.Status, &status.StartTime, &status.EndTime, &providersProcessedJSON, &providersRemovedJSON, &status.Error,
	)
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(providersProcessedJSON, &status.ProvidersProcessed)
	_ = json.Unmarshal(providersRemovedJSON, &status.ProvidersRemoved)

	return status, nil
}

func (r *SQLiteProviderProcessRepository) ListProcesses(ctx context.Context) ([]*providers.ProcessStatus, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM provider_processes ORDER BY start_time DESC")
	if err != nil {
		return nil, ErrQueryProcesses
	}
	defer rows.Close()

	var statuses []*providers.ProcessStatus
	for rows.Next() {
		status := &providers.ProcessStatus{}
		var providersProcessedJSON []byte
		var providersRemovedJSON []byte
		err := rows.Scan(
			&status.ID, &status.Status, &status.StartTime, &status.EndTime, &providersProcessedJSON, &providersRemovedJSON, &status.Error,
		)
		if err != nil {
			return nil, ErrScanProcess
		}
		_ = json.Unmarshal(providersProcessedJSON, &status.ProvidersProcessed)
		_ = json.Unmarshal(providersRemovedJSON, &status.ProvidersRemoved)
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrIterateProcessRows
	}
	return statuses, nil
}

func (r *SQLiteProviderProcessRepository) IsProcessRunning(ctx context.Context, processDeadlineDuration time.Duration) (bool, error) {
	var startTime time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT start_time
		FROM provider_processes
		WHERE status = ?
		ORDER BY start_time DESC
		LIMIT 1
	`, "running").Scan(&startTime) // Get the start_time of the latest 'running' process

	if err != nil {
		if err == sql.ErrNoRows { // No running process found
			return false, nil
		}
		return false, ErrFetchProcessRunning
	}

	deadline := startTime.Add(processDeadlineDuration)
	log.Info().
		Time("deadline", deadline).
		Dur("duration", processDeadlineDuration).
		Msg("Process deadline")

	return deadline.After(time.Now()), nil // Check if deadline is in the future
}
