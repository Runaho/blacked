package db

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

const (
	dbName   = "blacked.db"
	testDB   = "blacked-test.db"
	memoryDB = ":memory:"
	schema   = `
	CREATE TABLE IF NOT EXISTS blacklist_entries (
		id TEXT PRIMARY KEY,
		process_id TEXT,
		scheme TEXT,
		domain TEXT,
		host TEXT,
		sub_domains TEXT,
		path TEXT,
		raw_query TEXT,
		source_url TEXT,
		source TEXT,
		category TEXT,
		confidence REAL,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME,
		UNIQUE (source_url, source)
	);
	CREATE TABLE IF NOT EXISTS provider_processes (
		id TEXT PRIMARY KEY,
		status TEXT,
		start_time DATETIME,
		end_time DATETIME,
		providers_processed TEXT, -- Store as comma-separated string or JSON
		providers_removed TEXT,   -- Store as comma-separated string or JSON
		error TEXT
	);
	`
)

func initDB(db *sql.DB) error {
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}
	log.Trace().Msg("Database schema initialized or already exists (including provider_processes table).")
	return nil
}

func EnsureDBSchemaExists(opts ...Option) error {
	baseOpts := dbOptions{
		isTesting: false,
		inMemory:  false,
	}

	for _, opt := range opts {
		opt(&baseOpts)
	}

	log.
		Trace().
		Bool("is_testing", baseOpts.isTesting).
		Bool("in_memory", baseOpts.inMemory).
		Msg("ensureDBSchemaExists: Checking/Ensuring DB Schema")

	useRW := true // Schema needs to be checked/created always if not in memory

	if !baseOpts.inMemory && !baseOpts.isTesting {
		if _, err := os.Stat(dbName); os.IsNotExist(err) { // Check if DB file does not exist
			useRW = true // Create schema if file doesn't exist
			log.Debug().Msg("Database file does not exist, will create and initialize schema.")
		} else {
			useRW = true // Check schema even if file exists
			log.Trace().Msg("Database file exists, schema will be checked/initialized.")
		}
	} else if baseOpts.inMemory || baseOpts.isTesting {
		useRW = true
		log.Trace().Msg("In-memory or test database, schema will be checked/initialized.")
	}

	if !useRW {
		log.Trace().Msg("No schema creation or check needed.")
		return nil
	}

	dbRW, err := Connect(WithInMemory(baseOpts.inMemory), WithTesting(baseOpts.isTesting))
	if err != nil {
		log.Error().Err(err).Stack().Msg("Failed to open RW connection for schema creation.")
		return err
	}
	defer dbRW.Close()

	log.Trace().Msg("RW connection opened for schema check/initialization.")

	log.Trace().Msg("Schema check/initialization completed, RW connection closed.")

	if err = initDB(dbRW); err != nil {
		log.Error().Err(err).Stack().Msg("Failed to initialize schema.")
		return err
	}

	return nil
}

func Connect(options ...Option) (*sql.DB, error) {
	opts := dbOptions{
		isTesting:   false,
		inMemory:    false,
		isInWALMode: true,
	}
	for _, opt := range options {
		opt(&opts)
	}

	var dsn string
	switch {
	case opts.inMemory:
		dsn = memoryDB
	case opts.isTesting:
		dsn = testDB
	default:
		dsn = dbName
	}

	// IMPORTANT: Enable WAL mode if you want better concurrency (readers).
	// This must be added to the DSN or executed via a PRAGMA after open.
	//
	// If you want WAL mode for a file-based DB, you append: "?_journal_mode=WAL"
	// or do something like this:
	// dsn = dsn + "?_journal_mode=WAL"
	//

	if opts.isInWALMode {
		dsn = dsn + "?_journal_mode=WAL"
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	// If your app is long-running, you can set how long to keep idle conns:
	// db.SetConnMaxIdleTime(5 * time.Minute)
	// db.SetConnMaxLifetime(30 * time.Minute)

	return db, nil
}
