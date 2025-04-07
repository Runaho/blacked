package cache

import (
	"blacked/features/entries"
	"blacked/internal/config"
	"errors"
	"strings"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

var (
	ErrBloomKeyNotFound = errors.New("key not found in bloom filter")
)

func SearchBlacklistEntryStream(sourceUrl string) (entryStream entries.EntryStream, err error) {
	bdb, err := GetBadgerInstance()
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

	err = bdb.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(sourceUrl))
		if err != nil {
			return err
		}
		err = item.Value(func(val []byte) error {
			entryStream = entries.EntryStream{
				SourceUrl: sourceUrl,
				IDs:       strings.Split(string(val), ","),
			}
			return nil
		})

		return err
	})

	return
}
