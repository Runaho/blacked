package repository

import (
	"blacked/features/entries"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DuckDBRepository is the concrete implementation of BlacklistRepository using DuckDB.
type DuckDBRepository struct {
	db *sql.DB
}

// NewDuckDBRepository creates a new DuckDBRepository instance.
func NewDuckDBRepository(db *sql.DB) *DuckDBRepository {
	return &DuckDBRepository{db: db}
}

// GetAllEntries retrieves all active blacklist entries from DuckDB.
func (r *DuckDBRepository) GetAllEntries(ctx context.Context) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE deleted_at IS NULL") // WHERE clause to filter out deleted entries
	if err != nil {
		return nil, fmt.Errorf("failed to query all active entries from DuckDB: %w", err)
	}
	defer rows.Close()

	var _entries []entries.Entry = []entries.Entry{} // Initialize to empty slice if no active entries
	for rows.Next() {
		var entry entries.Entry
		var subDomainsStr string
		var deletedAt sql.NullTime // Use sql.NullTime for nullable DATETIME in DB
		err := rows.Scan(
			&entry.ID, &entry.Scheme, &entry.Domain, &entry.Host, &subDomainsStr,
			&entry.Path, &entry.RawQuery, &entry.SourceURL, &entry.Source, &entry.Category,
			&entry.Confidence, &entry.CreatedAt, &entry.UpdatedAt, &deletedAt, // Scan deletedAt
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row from DuckDB: %w", err)
		}
		entry.SubDomains = strings.Split(subDomainsStr, ",")
		if entry.SubDomains[0] == "" {
			entry.SubDomains = nil
		}
		if deletedAt.Valid { // Handle potential NULL value from DB
			entry.DeletedAt = &deletedAt.Time // Assign time.Time pointer
		} else {
			entry.DeletedAt = nil // Ensure it's nil if not set in DB
		}
		_entries = append(_entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error from DuckDB: %w", err)
	}
	return _entries, nil
}

// GetEntryByID retrieves a blacklist entry by its ID from DuckDB, even if deleted.
func (r *DuckDBRepository) GetEntryByID(ctx context.Context, id string) (*entries.Entry, error) {
	row := r.db.QueryRowContext(ctx, "SELECT * FROM blacklist_entries WHERE id = ?", id) // No WHERE deleted_at IS NULL here if you want to retrieve deleted entries too
	var entry entries.Entry
	var subDomainsStr string
	var deletedAt sql.NullTime // For nullable DATETIME

	err := row.Scan(
		&entry.ID, &entry.Scheme, &entry.Domain, &entry.Host, &subDomainsStr,
		&entry.Path, &entry.RawQuery, &entry.SourceURL, &entry.Source, &entry.Category,
		&entry.Confidence, &entry.CreatedAt, &entry.UpdatedAt, &deletedAt, // Scan deletedAt
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Entry not found
		}
		return nil, fmt.Errorf("failed to query entry by ID from DuckDB: %w", err)
	}
	entry.SubDomains = strings.Split(subDomainsStr, ",")
	if entry.SubDomains[0] == "" {
		entry.SubDomains = nil
	}
	if deletedAt.Valid {
		entry.DeletedAt = &deletedAt.Time
	} else {
		entry.DeletedAt = nil
	}
	return &entry, nil
}

// GetEntriesBySource retrieves all active blacklist entries for a given source from DuckDB.
func (r *DuckDBRepository) GetEntriesBySource(ctx context.Context, source string) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE source = ? AND deleted_at IS NULL") // Added WHERE deleted_at IS NULL
	if err != nil {
		return nil, fmt.Errorf("failed to query active entries by source from DuckDB: %w", err)
	}
	defer rows.Close()

	var _entries []entries.Entry = []entries.Entry{} // Initialize empty slice
	for rows.Next() {
		var entry entries.Entry
		var subDomainsStr string
		var deletedAt sql.NullTime // For nullable DATETIME
		err := rows.Scan(
			&entry.ID, &entry.Scheme, &entry.Domain, &entry.Host, &subDomainsStr,
			&entry.Path, &entry.RawQuery, &entry.SourceURL, &entry.Source, &entry.Category,
			&entry.Confidence, &entry.CreatedAt, &entry.UpdatedAt, &deletedAt, // Scan deletedAt
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row for entries by source from DuckDB: %w", err)
		}
		entry.SubDomains = strings.Split(subDomainsStr, ",")
		if entry.SubDomains[0] == "" {
			entry.SubDomains = nil
		}
		if deletedAt.Valid {
			entry.DeletedAt = &deletedAt.Time
		} else {
			entry.DeletedAt = nil
		}
		_entries = append(_entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error for entries by source from DuckDB: %w", err)
	}
	return _entries, nil
}

