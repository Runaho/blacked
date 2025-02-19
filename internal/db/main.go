package db

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

type dbInstance struct {
	db  *sql.DB
	err error
	mu  sync.RWMutex
}

var (
	instance dbInstance
	initOnce sync.Once
	roOnce   sync.Once
	roDB     *sql.DB
	roErr    error
)

func GetDB() (*sql.DB, error) {
	initOnce.Do(func() {
		instance.mu.Lock()
		defer instance.mu.Unlock()

		if err := EnsureDBSchemaExists(); err != nil {
			log.Error().Err(err).Stack().Msg("Failed to ensure schema exists")
			instance.err = err
			return
		}

		roDB, err := GetReadOnlyDB()
		if err != nil {
			log.Error().Err(err).Stack().Msg("Failed to open read‐only database connection")
			instance.err = err
			return
		}

		instance.db = roDB

		log.Trace().Msg("Database connection (read‐only) initialized.")
	})

	instance.mu.RLock()
	defer instance.mu.RUnlock()
	return instance.db, instance.err
}

func Close() error {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.db != nil {
		err := instance.db.Close()
		instance.db = nil
		if err != nil {
			return fmt.Errorf("failed to close read‐only database connection: %w", err)
		}
		fmt.Println("Database connection (read‐only) closed.")
	}
	return nil
}

func ResetForTesting() {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	// Close any existing open DB
	if instance.db != nil {
		_ = instance.db.Close()
		instance.db = nil
	}

	instance.err = nil
	initOnce = sync.Once{}

	fmt.Println("DB reset for testing.")
}

func GetReadOnlyDB(opts ...Option) (*sql.DB, error) {
	roOnce.Do(func() {
		roDB, roErr = Connect(WithReadOnly(true))
		if roErr != nil {
			log.Error().Err(roErr).Msg("Failed to open read-only database connection")
			return
		}
		log.Trace().Msg("Read-only database connection initialized.")
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
