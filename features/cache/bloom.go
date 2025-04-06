package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

var (
	bloomFilter                  *bloom.BloomFilter
	ErrBloomFilterNotInitialized = errors.New("Bloom filter not initialized")
)

func GetBloomFilter() (*bloom.BloomFilter, error) {
	if bloomFilter == nil {
		return nil, ErrBloomFilterNotInitialized
	}
	return bloomFilter, nil
}

func BuildBloomFilterFromBadger(ctx context.Context, cacheDB *badger.DB, keyCount int) error {

	if keyCount < 1000 {
		keyCount = 1000
	}

	bloomFilter = bloom.NewWithEstimates(uint(keyCount), 0.01)

	log.Info().
		Int("badger_keys", keyCount).
		Uint("bloom_capacity", bloomFilter.Cap()).
		Uint("hash_functions", bloomFilter.K()).
		Msg("Created bloom filter")

	return PopulateBloomFilterFromBadger(ctx, cacheDB)
}
func PopulateBloomFilterFromBadger(ctx context.Context, cacheDB *badger.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Hour)
	defer cancel()

	bf, e := GetBloomFilter()
	if e != nil {
		return e
	}

	keyCount := 0
	startTime := time.Now()

	log.Info().Msg("Starting to populate bloom filter with Badger DB keys")

	err := cacheDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // We only need keys, not values

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Add key to bloom filter
				k := it.Item().Key()
				bf.Add(k)
				keyCount++

				// Log progress periodically
				if keyCount%100000 == 0 {
					elapsed := time.Since(startTime)
					rate := float64(keyCount) / elapsed.Seconds()
					log.Trace().
						Int("keys_added", keyCount).
						Dur("elapsed", elapsed).
						Float64("keys_per_second", rate).
						Msg("Building bloom filter - progress")
				}
			}
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Int("keys_processed", keyCount).Msg("Error populating bloom filter")
		return fmt.Errorf("failed to populate bloom filter: %w", err)
	}

	duration := time.Since(startTime)
	log.Info().
		Int("total_keys", keyCount).
		Dur("duration", duration).
		Float64("keys_per_second", float64(keyCount)/duration.Seconds()).
		Msg("Bloom filter population completed")

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
