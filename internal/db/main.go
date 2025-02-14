package db

import (
	"database/sql"
	"fmt"
	"sync"
)

type dbInstance struct {
	db  *sql.DB
	err error
	mu  sync.RWMutex
}

var (
	instance dbInstance
	initOnce sync.Once
)

func GetDB() (*sql.DB, error) {
	initOnce.Do(func() {
		instance.mu.Lock()
		defer instance.mu.Unlock()

		if err := EnsureDBExists(); err != nil {
			instance.err = fmt.Errorf("failed to ensure schema exists: %w", err)
			return
		}

		roDB, err := GetReadOnlyDB()
		if err != nil {
			instance.err = fmt.Errorf("failed to open read‐only DB: %w", err)
			return
		}

		instance.db = roDB

		fmt.Println("Database connection (read‐only) initialized.")
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