// GetEntriesByCategory retrieves all active blacklist entries for a given category from DuckDB.
func (r *DuckDBRepository) GetEntriesByCategory(ctx context.Context, category string) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE category = ? AND deleted_at IS NULL") // Added WHERE deleted_at IS NULL
	if err != nil {
		return nil, fmt.Errorf("failed to query active entries by category from DuckDB: %w", err)
	}
	defer rows.Close()

	var _entries []entries.Entry = []entries.Entry{} // Initialize empty slice
	for rows.Next() {
		var entry entries.Entry
		var subDomainsStr string
		var deletedAt sql.NullTime // For nullable DATETIME
		err := rows.Scan(
			&entry.ID, &entry.Scheme, &entry.Domain, &entry.Host, &subDomainsStr,
			&entry.Path, &entry.RawQuery, &entry.SourceURL, &entry.Source, &entry.Category,
			&entry.Confidence, &entry.CreatedAt, &entry.UpdatedAt, &deletedAt, // Scan deletedAt
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row for entries by category from DuckDB: %w", err)
		}
		entry.SubDomains = strings.Split(subDomainsStr, ",")
		if entry.SubDomains[0] == "" {
			entry.SubDomains = nil
		}
		if deletedAt.Valid {
			entry.DeletedAt = &deletedAt.Time
		} else {
			entry.DeletedAt = nil
		}
		_entries = append(_entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error for entries by category from DuckDB: %w", err)
	}
	return _entries, nil
}

