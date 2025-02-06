package utils

import (
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

	// Initialize DB (once) for tests
	db.SetTesting(true)
	_db, err = db.GetDB()
	assert.NoError(t, err, "Expected no error while obtaining DB")

	cc, err = ic.InitCollyClient()
	assert.NoError(t, err, "Expected no error while initializing colly client")

	return
}
