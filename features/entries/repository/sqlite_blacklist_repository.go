package repository

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/internal/utils"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// SQLiteRepository is the concrete implementation of BlacklistRepository using SQLite.
type SQLiteRepository struct {
	db        *sql.DB
	writeLock sync.Mutex // Add this
}

// NewSQLiteRepository creates a new SQLiteRepository instance.
func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) StreamEntries(ctx context.Context, out chan<- entries.EntryStream) error {
	defer close(out)

	query := `
	SELECT
        source_url,
        GROUP_CONCAT(id, ',') as ids
    FROM
    	blacklist_entries
    WHERE
        deleted_at IS NULL
    GROUP BY
    	source_url
    ORDER BY
        COUNT(*) DESC;
    `

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var sourceURL string
			var idsConcat string

			if err := rows.Scan(&sourceURL, &idsConcat); err != nil {
				return err
			}

			ids := []string{}
			if idsConcat != "" {
				ids = strings.Split(idsConcat, ",")
			}

			out <- entries.EntryStream{
				SourceUrl: sourceURL,
				IDs:       ids,
			}
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return nil
}

// GetAllEntries retrieves all active blacklist entries from SQLite.
func (r *SQLiteRepository) GetAllEntries(ctx context.Context) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE deleted_at IS NULL") // WHERE clause to filter out deleted entries
	if err != nil {
		return nil, fmt.Errorf("failed to query all active entries from SQLite: %w", err)
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
			return nil, fmt.Errorf("failed to scan row from SQLite: %w", err)
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
		return nil, fmt.Errorf("rows iteration error from SQLite: %w", err)
	}
	return _entries, nil
}

// GetEntryByID retrieves a blacklist entry by its ID from SQLite, even if deleted.
func (r *SQLiteRepository) GetEntryByID(ctx context.Context, id string) (*entries.Entry, error) {
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
		return nil, fmt.Errorf("failed to query entry by ID from SQLite: %w", err)
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

func (r *SQLiteRepository) GetEntriesByIDs(ctx context.Context, ids []string) ([]*entries.Entry, error) {
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
		return nil, fmt.Errorf("failed to query entries by IDs from SQLite: %w", err)
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
			log.Error().Err(err).Msg("Failed to scan row from SQLite")
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
		return nil, fmt.Errorf("rows iteration error from SQLite: %w", err)
	}

	return entriesList, nil
}

// GetEntriesBySource retrieves all active blacklist entries for a given source from SQLite.
func (r *SQLiteRepository) GetEntriesBySource(ctx context.Context, source string) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE source = ? AND deleted_at IS NULL") // Added WHERE deleted_at IS NULL
	if err != nil {
		return nil, fmt.Errorf("failed to query active entries by source from SQLite: %w", err)
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
			return nil, fmt.Errorf("failed to scan row for entries by source from SQLite: %w", err)
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
		return nil, fmt.Errorf("rows iteration error for entries by source from SQLite: %w", err)
	}
	return _entries, nil
}

// GetEntriesByCategory retrieves all active blacklist entries for a given category from SQLite.
func (r *SQLiteRepository) GetEntriesByCategory(ctx context.Context, category string) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE category = ? AND deleted_at IS NULL") // Added WHERE deleted_at IS NULL
	if err != nil {
		return nil, fmt.Errorf("failed to query active entries by category from SQLite: %w", err)
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
			return nil, fmt.Errorf("failed to scan row for entries by category from SQLite: %w", err)
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
		return nil, fmt.Errorf("rows iteration error for entries by category from SQLite: %w", err)
	}
	return _entries, nil
}

// SaveEntry performs UPSERT (Insert or Update) for a single entries.Entry.
func (r *SQLiteRepository) SaveEntry(ctx context.Context, entry entries.Entry) error {
	r.writeLock.Lock()
	defer r.writeLock.Unlock()

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
		// Consider logging the specific entry details here if needed
		log.Error().Err(err).Str("entry_id", entry.ID).Str("source_url", entry.SourceURL).Msg("Failed to UPSERT entry")
		return err
	}
	return tx.Commit()
}

// blackLinks/repository.go

