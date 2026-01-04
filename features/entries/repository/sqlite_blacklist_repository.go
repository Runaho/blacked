package repository

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/internal/utils"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrInvalidEntryQueryType = errors.New("invalid entry query type")
	ErrQueryAllEntries       = errors.New("failed to query all active entries from SQLite")

	ErrToScan        = errors.New("failed to scan row from SQLite")
	ErrToQuery       = errors.New("failed to query entry by ID from SQLite")
	ErrRowsIteration = errors.New("error iterating rows from SQLite")
	ErrTx            = errors.New("failed to begin transaction for SQLite")
	ErrTxPrepare     = errors.New("failed to prepare transaction for SQLite")
	ErrUpsert        = errors.New("failed to UPSERT entry in SQLite")
	ErrDelete        = errors.New("failed to delete entry in SQLite")
)

// SQLiteRepository is the concrete implementation of BlacklistRepository using SQLite.
type SQLiteRepository struct {
	db *sql.DB
	// writeLock removed - no longer needed with single-threaded writer
}

// NewSQLiteRepository creates a new SQLiteRepository instance.
func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) StreamEntriesCount(ctx context.Context) (int, error) {
	query := `
	SELECT
        COUNT(DISTINCT source_url)
    FROM
        blacklist_entries
    WHERE
        deleted_at IS NULL;
	`

	row := r.db.QueryRowContext(ctx, query)
	var count int
	if err := row.Scan(&count); err != nil {
		log.Error().Err(err).Msg("Failed to count entries in SQLite")
		return 0, err
	}
	return count, nil
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
				IDsRaw:    idsConcat,
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
		log.Error().Err(err).Msg("Failed to query all active entries from SQLite")
		return nil, ErrQueryAllEntries
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
			log.Error().Err(err).Msg("Failed to scan row from SQLite")
			return nil, ErrToScan
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
		log.Error().Err(err).Msg("Error iterating rows from SQLite")
		return nil, ErrRowsIteration
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

		log.Err(err).
			Str("entry_id", id).
			Msg("Failed to scan row from SQLite")

		return nil, ErrToQuery
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
		log.Err(err).
			Strs("ids", ids).
			Msg("Failed to query entries by IDs from SQLite")

		return nil, ErrToQuery
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
		log.Err(err).Msg("Error iterating rows from SQLite")
		return nil, ErrRowsIteration
	}

	return entriesList, nil
}

// GetEntriesBySource retrieves all active blacklist entries for a given source from SQLite.
func (r *SQLiteRepository) GetEntriesBySource(ctx context.Context, source string) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE source = ? AND deleted_at IS NULL") // Added WHERE deleted_at IS NULL
	if err != nil {
		log.Err(err).
			Str("source", source).
			Msg("Failed to query active entries by source from SQLite")

		return nil, ErrToQuery
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
			log.Err(err).
				Str("source", source).
				Msg("Failed to scan row for entries by source from SQLite")

			return nil, ErrToScan
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
		log.Err(err).
			Str("source", source).
			Msg("Rows iteration error for entries by source from SQLite")

		return nil, ErrRowsIteration
	}
	return _entries, nil
}

// GetEntriesByCategory retrieves all active blacklist entries for a given category from SQLite.
func (r *SQLiteRepository) GetEntriesByCategory(ctx context.Context, category string) ([]entries.Entry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT * FROM blacklist_entries WHERE category = ? AND deleted_at IS NULL") // Added WHERE deleted_at IS NULL
	if err != nil {
		log.Err(err).
			Str("category", category).
			Msg("Failed to query active entries by category from SQLite")

		return nil, ErrToQuery
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
			log.Err(err).
				Str("category", category).
				Msg("Failed to scan row for entries by category from SQLite")

			return nil, ErrToScan
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
		log.Err(err).
			Str("category", category).
			Msg("Rows iteration error for entries by category from SQLite")

		return nil, ErrRowsIteration
	}
	return _entries, nil
}

// SaveEntry performs UPSERT (Insert or Update) for a single entries.Entry.
func (r *SQLiteRepository) SaveEntry(ctx context.Context, entry entries.Entry) error {
	// No lock needed - single-threaded writer ensures no concurrent access

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin transaction for SaveEntry (UPSERT)")
		return ErrTx
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
		return ErrUpsert
	}
	return tx.Commit()
}

