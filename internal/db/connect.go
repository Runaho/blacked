package db

import (
	"database/sql"
	"errors"
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
		isTesting:  false,
		isReadOnly: false,
		inMemory:   false,
	}

	for _, opt := range opts {
		opt(&baseOpts)
	}

	log.
		Trace().
		Bool("is_testing", baseOpts.isTesting).
		Bool("is_read_only", baseOpts.isReadOnly).
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

	dbRW, err := Connect(WithReadOnly(false), WithInMemory(baseOpts.inMemory), WithTesting(baseOpts.isTesting))
	if err != nil {
		log.Error().Err(err).Stack().Msg("Failed to open RW connection for schema creation.")
		return err
	}
	defer dbRW.Close()

	log.Trace().Msg("RW connection opened for schema check/initialization.")

	log.Trace().Msg("Schema check/initialization completed, RW connection closed.")
	return nil
}

func Connect(options ...Option) (*sql.DB, error) {
	opts := dbOptions{
		isTesting:  false,
		isReadOnly: false,
		inMemory:   false,
	}

	for _, opt := range options {
		opt(&opts)
	}

	log.
		Trace().
		Bool("is_testing", opts.isTesting).
		Bool("is_read_only", opts.isReadOnly).
		Bool("in_memory", opts.inMemory).
		Msg("Connect: Applying DB options")

	var dsn string
	switch {
	case opts.inMemory:
		dsn = memoryDB
	case opts.isTesting:
		dsn = testDB
	default:
		dsn = dbName
	}

	// Append read-only access mode if requested for file-based DBs
	if opts.isReadOnly && !opts.inMemory {
		dsn = fmt.Sprintf("%s?access_mode=read_only", dsn)
	} else if opts.isReadOnly && opts.inMemory {
		return nil, errors.New("read-only mode is not supported with in-memory database")
	}

	log.Trace().Str("dsn", dsn).Msg("Opening SQLite database connection")

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	// Initialize schema only for read-write connections (default)
	if !opts.isReadOnly {
		if err := initDB(db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to initialize database schema: %w", err)
		}
	} else {
		log.Trace().Msg("Skipping schema initialization for read-only connection.")
	}

	return db, nil
}
