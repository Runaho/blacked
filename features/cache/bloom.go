package cache

import (
	"context"
	"errors"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/rs/zerolog/log"
)

var (
	bloomFilter *bloom.BloomFilter

	ErrBloomFilterNotInitialized = errors.New("bloom filter not initialized")
	ErrPopulateBloom             = errors.New("failed to populate bloom filter")
)

func GetBloomFilter() (*bloom.BloomFilter, error) {
	if bloomFilter == nil {
		return nil, ErrBloomFilterNotInitialized
	}
	return bloomFilter, nil
}

func BuildBloomFilterFromCacheProvider(ctx context.Context, cacheProvider EntryCache, keyCount int) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Hour)
	defer cancel()

	if keyCount < 1000 {
		keyCount = 1000
	}

	bloomFilter = bloom.NewWithEstimates(uint(keyCount), 0.01)

	log.Info().
		Int("cache_keys", keyCount).
		Uint("bloom_capacity", bloomFilter.Cap()).
		Uint("hash_functions", bloomFilter.K()).
		Msg("Created bloom filter & Starting to populate bloom filter")

	addedKeys := 0
	startTime := time.Now()

	cacheProvider.Iterate(ctx, func(key string) error {
		if log.Trace().Enabled() {
			log.Trace().Str("key", key).Msg("Adding key to bloom filter")
		}

		bloomFilter.AddString(key)
		addedKeys++

		if addedKeys%100000 == 0 {
			elapsed := time.Since(startTime)
			rate := float64(addedKeys) / elapsed.Seconds()
			log.Trace().
				Int("keys_added", addedKeys).
				Dur("elapsed", elapsed).
				Float64("keys_per_second", rate).
				Msg("Building bloom filter - progress")
		}

		return nil
	})

	return nil
}

func CheckURL(url string) (bool, error) {
	if url == "" {
		return false, errors.New("empty URL")
	}

	bf, err := GetBloomFilter()
	if err != nil {
		return false, err
	}

	isLikely := bf.Test([]byte(url))

	return isLikely, nil
}

// CheckURLs checks multiple URLs against the bloom filter and returns those that might be in the blacklist
func CheckURLs(urls []string) ([]string, error) {
	bf, err := GetBloomFilter()
	if err != nil {
		return nil, err
	}

	var possibleMatches []string

	for _, url := range urls {
		if url != "" && bf.Test([]byte(url)) {
			possibleMatches = append(possibleMatches, url)
		}
	}

	return possibleMatches, nil
}
