package cache

import (
	"blacked/features/cache/cache_errors"
	"blacked/features/entries"
	"blacked/features/entries/services"
	"blacked/internal/config"
	"context"
	"errors"

	"github.com/rs/zerolog/log"
)

var (
	ErrBloomKeyNotFound = errors.New("key not found in bloom filter")
)

func GetEntryStream(sourceUrl string) (entryStream entries.EntryStream, err error) {
	cacheProvider, err := GetCacheProvider()
	entryStream.SourceUrl = sourceUrl

	if err != nil {
		log.Err(err).Msg("Failed to connect to badger instance for memory cache")
		return
	}

	if config.GetConfig().Cache.UseBloom {
		isLikely, err := CheckURL(sourceUrl)
		log.Debug().Bool("is_likely", isLikely).Msg("Checked bloom filter")
		if err != nil {
			return entryStream, err
		}

		if !isLikely {
			return entryStream, ErrBloomKeyNotFound
		}
	}

	ids, err := cacheProvider.Get(sourceUrl)

	if err != nil {
		if err == cache_errors.ErrKeyNotFound {
			log.Warn().
				Str("source_url", sourceUrl).
				Msg("Key not found in cache")

			querySerivce, err := services.NewQueryService()
			if err != nil {
				log.Err(err).Msg("Failed to create query service")
				return entryStream, err
			}

			entryStream.IDs, err = querySerivce.GetIdsByLink(context.Background(), sourceUrl)
			if err != nil {
				log.Err(err).Msg("Failed to query blacklist entries")
				return entryStream, err
			}

			err = cacheProvider.SetIds(sourceUrl, entryStream.IDs)
			cacheProvider.Commit()

			return entryStream, err
		}

		log.Err(err).
			Str("source_url", sourceUrl).
			Msg("Failed to get item from cache")

		return
	}

	return entries.EntryStream{
		SourceUrl: sourceUrl,
		IDs:       ids,
	}, nil
}