// BatchSaveEntries performs a batch UPSERT of multiple BlackListEntry records for performance.
func (r *SQLiteRepository) BatchSaveEntries(ctx context.Context, entries []entries.Entry) error {
	if len(entries) == 0 {
		return nil // Nothing to do if batch is empty
	}
	r.writeLock.Lock()
	defer r.writeLock.Unlock()

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
			log.Error().Err(err).Str("entry_id", entry.ID).Str("source_url", entry.SourceURL).Msg("Error executing batch statement for entry")
			return err
		}
	}

	return tx.Commit() // Commit the whole batch transaction
}

// RemoveOlderInsertions soft deletes blacklist entries from a provider that do not have the latest insertion ID.
func (r *SQLiteRepository) RemoveOlderInsertions(ctx context.Context, providerName string, currentProcessID string) error {
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
func (r *SQLiteRepository) ClearAllEntries(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for ClearAllEntries (SOFT DELETE): %w", err)
	}
	defer tx.Rollback()

	currentTime := time.Now()
	_, err = tx.ExecContext(ctx, "UPDATE blacklist_entries SET deleted_at = ?", currentTime) // Soft delete all by setting deleted_at
	if err != nil {
		return fmt.Errorf("failed to soft delete all entries in SQLite: %w", err)
	}

	return tx.Commit()
}

// SoftDeleteEntryByID soft deletes a entries.Entry by its ID.
func (r *SQLiteRepository) SoftDeleteEntryByID(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for SoftDeleteEntryByID: %w", err)
	}
	defer tx.Rollback()

	currentTime := time.Now()
	_, err = tx.ExecContext(ctx, "UPDATE blacklist_entries SET deleted_at = ? WHERE id = ?", currentTime, id)
	if err != nil {
		return fmt.Errorf("failed to soft delete entry with ID %s in SQLite: %w", id, err)
	}

	return tx.Commit()
}

func (r *SQLiteRepository) QueryLink(ctx context.Context, link string) (
	hits []entries.Hit,
	err error) {

	normalizedLink := utils.NormalizeURL(link)
	parsedURL, parseErr := url.Parse(normalizedLink)
	if parseErr != nil {
		// --- URL Parsing Failed ---
		log.Warn().Err(parseErr).Str("raw_link", link).Msg("Failed to parse input URL, attempting exact match query only")
		hits = append(hits, r.queryExactURLMatch(ctx, normalizedLink)...)
		return hits, nil
	}

	host := parsedURL.Hostname()
	domain := ""

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

// QueryLinkByType queries blacklist entries based on URL criteria and query type.  If queryType is nil, it defaults to a full URL query.
func (r *SQLiteRepository) QueryLinkByType(ctx context.Context, link string, queryType *enums.QueryType) (
	hits []entries.Hit,
	err error) {
	if queryType == nil || *queryType == enums.QueryTypeMixed {
		return r.QueryLink(ctx, link)
	}

	startTime := time.Now()
	var query string

	switch *queryType {
	case enums.QueryTypeFull:
		query = "SELECT id FROM blacklist_entries WHERE source_url = ? AND deleted_at IS NULL"
	case enums.QueryTypeHost:
		query = "SELECT id FROM blacklist_entries WHERE host = ? AND deleted_at IS NULL"
	case enums.QueryTypeDomain:
		query = "SELECT id FROM blacklist_entries WHERE domain = ? AND deleted_at IS NULL"
	case enums.QueryTypePath:
		query = "SELECT id FROM blacklist_entries WHERE path = ? AND deleted_at IS NULL"
	default:
		return nil, fmt.Errorf("invalid query type: %v", queryType)
	}
	log.Debug().Str("query", query).Str("type", queryType.String()).Msg("starting query")
	rows, err := r.db.QueryContext(ctx, query, link)
	if err != nil {
		log.Error().Err(err).Msg("Query failed")
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan row")
			return nil, err
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    queryType.String(),
			MatchedValue: link,
		})
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating rows")
		return nil, err
	}

	log.Debug().Dur("duration", time.Since(startTime)).Str("query_type", queryType.String()).Msg("Query completed")

	return hits, nil
}

func (r *SQLiteRepository) queryExactURLMatch(ctx context.Context, normalizedLink string) []entries.Hit {
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

func (r *SQLiteRepository) queryHostMatch(ctx context.Context, host string) []entries.Hit {
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

func (r *SQLiteRepository) queryDomainMatch(ctx context.Context, domain string) []entries.Hit {
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

func (r *SQLiteRepository) queryPathMatch(ctx context.Context, path string) []entries.Hit {
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
