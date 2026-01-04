package db

import (
	"database/sql"
	"errors"
	"os"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

// Error variables for database connections
var (
	ErrOpenDatabase = errors.New("failed to open SQLite database")
	ErrPingDatabase = errors.New("failed to ping SQLite database")
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

	if opts.isInWALMode && !opts.inMemory {
		dsn = dsn + "?_journal_mode=WAL"
	}

	db, err := connectSQLite(dsn, 1, 1) // Default: single connection
	if err != nil {
		return nil, err
	}

	return db, nil
}

// ConnectReadOnly creates a read-only connection pool optimized for concurrent reads.
// In WAL mode, multiple readers can read simultaneously without blocking.
func ConnectReadOnly(options ...Option) (*sql.DB, error) {
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

	// Add read-only mode and WAL mode for file-based databases
	if !opts.inMemory {
		dsn = dsn + "?_journal_mode=WAL&mode=ro"
	}

	// Allow multiple concurrent readers (e.g., 10 connections for parallel query handling)
	db, err := connectSQLite(dsn, 10, 5)
	if err != nil {
		return nil, err
	}

	log.Debug().Int("max_open", 10).Int("max_idle", 5).Msg("Read-only connection pool created")
	return db, nil
}

// ConnectReadWrite creates a write connection optimized for single-writer pattern.
// SQLite only allows one writer at a time, so we use a single connection.
func ConnectReadWrite(options ...Option) (*sql.DB, error) {
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

	// Add WAL mode for file-based databases (read-write is default)
	if opts.isInWALMode && !opts.inMemory {
		dsn = dsn + "?_journal_mode=WAL"
	}

	// Single connection for writes to prevent contention
	db, err := connectSQLite(dsn, 1, 1)
	if err != nil {
		return nil, err
	}

	log.Debug().Int("max_open", 1).Int("max_idle", 1).Msg("Read-write connection created")
	return db, nil
}

func connectSQLite(dataSourceName string, maxOpen, maxIdle int) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		log.Err(err).Str("dsn", dataSourceName).Msg("Failed to open SQLite database")
		return nil, ErrOpenDatabase
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		log.Err(err).Str("dsn", dataSourceName).Msg("Failed to ping SQLite database")
		return nil, ErrPingDatabase
	}

	// Configure connection pool
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		log.Warn().Err(err).Msg("Failed to enable foreign keys")
	}

	// Optimize SQLite for better write performance with WAL mode
	if _, err := db.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		log.Warn().Err(err).Msg("Failed to set synchronous mode")
	}

	// Increase cache size for better performance (10MB)
	if _, err := db.Exec("PRAGMA cache_size = -10000"); err != nil {
		log.Warn().Err(err).Msg("Failed to set cache size")
	}

	// Set busy timeout to handle any remaining contention
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		log.Warn().Err(err).Msg("Failed to set busy timeout")
	}

	return db, nil
}
