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

func SearchBlacklistEntryStream(sourceUrl string) (entries.EntryStream, error) {
	bdb := GetBadgerInstance()

	if config.GetConfig().Cache.UseBloom {
		isLikely, err := CheckURL(sourceUrl)
		log.Debug().Bool("is_likely", isLikely).Msg("Checked bloom filter")
		if err != nil {
			return entries.EntryStream{}, err
		}

		if !isLikely {
			return entries.EntryStream{}, ErrBloomKeyNotFound
		}
	}

	var entryStream entries.EntryStream

	err := bdb.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(sourceUrl))
		if err != nil {
			return err
		}
		err = item.Value(func(val []byte) error {
			entryStream.SourceUrl = sourceUrl
			entryStream.IDs = strings.Split(string(val), ",")
			return nil
		})

		return err
	})

	return entryStream, err
}
