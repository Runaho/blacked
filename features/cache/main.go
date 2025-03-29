package cache

import (
	"blacked/internal/config"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

var (
	instance *badger.DB
	cfg      *config.CacheSettings
)

// Use defer db.Close() to close the database connection
func BadgerSingleInstance() (db *badger.DB, err error) {
	if cfg == nil {
		cfg = &config.GetConfig().Cache
	}

	instance, err = badger.Open(badger.DefaultOptions(cfg.BadgerPath).WithInMemory(cfg.InMemory))
	if err != nil {
		log.Error().Err(err).Msg("Failed to open badger database")
	}
	return instance, err
}

func GetBadgerInstance() *badger.DB {
	if instance == nil {
		log.Info().Msg("Badger instance is nil trying to init")
		BadgerSingleInstance()
	}

	return instance
}
