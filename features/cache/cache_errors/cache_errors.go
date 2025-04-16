package cache_errors

import "errors"

var (
	ErrCacheNotInitialized = errors.New("cache not initialized")
	ErrKeyNotFound         = errors.New("key not found in cache")
	ErrValueTooLarge       = errors.New("value too large for cache")
)