// blackLinks/repository.go
// BatchSaveEntries performs a batch UPSERT of multiple BlackListEntry records for performance.
func (r *SQLiteRepository) BatchSaveEntries(ctx context.Context, entries []*entries.Entry) error {
	tracer := otel.Tracer("blacked/repository")
	ctx, span := tracer.Start(ctx, "repository.batch_save",
		trace.WithAttributes(
			attribute.Int("batch_size", len(entries)),
		),
	)
	defer span.End()

	if len(entries) == 0 {
		return nil
	}

	// No lock needed - single-threaded writer ensures no concurrent access

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin transaction for BatchSaveEntries")
		return ErrTx
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO blacklist_entries (
            id, process_id, scheme, domain, host, sub_domains, path, raw_query, source_url, source, category, confidence, created_at, updated_at, deleted_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
        ON CONFLICT (source_url, source) DO UPDATE SET
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
            deleted_at = NULL
    `)
	if err != nil {
		log.Err(err).Msg("Failed to prepare batch insert statement")
		return ErrTxPrepare
	}
	defer stmt.Close()

	var subDomainsBuilder strings.Builder

	for _, entry := range entries {
		subDomainsBuilder.Reset()
		for i, sub := range entry.SubDomains {
			if i > 0 {
				subDomainsBuilder.WriteString(",")
			}
			subDomainsBuilder.WriteString(sub)
		}
		subDomainsStr := subDomainsBuilder.String()

		_, err := stmt.ExecContext(ctx,
			entry.ID, entry.ProcessID, entry.Scheme, entry.Domain, entry.Host, subDomainsStr,
			entry.Path, entry.RawQuery, entry.SourceURL, entry.Source, entry.Category, entry.Confidence,
			entry.CreatedAt, entry.UpdatedAt,
		)
		if err != nil {
			log.Error().Err(err).Str("entry_id", entry.ID).Str("source_url", entry.SourceURL).Msg("Error executing batch statement for entry")
			return err
		}
	}

	return tx.Commit()
}

// RemoveOlderInsertions soft deletes blacklist entries from a provider that do not have the latest insertion ID.
func (r *SQLiteRepository) RemoveOlderInsertions(ctx context.Context, providerName string, currentProcessID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Err(err).
			Str("provider", providerName).
			Msg("Failed to begin transaction for RemoveOlderInsertions")

		return ErrTx
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
		log.Error().Err(err).Str("provider", providerName).Msg("Failed to soft delete older insertions")
		return ErrDelete
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Warn().Err(err).Str("provider", providerName).Msg("Failed to get rows affected count during RemoveOlderInsertions")
	} else {
		log.Debug().Int64("rows_affected", rowsAffected).Str("provider", providerName).Msg("Rows affected during RemoveOlderInsertions")
	}

	return tx.Commit()
}

// ClearAllEntries performs a SOFT DELETE of all blacklist entries.
func (r *SQLiteRepository) ClearAllEntries(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Err(err).Msg("Failed to begin transaction for ClearAllEntries (SOFT DELETE)")
		return ErrTx
	}
	defer tx.Rollback()

	currentTime := time.Now()
	_, err = tx.ExecContext(ctx, "UPDATE blacklist_entries SET deleted_at = ?", currentTime) // Soft delete all by setting deleted_at
	if err != nil {
		log.Err(err).Msg("Failed to soft delete all entries in SQLite")
		return ErrDelete
	}

	return tx.Commit()
}

// SoftDeleteEntryByID soft deletes a entries.Entry by its ID.
func (r *SQLiteRepository) SoftDeleteEntryByID(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		log.Err(err).
			Str("entry_id", id).
			Msg("Failed to begin transaction for SoftDeleteEntryByID")

		return ErrTx
	}
	defer tx.Rollback()

	currentTime := time.Now()
	_, err = tx.ExecContext(ctx, "UPDATE blacklist_entries SET deleted_at = ? WHERE id = ?", currentTime, id)
	if err != nil {
		log.Err(err).
			Str("entry_id", id).
			Msg("Failed to soft delete entry by ID in SQLite")

		return ErrDelete
	}

	return tx.Commit()
}

func (r *SQLiteRepository) QueryLink(ctx context.Context, link string) (
	hits []entries.Hit,
	err error) {
	tracer := otel.Tracer("blacked/repository")
	ctx, span := tracer.Start(ctx, "repository.query_link",
		trace.WithAttributes(
			attribute.String("query.link", link),
		),
	)
	defer span.End()

	normalizedLink := utils.NormalizeURL(link)
	parsedURL, parseErr := url.Parse(normalizedLink)
	if parseErr != nil {
		// --- URL Parsing Failed ---
		log.Warn().Err(parseErr).Str("raw_link", link).Msg("Failed to parse input URL, attempting exact match query only")
		hits = append(hits, r.QueryExactURLMatch(ctx, normalizedLink)...)
		return hits, nil
	}

	host := parsedURL.Hostname()
	domain := ""

	d, _, err := utils.ExtractDomainAndSubDomains(parsedURL.Host)

	if err != nil {
		log.Err(err).
			Str("raw_link", link).
			Str("parsed_host", parsedURL.Host).
			Msg("error extracting domain and subdomains")
	} else {
		domain = d
	}
	path := parsedURL.Path

	// Exact URL match
	hits = append(hits, r.QueryExactURLMatch(ctx, normalizedLink)...)

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
		log.Error().Str("query_type", queryType.String()).Msg("Invalid query type")
		return nil, ErrInvalidEntryQueryType
	}
	log.Debug().Str("query", query).Str("type", queryType.String()).Msg("starting query")
	rows, err := r.db.QueryContext(ctx, query, link)
	if err != nil {
		log.Err(err).
			Str("query", query).
			Str("type", queryType.String()).
			Str("link", link).
			Msg("Query failed")

		return nil, ErrToQuery
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Err(err).
				Msg("Failed to scan row")

			return nil, ErrToScan
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    queryType.String(),
			MatchedValue: link,
		})
	}

	if err := rows.Err(); err != nil {
		log.Err(err).
			Msg("Error iterating rows")

		return nil, ErrRowsIteration
	}

	log.Debug().Dur("duration", time.Since(startTime)).Str("query_type", queryType.String()).Msg("Query completed")

	return hits, nil
}

func (r *SQLiteRepository) QueryExactURLMatch(ctx context.Context, normalizedLink string) []entries.Hit {
	tracer := otel.Tracer("blacked/repository")
	_, span := tracer.Start(ctx, "repository.query_exact_url",
		trace.WithAttributes(
			attribute.String("query.url", normalizedLink),
		),
	)
	defer span.End()

	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE source_url = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, normalizedLink)
	if err != nil {
		log.Err(err).Msg("Exact URL match query failed")
		if err == sql.ErrNoRows {
			log.Debug().Str("normalized_link", normalizedLink).Msg("No exact URL match found")
		} else {
			log.Err(err).Msg("Error executing exact URL match query")
		}

		return nil
	}
	defer rows.Close()

	var hits []entries.Hit

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Err(err).Msg("Failed to scan row in queryExactURLMatch")
			continue // Or handle the error as appropriate
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    "EXACT_URL",
			MatchedValue: normalizedLink,
		})
	}

	if err := rows.Err(); err != nil {
		log.Err(err).Msg("Error iterating rows in queryExactURLMatch")
		return nil
	}

	duration := time.Since(startTime)
	log.Debug().Dur("duration", duration).Str("match_type", "EXACT_URL").Msg("Exact URL match query completed")

	return hits
}

func (r *SQLiteRepository) queryHostMatch(ctx context.Context, host string) []entries.Hit {
	tracer := otel.Tracer("blacked/repository")
	_, span := tracer.Start(ctx, "repository.query_host",
		trace.WithAttributes(
			attribute.String("query.host", host),
		),
	)
	defer span.End()

	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE host = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, host)
	if err != nil {
		log.Err(err).
			Str("host", host).
			Msg("Host match query failed")

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
		log.Err(err).
			Str("host", host).
			Msg("Error iterating rows in queryHostMatch")

		return nil
	}

	duration := time.Since(startTime)
	log.Debug().Dur("duration", duration).Str("match_type", "HOST").Msg("Host match query completed")

	return hits
}

func (r *SQLiteRepository) queryDomainMatch(ctx context.Context, domain string) []entries.Hit {
	tracer := otel.Tracer("blacked/repository")
	_, span := tracer.Start(ctx, "repository.query_domain",
		trace.WithAttributes(
			attribute.String("query.domain", domain),
		),
	)
	defer span.End()

	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE domain = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, domain)
	if err != nil {
		log.Err(err).
			Str("domain", domain).
			Msg("Domain match query failed")
		return nil
	}
	defer rows.Close()

	var hits []entries.Hit

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Err(err).
				Str("domain", domain).
				Msg("Failed to scan row in queryDomainMatch")

			continue // Or handle the error as appropriate
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    "DOMAIN",
			MatchedValue: domain,
		})
	}

	if err := rows.Err(); err != nil {
		log.Err(err).
			Str("domain", domain).
			Msg("Error iterating rows in queryDomainMatch")

		return nil
	}

	log.Debug().Dur("duration", time.Since(startTime)).Str("match_type", "DOMAIN").Msg("Domain match query completed")

	return hits
}

func (r *SQLiteRepository) queryPathMatch(ctx context.Context, path string) []entries.Hit {
	tracer := otel.Tracer("blacked/repository")
	_, span := tracer.Start(ctx, "repository.query_path",
		trace.WithAttributes(
			attribute.String("query.path", path),
		),
	)
	defer span.End()

	startTime := time.Now()
	query := "SELECT id FROM blacklist_entries WHERE path = ? AND deleted_at IS NULL"
	rows, err := r.db.QueryContext(ctx, query, path)
	if err != nil {
		log.Err(err).
			Str("path", path).
			Msg("Path match query failed")

		return nil
	}
	defer rows.Close()

	var hits []entries.Hit

	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		if err != nil {
			log.Err(err).
				Str("path", path).
				Msg("Failed to scan row in queryPathMatch")

			continue // Or handle the error as appropriate
		}
		hits = append(hits, entries.Hit{
			ID:           id,
			MatchType:    "PATH",
			MatchedValue: path,
		})
	}

	if err := rows.Err(); err != nil {
		log.Err(err).
			Str("path", path).
			Msg("Error iterating rows in queryPathMatch")

		return nil
	}

	log.Debug().Dur("duration", time.Since(startTime)).Str("match_type", "PATH").Msg("Path match query completed")

	return hits
}
