package cache

import (
	"blacked/features/entries"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

func SearchBlacklistEntryStream(sourceUrl string) (entries.EntryStream, error) {
	bdb := GetBadgerInstance()

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
