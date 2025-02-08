package repository

import (
	"blacked/features/entries"
	"blacked/internal/utils"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
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
		&entry.ID, &entry.ProcessID, &entry.Scheme, &entry.Domain, &entry.Host, &subDomainsStr,
		&entry.Path, &entry.RawQuery, &entry.SourceURL, &entry.Source, &entry.Category,
		&entry.Confidence, &entry.CreatedAt, &entry.UpdatedAt, &deletedAt, // Scan deletedAt
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Entry not found
		}
		return nil, fmt.Errorf("failed to query entry by ID from DuckDB: %w", err)
	}
	entry.SubDomains = strings.Split(subDomainsStr, ",") // Split the string
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

func (r *DuckDBRepository) GetEntriesByIDs(ctx context.Context, ids []string) ([]*entries.Entry, error) {
	if len(ids) == 0 {
		return []*entries.Entry{}, nil // Return empty slice if no IDs provided
	}

	// Construct the query with a WHERE id IN (...) clause
	query := `
		SELECT id, process_id, scheme, domain, host, sub_domains, path, raw_query, source_url, source, category, confidence, created_at, updated_at, deleted_at
		FROM blacklist_entries
		WHERE id IN (` + strings.Join(strings.Split(strings.Repeat("?", len(ids)), ""), ", ") + `)` // Generate placeholders
	// AND deleted_at IS NULL -- If you only want active entries

	// Convert the slice of IDs to a slice of interfaces for the query
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query entries by IDs from DuckDB: %w", err)
	}
	defer rows.Close()

	var entriesList []*entries.Entry
	for rows.Next() {
		var entry entries.Entry
		var subDomainsStr string
		var deletedAt sql.NullTime // For nullable DATETIME

		err := rows.Scan(
			&entry.ID, &entry.ProcessID, &entry.Scheme, &entry.Domain, &entry.Host, &subDomainsStr,
			&entry.Path, &entry.RawQuery, &entry.SourceURL, &entry.Source, &entry.Category,
			&entry.Confidence, &entry.CreatedAt, &entry.UpdatedAt, &deletedAt, // Scan deletedAt
		)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan row from DuckDB")
			continue // Skip to the next row
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
		entriesList = append(entriesList, &entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error from DuckDB: %w", err)
	}

	return entriesList, nil
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
		log.Debug().Interface("entry", entry).Msg("About to insert entry") // Print the entry data

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

func (r *DuckDBRepository) QueryLink(ctx context.Context, link string) (
	hits []entries.Hit,
	err error) {
	parsedURL, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	normalizedLink := utils.NormalizeURL(link)
	host := parsedURL.Hostname()
	domain := "" // Extract domain here.  You'll need to use your extractDomain function.

	d, _, err := utils.ExtractDomainAndSubDomains(parsedURL.Host)

	if err != nil {
		log.Error().Err(err).Msg("error extracting domain and subdomains")
	} else {
		domain = d
	}
	path := parsedURL.Path

	// Exact URL match
	hits = append(hits, r.queryExactURLMatch(ctx, normalizedLink)...)

	// Host match
	hits = append(hits, r.queryHostMatch(ctx, host)...)

	// Domain match
	hits = append(hits, r.queryDomainMatch(ctx, domain)...)

	// Path match
	// query if path is empty or "/" and skip if so
	// TODO Make path match optional or configurable
	if path != "" && len(path) != 0 && path != "/" {
		hits = append(hits, r.queryPathMatch(ctx, path)...)
	}

	return hits, nil
}

func (r *DuckDBRepository) queryExactURLMatch(ctx context.Context, normalizedLink string) []entries.Hit {
	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE source_url = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, normalizedLink)
	if err != nil {
		log.Error().Err(err).Msg("Exact URL match query failed")
		return nil
	}
	defer rows.Close()

	var hits []entries.Hit

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan row in queryExactURLMatch")
			continue // Or handle the error as appropriate
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    "EXACT_URL",
			MatchedValue: normalizedLink,
		})
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating rows in queryExactURLMatch")
		return nil
	}

	duration := time.Since(startTime)
	log.Debug().Dur("duration", duration).Str("match_type", "EXACT_URL").Msg("Exact URL match query completed")

	return hits
}

func (r *DuckDBRepository) queryHostMatch(ctx context.Context, host string) []entries.Hit {
	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE host = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, host)
	if err != nil {
		log.Error().Err(err).Msg("Host match query failed")
		return nil
	}
	defer rows.Close()

	var hits []entries.Hit

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan row in queryHostMatch")
			continue // Or handle the error as appropriate
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    "HOST",
			MatchedValue: host,
		})
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating rows in queryHostMatch")
		return nil
	}

	duration := time.Since(startTime)
	log.Debug().Dur("duration", duration).Str("match_type", "HOST").Msg("Host match query completed")

	return hits
}

func (r *DuckDBRepository) queryDomainMatch(ctx context.Context, domain string) []entries.Hit {
	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE domain = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, domain)
	if err != nil {
		log.Error().Err(err).Msg("Domain match query failed")
		return nil
	}
	defer rows.Close()

	var hits []entries.Hit

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan row in queryDomainMatch")
			continue // Or handle the error as appropriate
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    "DOMAIN",
			MatchedValue: domain,
		})
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating rows in queryDomainMatch")
		return nil
	}

	log.Debug().Dur("duration", time.Since(startTime)).Str("match_type", "DOMAIN").Msg("Domain match query completed")

	return hits
}

func (r *DuckDBRepository) queryPathMatch(ctx context.Context, path string) []entries.Hit {
	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE path = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, path)
	if err != nil {
		log.Error().Err(err).Msg("Path match query failed")
		return nil
	}
	defer rows.Close()

	var hits []entries.Hit

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan row in queryPathMatch")
			continue // Or handle the error as appropriate
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    "PATH",
			MatchedValue: path,
		})
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating rows in queryPathMatch")
		return nil
	}

	log.Debug().Dur("duration", time.Since(startTime)).Str("match_type", "PATH").Msg("Path match query completed")

	return hits
}
