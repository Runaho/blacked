package db

import (
	"database/sql"
	"fmt"
	"time"

	"blacked/internal/db/models"

	"github.com/rs/zerolog/log"
)

// currentSchemaVersion is the latest migration version.
// Bump this whenever a new migration file is added.
const currentSchemaVersion = 1

// Migrations is the ordered list of SQL migration files (basename without extension).
var Migrations = []int{1}

// Migration files are stored in internal/db/migrations/ as 000001_init_schema.up.sql
// and optionally 000001_init_schema.down.sql.

func getMigrationSQL(version int, direction string) (string, error) {
	// Embedding raw SQL in Go strings to avoid embed package requirement.
	// Each migration's up/down SQL is stored as a string constant keyed by version+direction.
	switch version {
	case 1:
		if direction == "up" {
			return migration1Up, nil
		}
		return migration1Down, nil
	}
	return "", fmt.Errorf("migration %d not found", version)
}

const migration1Up = `
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
    ip          TEXT,
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
    UNIQUE (source_url, source, host)
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

CREATE INDEX IF NOT EXISTS idx_entries_domain ON entries(domain);
CREATE INDEX IF NOT EXISTS idx_entries_host ON entries(host);
CREATE INDEX IF NOT EXISTS idx_entries_ip ON entries(ip);
CREATE INDEX IF NOT EXISTS idx_entries_source ON entries(source);
CREATE INDEX IF NOT EXISTS idx_entries_source_url ON entries(source_url);
CREATE INDEX IF NOT EXISTS idx_sources_provider ON sources(provider_id);
`

const migration1Down = `
DROP TABLE IF EXISTS provider_processes;
DROP TABLE IF EXISTS entries;
DROP TABLE IF EXISTS sources;
DROP TABLE IF EXISTS providers;
`

// ensureMigrationsTable creates the migrations tracking table if it doesn't exist.
func ensureMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

// GetCurrentVersion returns the latest applied migration version, or 0 if none applied.
func GetCurrentVersion(db *sql.DB) (int, error) {
	if err := ensureMigrationsTable(db); err != nil {
		return 0, err
	}
	var version int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	return version, err
}

// Migrate runs all pending migrations up to currentSchemaVersion.
func Migrate(db *sql.DB) error {
	current, err := GetCurrentVersion(db)
	if err != nil {
		return fmt.Errorf("Migrate: failed to get current version: %w", err)
	}

	if current > currentSchemaVersion {
		return fmt.Errorf("database schema version (%d) is newer than supported version (%d)",
			current, currentSchemaVersion)
	}

	for _, version := range Migrations {
		if version <= current {
			continue // already applied
		}

		sqlStr, err := getMigrationSQL(version, "up")
		if err != nil {
			return fmt.Errorf("Migrate: migration %d not found: %w", version, err)
		}

		log.Info().Int("version", version).Msg("Applying migration")
		if _, err := db.Exec(sqlStr); err != nil {
			return fmt.Errorf("Migrate: failed to apply migration %d: %w", version, err)
		}

		if _, err := db.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC(),
		); err != nil {
			return fmt.Errorf("Migrate: failed to record migration %d: %w", version, err)
		}
		log.Info().Int("version", version).Msg("Migration applied successfully")
	}
	return nil
}

// Rollback runs migrations down by one step (for testing/reversibility).
func Rollback(db *sql.DB) error {
	current, err := GetCurrentVersion(db)
	if err != nil {
		return fmt.Errorf("Rollback: failed to get current version: %w", err)
	}
	if current == 0 {
		return fmt.Errorf("Rollback: no migrations to roll back")
	}

	sqlStr, err := getMigrationSQL(current, "down")
	if err != nil {
		return fmt.Errorf("Rollback: migration %d not found: %w", current, err)
	}

	log.Info().Int("version", current).Msg("Rolling back migration")
	if _, err := db.Exec(sqlStr); err != nil {
		return fmt.Errorf("Rollback: failed to rollback migration %d: %w", current, err)
	}

	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = ?`, current); err != nil {
		return fmt.Errorf("Rollback: failed to remove migration record %d: %w", current, err)
	}
	log.Info().Int("version", current).Msg("Rollback complete")
	return nil
}

// MigrateSchema runs all pending migrations (aliased for backward compat with FullMigration).
// Deprecated: use Migrate() directly.
func MigrateSchema(db *sql.DB) error {
	return Migrate(db)
}

// FullMigration runs schema migration then seeds providers and sources.
// Replaces the old NewSchemaDDL-based approach.
func FullMigration(db *sql.DB) error {
	if err := Migrate(db); err != nil {
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