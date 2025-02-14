package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

const (
	dbName   = "blacked.db"
	testDB   = "blacked-test.db"
	memoryDB = ":memory:"
)

type dbOptions struct {
	isTesting  bool
	isReadOnly bool
	inMemory   bool
}

func (o *dbOptions) GetIsReadOnly() bool {
	return o.isReadOnly
}

func (o *dbOptions) GetIsTesting() bool {
	return o.isTesting
}

func (o *dbOptions) GetInMemory() bool {
	return o.inMemory
}

type Option func(*dbOptions)

func WithTesting(state bool) Option {
	return func(opts *dbOptions) {
		opts.isTesting = state
	}
}

func WithReadOnly(state bool) Option {
	return func(opts *dbOptions) {
		opts.isReadOnly = state
	}
}

func WithInMemory(state bool) Option {
	return func(opts *dbOptions) {
		opts.inMemory = state
	}
}

func Connect(options ...Option) (*sql.DB, error) {
	opts := dbOptions{
		isTesting:  false, // Default to false
		isReadOnly: false, // Default to read-write
		inMemory:   false, // Default to file-based
	}

	// Apply options in the order they are provided
	for _, opt := range options {
		opt(&opts)
	}

	log.
		Debug().
		Bool("is_testing", opts.isTesting).
		Bool("is_read_only", opts.isReadOnly).
		Bool("in_memory", opts.inMemory).
		Msg("Connect: Applying DB options")

	// Determine the data source name (DSN) based on options
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

	log.Debug().Str("dsn", dsn).Msg("Opening SQLite database connection")

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
		log.Debug().Msg("Skipping schema initialization for read-only connection.")
	}

	return db, nil
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`
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
    `)
	if err != nil {
		return fmt.Errorf("failed to create table and indexes: %w", err)
	}
	log.Info().Msg("Database schema initialized or already exists.")
	return nil
}

func EnsureDBExists(opts ...Option) error {
	baseOpts := dbOptions{
		isTesting:  false,
		isReadOnly: false,
		inMemory:   false,
	}

	for _, opt := range opts {
		opt(&baseOpts)
	}

	log.
		Debug().
		Bool("is_testing", baseOpts.isTesting).
		Bool("is_read_only", baseOpts.isReadOnly).
		Bool("in_memory", baseOpts.inMemory).
		Msg("EnsureDBExists: Checking/Ensuring DB Schema")

	useRW := false

	if !baseOpts.inMemory && !baseOpts.isTesting {
		if _, err := os.Stat(dbName); err != nil {
			useRW = true
			log.Info().Msg("Database file does not exist, will create and initialize schema.")
		} else {
			log.Debug().Msg("Database file exists, schema will not be checked/initialized.")
			useRW = false
			return nil
		}
	} else if baseOpts.inMemory || baseOpts.isTesting {
		useRW = true
		log.Debug().Msg("In-memory or test database, schema will be checked/initialized.")
	}

	if !useRW {
		log.Debug().Msg("No schema creation or check needed.")
		return nil
	}

	dbRW, err := Connect(WithReadOnly(false), WithInMemory(baseOpts.inMemory), WithTesting(baseOpts.isTesting))
	if err != nil {
		return fmt.Errorf("failed to open sqlite in RW mode for schema creation: %w", err)
	}
	defer dbRW.Close()

	log.Debug().Msg("RW connection opened for schema check/initialization.")

	log.Debug().Msg("Schema check/initialization completed, RW connection closed.")
	return nil
}

var (
	roOnce sync.Once
	roDB   *sql.DB
	roErr  error
)

func GetReadOnlyDB(opts ...Option) (*sql.DB, error) {
	roOnce.Do(func() {
		roDB, roErr = Connect(WithReadOnly(true))
		if roErr != nil {
			log.Error().Err(roErr).Msg("Failed to open read-only database connection")
			return
		}
		log.Info().Msg("Read-only database connection initialized.")
	})
	return roDB, roErr
}

func GetReadWriteDB() (*sql.DB, error) {
	dbRW, err := Connect(WithReadOnly(false))
	if err != nil {
		return nil, fmt.Errorf("failed to open read-write database connection: %w", err)
	}
	log.Debug().Msg("Short-lived read-write database connection opened.")
	return dbRW, nil
}

func GetTestDB() (*sql.DB, error) {
	dbTest, err := Connect(WithTesting(true), WithReadOnly(false))
	if err != nil {
		return nil, fmt.Errorf("failed to open test database connection: %w", err)
	}
	log.Debug().Msg("Test database connection opened.")
	return dbTest, nil
}

func CloseReadOnlyDB() error {
	if roDB != nil {
		err := roDB.Close()
		if err != nil {
			return fmt.Errorf("failed to close read-only database connection: %w", err)
		}
		roDB = nil
		log.Info().Msg("Read-only database connection closed.")
	} else {
		log.Debug().Msg("Read-only database connection was not open, or already closed.")
	}
	return nil
}