// SaveEntry performs UPSERT (Insert or Update) for a single entries.Entry.
func (r *DuckDBRepository) SaveEntry(ctx context.Context, entry entries.Entry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for SaveEntry (UPSERT): %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
			INSERT INTO blacklist_entries (
				id, process_id, scheme, domain, host, sub_domains, path, raw_query, source_url, source, category, confidence, created_at, updated_at, deleted_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL) -- Insert with NULL deleted_at for new entries
			ON CONFLICT (source_url, source) DO UPDATE SET -- UPSERT logic on conflict of 'source_url' and 'source'
				process_id = EXCLUDED.process_id,
				scheme = EXCLUDED.scheme,
				domain = EXCLUDED.domain,
				host = EXCLUDED.host,
				sub_domains = EXCLUDED.sub_domains,
				path = EXCLUDED.path,
				raw_query = EXCLUDED.raw_query,
				category = EXCLUDED.category,
				confidence = EXCLUDED.confidence,
				updated_at = EXCLUDED.updated_at, -- Update 'updated_at' on update
				deleted_at = NULL                  -- Ensure entry is NOT deleted upon update (reset soft delete)
			WHERE EXCLUDED.updated_at > blacklist_entries.updated_at -- Optional: Update only if new data is "newer" (based on UpdatedAt)
		`,
		entry.ID, entry.ProcessID, entry.Scheme, entry.Domain, entry.Host, strings.Join(entry.SubDomains, ","),
		entry.Path, entry.RawQuery, entry.SourceURL, entry.Source, entry.Category, entry.Confidence,
		entry.CreatedAt, entry.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to UPSERT entry to DuckDB: %w", err)
	}

	return tx.Commit()
}

// blackLinks/repository.go

// BatchSaveEntries performs a batch UPSERT of multiple BlackListEntry records for performance.
func (r *DuckDBRepository) BatchSaveEntries(ctx context.Context, entries []entries.Entry) error {
	if len(entries) == 0 {
		return nil // Nothing to do if batch is empty
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for BatchSaveEntries: %w", err)
	}
	defer tx.Rollback() // Ensure rollback in case of errors

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO blacklist_entries (
			id, process_id, scheme, domain, host, sub_domains, path, raw_query, source_url, source, category, confidence, created_at, updated_at, deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL) -- Insert with NULL deleted_at initially, now includes process_id
		ON CONFLICT (source_url, source) DO UPDATE SET -- UPSERT logic on conflict of 'source_url' and 'source'
			process_id = EXCLUDED.process_id,
			scheme = EXCLUDED.scheme,
			domain = EXCLUDED.domain,
			host = EXCLUDED.host,
			sub_domains = EXCLUDED.sub_domains,
			path = EXCLUDED.path,
			raw_query = EXCLUDED.raw_query,
			category = EXCLUDED.category,
			confidence = EXCLUDED.confidence,
			updated_at = EXCLUDED.updated_at,
			deleted_at = NULL  -- Reset deleted_at on update to ensure entry becomes active again if it was soft-deleted
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare batch insert statement: %w", err)
	}
	defer stmt.Close() // Ensure statement closure

	for _, entry := range entries {
		_, err := stmt.ExecContext(ctx,
			entry.ID, entry.ProcessID, entry.Scheme, entry.Domain, entry.Host, strings.Join(entry.SubDomains, ","), // Include processID here
			entry.Path, entry.RawQuery, entry.SourceURL, entry.Source, entry.Category, entry.Confidence,
			entry.CreatedAt, entry.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("error executing batch insert for entry ID %s: %w", entry.ID, err)
		}
	}

	return tx.Commit() // Commit the whole batch transaction
}

// RemoveOlderInsertions soft deletes blacklist entries from a provider that do not have the latest insertion ID.
func (r *DuckDBRepository) RemoveOlderInsertions(ctx context.Context, providerName string, currentProcessID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for RemoveOlderInsertions: %w", err)
	}
	defer tx.Rollback()

	currentTime := time.Now()
	result, err := tx.ExecContext(ctx, `
		UPDATE blacklist_entries
		SET deleted_at = ?
		WHERE source = ?
		  AND process_id != ?
		  AND deleted_at IS NULL -- Only update entries that are currently NOT deleted (active)
	`, currentTime, providerName, currentProcessID)

	if err != nil {
		return fmt.Errorf("failed to soft delete older insertions for provider '%s': %w", providerName, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		fmt.Printf("Warning: Failed to get rows affected count during RemoveOlderInsertions for %s: %v\n", providerName, err)
		// Non-critical error, continue without failing the whole process
	} else {
		fmt.Printf("Soft-deleted %d older entries for provider '%s' (insertion IDs older than %s)\n", rowsAffected, providerName, currentProcessID)
	}

	return tx.Commit()
}

// ClearAllEntries performs a SOFT DELETE of all blacklist entries.
func (r *DuckDBRepository) ClearAllEntries(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for ClearAllEntries (SOFT DELETE): %w", err)
	}
	defer tx.Rollback()

	currentTime := time.Now()
	_, err = tx.ExecContext(ctx, "UPDATE blacklist_entries SET deleted_at = ?", currentTime) // Soft delete all by setting deleted_at
	if err != nil {
		return fmt.Errorf("failed to soft delete all entries in DuckDB: %w", err)
	}

	return tx.Commit()
}

// CheckIfHostExists checks if an active entry with the given host exists in the blacklist.
func (r *DuckDBRepository) CheckIfHostExists(ctx context.Context, host string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM blacklist_entries WHERE host = ? AND deleted_at IS NULL)", host).Scan(&exists) // Added WHERE deleted_at IS NULL
	if err != nil {
		return false, fmt.Errorf("failed to check if active host exists in DuckDB: %w", err)
	}
	return exists, nil
}

// SoftDeleteEntryByID soft deletes a entries.Entry by its ID.
func (r *DuckDBRepository) SoftDeleteEntryByID(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for SoftDeleteEntryByID: %w", err)
	}
	defer tx.Rollback()

	currentTime := time.Now()
	_, err = tx.ExecContext(ctx, "UPDATE blacklist_entries SET deleted_at = ? WHERE id = ?", currentTime, id)
	if err != nil {
		return fmt.Errorf("failed to soft delete entry with ID %s in DuckDB: %w", id, err)
	}

	return tx.Commit()
}
