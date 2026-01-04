package db

import (
	"database/sql"
	"errors"
	"sync"

	"github.com/rs/zerolog/log"
)

// Error variables for database operations
var (
	ErrCloseRODB  = errors.New("failed to close read-only database connection")
	ErrCloseRWDB  = errors.New("failed to close read-write database connection")
	ErrOpenTestDB = errors.New("failed to open test database connection")
)

type dbInstance struct {
	readDB  *sql.DB // Read-only connection pool (multiple readers allowed)
	writeDB *sql.DB // Write connection (single writer)
	err     error
}

var (
	instance dbInstance
	initOnce sync.Once
)

// GetDB returns the read-only database connection pool.
// Use this for all SELECT queries - supports concurrent reads.
func GetDB() (*sql.DB, error) {
	InitializeDB()
	return instance.readDB, instance.err
}

// GetReadDB is an alias for GetDB - returns the read-only connection pool.
func GetReadDB() (*sql.DB, error) {
	return GetDB()
}

// GetWriteDB returns the write database connection.
// Use this for INSERT/UPDATE/DELETE operations.
// This connection has MaxOpenConns=1 to prevent SQLite write contention.
func GetWriteDB() (*sql.DB, error) {
	InitializeDB()
	return instance.writeDB, instance.err
}

func InitializeDB(options ...Option) {
	initOnce.Do(func() {
		if err := EnsureDBSchemaExists(); err != nil {
			log.Error().Err(err).Stack().Msg("Failed to ensure schema exists")
			instance.err = err
			return
		}

		// Create read-only connection pool (multiple concurrent readers)
		readDB, err := ConnectReadOnly(options...)
		if err != nil {
			log.Error().Err(err).Stack().Msg("Failed to open read-only database connection")
			instance.err = err
			return
		}
		instance.readDB = readDB

		// Create write connection (single writer)
		writeDB, err := ConnectReadWrite(options...)
		if err != nil {
			log.Error().Err(err).Stack().Msg("Failed to open read-write database connection")
			_ = readDB.Close()
			instance.err = err
			return
		}
		instance.writeDB = writeDB

		log.Info().
			Msg("Database connections initialized (separate read/write pools)")
	})
}

func Close() error {
	var errs []error

	if instance.readDB != nil {
		if err := instance.readDB.Close(); err != nil {
			log.Err(err).Stack().Msg("Failed to close read-only database connection")
			errs = append(errs, ErrCloseRODB)
		}
		instance.readDB = nil
		log.Trace().Msg("Read-only database connection closed.")
	}

	if instance.writeDB != nil {
		if err := instance.writeDB.Close(); err != nil {
			log.Err(err).Stack().Msg("Failed to close read-write database connection")
			errs = append(errs, ErrCloseRWDB)
		}
		instance.writeDB = nil
		log.Trace().Msg("Read-write database connection closed.")
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func ResetForTesting() {
	// Close any existing open DB connections
	if instance.readDB != nil {
		_ = instance.readDB.Close()
		instance.readDB = nil
	}
	if instance.writeDB != nil {
		_ = instance.writeDB.Close()
		instance.writeDB = nil
	}

	instance.err = nil
	initOnce = sync.Once{}

	log.Info().Msg("Database connections reset for testing.")
}

func GetTestDB() (*sql.DB, error) {
	dbTest, err := Connect(WithTesting(true))
	if err != nil {
		return nil, ErrOpenTestDB
	}

	log.Debug().Msg("Test database connection opened.")

	return dbTest, nil
}
