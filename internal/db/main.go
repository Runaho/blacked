package db

import (
	"database/sql"
	"fmt"
	"sync"
)

var (
	db    *sql.DB
	once  sync.Once
	dbErr error
)

func GetDB() (*sql.DB, error) {
	once.Do(func() {
		db, dbErr = initializeDatabase() // Call your existing InitializeDatabase function
		if dbErr != nil {
			dbErr = fmt.Errorf("failed to initialize database connection: %w", dbErr)
			return
		}
		fmt.Println("Database connection initialized.") // Informational message when connection is established
	})
	return db, dbErr
}

func DeferClose() {
	if db != nil {
		if err := db.Close(); err != nil {
			fmt.Printf("Failed to close database connection: %v\n", err)
		}
	}
}
