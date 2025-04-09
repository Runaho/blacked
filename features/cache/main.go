package cache

import (
	"blacked/internal/config"
	"errors"
	"sync" // Import sync package

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

var (
	instance *badger.DB
	cfg      *config.CacheSettings
	initOnce sync.Once
	initErr  error
)

// InitializeBadger ensures the BadgerDB instance is initialized exactly once.
// Call this function early in your application's startup.
func InitializeBadger() error {
	initOnce.Do(func() {
		log.Info().Msg("Attempting to initialize BadgerDB instance")
		cfg = &config.GetConfig().Cache // Get config inside the Once.Do

		opts := badger.DefaultOptions(cfg.BadgerPath).WithInMemory(cfg.InMemory)

		// BadgerSingleInstance logic moved here
		instance, initErr = badger.Open(opts)
		if initErr != nil {
			log.Error().Err(initErr).Msg("Failed to open badger database during initialization")
		} else {
			log.Info().Msg("BadgerDB instance initialized successfully")
		}

	})
	return initErr // Return the potential error from initialization
}

// GetBadgerInstance returns the singleton BadgerDB instance.
// It assumes InitializeBadger has been called previously.
func GetBadgerInstance() (*badger.DB, error) {
	if instance == nil {
		// If instance is nil here, it means InitializeBadger either wasn't called
		// or it failed. It's better to return the initErr.
		if initErr != nil {
			return nil, initErr
		}
		// This case ideally shouldn't be hit if InitializeBadger is called correctly at startup.
		return nil, errors.New("badger instance is nil")
	}
	return instance, nil
}

// CloseBadger closes the singleton BadgerDB instance.
// Should only be called during application shutdown.
func CloseBadger() {
	if instance != nil {
		err := instance.Close()
		instance = nil // Set to nil after closing
		if err != nil {
			log.Error().Err(err).Msg("Failed to close badger instance")
		} else {
			log.Info().Msg("Badger instance closed")
		}
	} else {
		log.Warn().Msg("Attempted to close a nil Badger instance.")
	}
}
