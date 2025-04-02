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
}

var (
	instance dbInstance
	initOnce sync.Once
	roErr    error
)

func GetDB() (*sql.DB, error) {
	InitializeDB()
	return instance.db, instance.err
}

func InitializeDB(options ...Option) {
	initOnce.Do(func() {
		if err := EnsureDBSchemaExists(); err != nil {
			log.Error().Err(err).Stack().Msg("Failed to ensure schema exists")
			instance.err = err
			return
		}

		roDB, err := Connect(options...)
		if err != nil {
			log.Error().Err(err).Stack().Msg("Failed to open read‐only database connection")
			instance.err = err
			return
		}

		instance.db = roDB

		log.Trace().Msg("Database connection (read‐only) initialized.")
	})
}

func Close() error {
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
	// Close any existing open DB
	if instance.db != nil {
		_ = instance.db.Close()
		instance.db = nil
	}

	instance.err = nil
	initOnce = sync.Once{}

	fmt.Println("DB reset for testing.")
}

func GetTestDB() (*sql.DB, error) {
	dbTest, err := Connect(WithTesting(true))
	if err != nil {
		return nil, fmt.Errorf("failed to open test database connection: %w", err)
	}
	log.Debug().Msg("Test database connection opened.")
	return dbTest, nil
}
