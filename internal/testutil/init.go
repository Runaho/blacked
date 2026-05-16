package testutil

import (
	"blacked/features/entry_collector"
	ic "blacked/internal/colly"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/logger"
	"context"
	"database/sql"
	"testing"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
)

func Initialize(t *testing.T) (ctx context.Context, _db *sql.DB, cc *colly.Collector, err error) {
	logger.InitializeLogger()
	err = config.InitConfig()
	assert.NoError(t, err, "Should initialize config without error")

	_db, err = db.GetTestDB()
	assert.NoError(t, err, "Expected no error while obtaining DB")

	db.EnsureDBSchemaExists(db.WithTesting(true))

	// Initialize the pond collector so provider tests don't fail with "Entry collector not set"
	ctx = context.Background()
	entry_collector.InitPondCollector(ctx, _db)

	cc, err = ic.InitCollyClient()
	assert.NoError(t, err, "Expected no error while initializing colly client")

	return
}
