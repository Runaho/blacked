package db

import (
	"database/sql"
	"fmt"
	"time"

	"blacked/internal/db/models"

	"github.com/rs/zerolog/log"
)

// NewSchemaDDL contains the CREATE statements for the cleaned schema.
// WARNING: Only run this on fresh databases or dev environments.
// For production, use incremental migrations that preserve existing data.
const NewSchemaDDL = `
CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    trust_score REAL NOT NULL DEFAULT 0.5,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sources (
    id              TEXT PRIMARY KEY,
    provider_id     TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    source_url      TEXT NOT NULL,
    type            TEXT NOT NULL,
    trust_score     REAL,
    update_interval INTEGER,
    enabled         INTEGER NOT NULL DEFAULT 1,
    last_fetch_at   DATETIME,
    last_fetch_status TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS entries (
    id          TEXT PRIMARY KEY,
    process_id  TEXT,
    scheme      TEXT,
    domain      TEXT,
    host        TEXT,
    sub_domains TEXT,
    path        TEXT,
    raw_query   TEXT,
    source_url  TEXT,
    source      TEXT NOT NULL,
    category    TEXT,
    confidence  REAL DEFAULT 1.0,
    created_at  INTEGER,
    updated_at  INTEGER,
    deleted_at  INTEGER,
    UNIQUE (source_url, source)
);

CREATE TABLE IF NOT EXISTS provider_processes (
    id          TEXT PRIMARY KEY,
    status      TEXT,
    start_time  DATETIME,
    end_time    DATETIME,
    providers_processed TEXT,
    providers_removed   TEXT,
    error       TEXT
);

-- Indexes for entries table
CREATE INDEX IF NOT EXISTS idx_entries_domain ON entries(domain);
CREATE INDEX IF NOT EXISTS idx_entries_host ON entries(host);
CREATE INDEX IF NOT EXISTS idx_entries_source ON entries(source);
CREATE INDEX IF NOT EXISTS idx_entries_source_url ON entries(source_url);

-- Indexes for sources
CREATE INDEX IF NOT EXISTS idx_sources_provider ON sources(provider_id);
`

// MigrateSchema creates the new tables if they don't exist.
func MigrateSchema(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	_, err := db.Exec(NewSchemaDDL)
	if err != nil {
		return fmt.Errorf("failed to execute new schema DDL: %w", err)
	}

	log.Trace().Msg("New schema tables ensured (providers, sources, entries, provider_processes)")
	return nil
}

// SeedProviders inserts the default provider seed data, ignoring conflicts.
func SeedProviders(db *sql.DB) error {
	stmt, err := db.Prepare(`
		INSERT INTO providers (id, name, description, trust_score, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			trust_score = EXCLUDED.trust_score,
			updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare seed providers: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, p := range models.ProviderSeed {
		_, err := stmt.Exec(p.ID, p.Name, p.Description, p.TrustScore, now, now)
		if err != nil {
			log.Warn().Err(err).Str("provider", p.ID).Msg("Failed to seed provider")
		}
	}

	log.Trace().Int("count", len(models.ProviderSeed)).Msg("Provider seed completed")
	return nil
}

// SeedSources inserts the default source seed data, ignoring conflicts.
func SeedSources(db *sql.DB) error {
	stmt, err := db.Prepare(`
		INSERT INTO sources (id, provider_id, name, source_url, type, update_interval, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider_id     = EXCLUDED.provider_id,
			name            = EXCLUDED.name,
			source_url      = EXCLUDED.source_url,
			type            = EXCLUDED.type,
			update_interval = EXCLUDED.update_interval,
			enabled         = EXCLUDED.enabled,
			updated_at      = EXCLUDED.updated_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare seed sources: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, s := range models.SourceSeed {
		var interval interface{}
		if s.UpdateInterval.Valid {
			interval = s.UpdateInterval.Int64
		} else {
			interval = nil
		}
		_, err := stmt.Exec(s.ID, s.ProviderID, s.Name, s.SourceURL, string(s.Type), interval, 1, now, now)
		if err != nil {
			log.Warn().Err(err).Str("source", s.ID).Msg("Failed to seed source")
		}
	}

	log.Trace().Int("count", len(models.SourceSeed)).Msg("Source seed completed")
	return nil
}

// FullMigration runs schema creation and seeding.
func FullMigration(db *sql.DB) error {
	if err := MigrateSchema(db); err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}
	if err := SeedProviders(db); err != nil {
		return fmt.Errorf("provider seeding failed: %w", err)
	}
	if err := SeedSources(db); err != nil {
		return fmt.Errorf("source seeding failed: %w", err)
	}
	return nil
}
