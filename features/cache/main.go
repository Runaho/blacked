package cache

import (
	"blacked/features/cache/badger_provider"
	"blacked/features/cache/cache_errors"
	"blacked/internal/config"
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

var (
	initCacheOnce sync.Once
	cacheInstance EntryCache
	cacheInitErr  error
)

// EntryCache defines the interface that all cache implementations must satisfy
type EntryCache interface {
	// Core operations
	Initialize(ctx context.Context) error
	Close() error

	// Main data operations
	Get(key string) ([]string, error)      // Returns parsed IDs and error
	Set(key string, ids string) error      // Takes raw comma-separated string
	SetIds(key string, ids []string) error // Takes array of IDs
	Commit() error
	Delete(key string) error
	Iterate(ctx context.Context, fn func(key string) error) error
}

type CacheType string

const (
	BadgerCache    CacheType = "badger"
	BigCache       CacheType = "bigcache"
	RistrettoCache CacheType = "ristretto"
)

// InitializeCache sets up the singleton cache instance based on config.
// Call this once during application startup.
func InitializeCache(ctx context.Context) error {
	initCacheOnce.Do(func() {
		cfg := config.GetConfig().Cache
		var selectedType CacheType

		switch strings.ToLower(cfg.CacheType) {
		case "badger":
			selectedType = BadgerCache
		default:
			log.Warn().Str("configured_type", cfg.CacheType).Msg("Unsupported cache type, defaulting to Badger")
			selectedType = BadgerCache
		}

		log.Info().Str("type", string(selectedType)).Msg("Initializing cache")

		switch selectedType {
		case BadgerCache:
			cacheInstance = badger_provider.NewBadgerProvider()
		default:
			// This case should technically not be reachable due to default above
			cacheInitErr = errors.New("internal error: invalid cache type selected")
			return
		}

		if err := cacheInstance.Initialize(ctx); err != nil {
			log.Error().Err(err).Str("type", string(selectedType)).Msg("Failed to initialize cache instance")
			cacheInitErr = err
		} else {
			log.Info().Str("type", string(selectedType)).Msg("Cache instance initialized successfully")
		}
	})
	return cacheInitErr
}

// GetCacheProvider returns the initialized singleton cache instance.
func GetCacheProvider() (EntryCache, error) {
	if cacheInstance == nil {
		// If instance is nil here, it means InitializeCache either wasn't called
		// or it failed.
		if cacheInitErr != nil {
			return nil, cacheInitErr
		}
		return nil, cache_errors.ErrCacheNotInitialized
	}
	return cacheInstance, nil
}

// CloseCache closes the singleton cache instance.
// Should only be called during application shutdown.
func CloseCache() {
	if cacheInstance != nil {
		err := cacheInstance.Close()
		cacheInstance = nil         // Set to nil after closing
		cacheInitErr = nil          // Reset init error
		initCacheOnce = sync.Once{} // Reset sync.Once to allow re-initialization if needed (e.g., in tests)
		if err != nil {
			log.Error().Err(err).Msg("Failed to close cache instance")
		} else {
			log.Info().Msg("Cache instance closed")
		}
	} else {
		log.Warn().Msg("Attempted to close a nil cache instance.")
	}
}
