package db

import (
	"blacked/internal/query"
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// entryRepository implements query.EntryRepository on the new entries table.
type entryRepository struct {
	db *sql.DB
}

// NewEntryRepository creates an EntryRepository backed by the given sql.DB.
// Use GetDB() (read pool) for querying, GetWriteDB() for writes.
func NewEntryRepository(db *sql.DB) query.EntryRepository {
	return &entryRepository{db: db}
}

func (r *entryRepository) SearchEntries(ctx context.Context, filter query.SearchFilter) ([]query.Entry, error) {
	var conditions []string
	var args []any

	addFilter := func(col, val string) {
		if val != "" {
			conditions = append(conditions, fmt.Sprintf("%s = ?", col))
			args = append(args, val)
		}
	}

	addFilter("domain", filter.Domain)
	addFilter("host", filter.Host)
	addFilter("path", filter.Path)
	addFilter("source", filter.SourceID)

	// Category uses LIKE for partial match
	if filter.Category != "" {
		conditions = append(conditions, "category LIKE ?")
		args = append(args, "%"+filter.Category+"%")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := max(filter.Offset, 0)

	q := fmt.Sprintf(`
		SELECT id, source, source_url, domain, host, path, raw_query, scheme, confidence, category
		FROM entries
		%s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, where)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	defer rows.Close()

	var out []query.Entry
	for rows.Next() {
		var e query.Entry
		var confidence sql.NullFloat64
		var sourceURL, rawQuery sql.NullString
		err := rows.Scan(
			&e.ID, &e.SourceID, &sourceURL,
			&e.Domain, &e.Host, &e.Path, &rawQuery, &e.Scheme, &confidence, &e.Category,
		)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		if confidence.Valid {
			e.Confidence = confidence.Float64
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return out, nil
}

// ExistsByHost confirms whether any non-deleted entry exists for a hostname.
func (r *entryRepository) ExistsByHost(ctx context.Context, host string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries WHERE host = ? AND deleted_at IS NULL LIMIT 1)
	`, host).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("exists by host: %w", err)
	}
	return exists, nil
}

// ExistsByDomain confirms whether any non-deleted entry exists for a domain.
func (r *entryRepository) ExistsByDomain(ctx context.Context, domain string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries WHERE domain = ? AND deleted_at IS NULL LIMIT 1)
	`, domain).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("exists by domain: %w", err)
	}
	return exists, nil
}

// ExistsByIP confirms whether any non-deleted entry exists for an IP address.
func (r *entryRepository) ExistsByIP(ctx context.Context, ip string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM entries WHERE host = ? AND deleted_at IS NULL LIMIT 1)
	`, ip).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("exists by ip: %w", err)
	}
	return exists, nil
}

// ExistsByBloomType routes DB existence checks by bloom match type.
// domain → ExistsByDomain, host → ExistsByHost, ip → ExistsByIP.
// file → path suffix, host_path → source_url contains, full_url → source_url exact.
func (r *entryRepository) ExistsByBloomType(ctx context.Context, matchType, key string) (bool, error) {
	switch matchType {
	case "domain":
		return r.ExistsByDomain(ctx, key)
	case "host":
		return r.ExistsByHost(ctx, key)
	case "ip":
		return r.ExistsByIP(ctx, key)
	case "file":
		var exists bool
		err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM entries WHERE path LIKE '%/' || ? AND deleted_at IS NULL LIMIT 1)
		`, key).Scan(&exists)
		if err != nil {
			return false, fmt.Errorf("exists by file: %w", err)
		}
		return exists, nil
	case "host_path":
		var exists bool
		// Bloom key is host+path (e.g. "evil.com/malware"). Confirm against host and path
		// with proper prefix matching to avoid substring false positives.
		err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM entries WHERE host || path = ? AND deleted_at IS NULL LIMIT 1)
		`, key).Scan(&exists)
		if err != nil {
			return false, fmt.Errorf("exists by host_path: %w", err)
		}
		return exists, nil
	case "full_url":
		var exists bool
		// Bloom key is host+path+?query (no scheme, no port — same as how entries
		// are stored now with Host normalized to Hostname()).
		// Reconstruct from normalized DB columns instead of raw source_url.
		err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM entries
			  WHERE host || COALESCE(path, '') || CASE WHEN raw_query != '' THEN '?' || raw_query ELSE '' END = ?
			  AND deleted_at IS NULL LIMIT 1)
		`, key).Scan(&exists)
		if err != nil {
			return false, fmt.Errorf("exists by full_url: %w", err)
		}
		return exists, nil
	default:
		return false, fmt.Errorf("unknown bloom match type: %s", matchType)
	}
}

// GetEntryByFullURL looks up an exact source_url match (used by Hit after bloom positive).
func (r *entryRepository) GetEntryByFullURL(ctx context.Context, fullURL string) (*query.Entry, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, source, domain, host, path, scheme, confidence, category
		FROM entries
		WHERE source_url = ?
		LIMIT 1
	`, fullURL)

	var e query.Entry
	var confidence sql.NullFloat64
	err := row.Scan(
		&e.ID, &e.SourceID,
		&e.Domain, &e.Host, &e.Path, &e.Scheme, &confidence, &e.Category,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get entry by full url: %w", err)
	}
	if confidence.Valid {
		e.Confidence = confidence.Float64
	}
	return &e, nil
}
