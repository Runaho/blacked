package repository

import (
	"blacked/features/providers"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
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
		return fmt.Errorf("failed to insert process status: %w", err)
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
		return fmt.Errorf("failed to update process status: %w", err)
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
		return nil, fmt.Errorf("failed to query processes: %w", err)
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
			return nil, fmt.Errorf("failed to scan process row: %w", err)
		}
		_ = json.Unmarshal(providersProcessedJSON, &status.ProvidersProcessed)
		_ = json.Unmarshal(providersRemovedJSON, &status.ProvidersRemoved)
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating process rows: %w", err)
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
		return false, fmt.Errorf("failed to fetch start time of running process: %w", err)
	}

	deadline := startTime.Add(processDeadlineDuration)
	log.Info().
		Time("deadline", deadline).
		Dur("duration", processDeadlineDuration).
		Msg("Process deadline")

	return deadline.After(time.Now()), nil // Check if deadline is in the future
}
