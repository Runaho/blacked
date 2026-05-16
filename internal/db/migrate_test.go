package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateSchema(t *testing.T) {
	db, err := Connect(WithInMemory(true))
	require.NoError(t, err)
	defer db.Close()

	err = MigrateSchema(db)
	require.NoError(t, err)

	tables := []string{"providers", "sources", "entries", "provider_processes"}
	for _, tbl := range tables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		require.NoError(t, err, "table %s should exist", tbl)
		assert.Equal(t, tbl, name)
	}

	// Check indexes exist
	indexes := []string{
		"idx_entries_domain",
		"idx_entries_host",
		"idx_entries_source",
		"idx_entries_source_url",
		"idx_sources_provider",
	}
	for _, idx := range indexes {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name)
		require.NoError(t, err, "index %s should exist", idx)
		assert.Equal(t, idx, name)
	}
}

func TestSeedProviders(t *testing.T) {
	db, err := Connect(WithInMemory(true))
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, MigrateSchema(db))
	require.NoError(t, SeedProviders(db))

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 5)

	// verify upsert is idempotent
	require.NoError(t, SeedProviders(db))
	err = db.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 5)
}

func TestSeedSources(t *testing.T) {
	db, err := Connect(WithInMemory(true))
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, MigrateSchema(db))
	require.NoError(t, SeedProviders(db))
	require.NoError(t, SeedSources(db))

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 3)
}

func TestFullMigration(t *testing.T) {
	db, err := Connect(WithInMemory(true))
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, FullMigration(db))

	var providerCount, sourceCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&providerCount))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&sourceCount))

	assert.GreaterOrEqual(t, providerCount, 5)
	assert.GreaterOrEqual(t, sourceCount, 3)
}
