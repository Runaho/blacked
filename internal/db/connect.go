package db

import (
	"database/sql"
	"fmt"

	_ "github.com/marcboeker/go-duckdb"
)

const (
	dbName = "blacked.db"
	testDB = "blacked-test.db"
)

var (
	isTesting bool
)

func SetTesting(state bool) {
	isTesting = state
}

// InitializeDatabase opens the database connection and then initializes the database schema using InitDB.
func initializeDatabase() (db *sql.DB, err error) {
	if isTesting {
		db, err = sql.Open("duckdb", testDB) // Open in-memory DuckDB connection for testing
	} else {
		db, err = sql.Open("duckdb", dbName) // Open DuckDB connection (same as before)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open DuckDB database: %w", err)
	}

	if err := initDB(db); err != nil { // Call InitDB to setup the schema after connection
		_ = db.Close() // Close DB if schema initialization fails
		return nil, fmt.Errorf("failed to initialize database schema: %w", err)
	}

	return db, nil
}

// InitDB initializes the database schema (creates tables, indexes) if they do not exist.
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
	return nil
}
