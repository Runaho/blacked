package cache

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"context"
	"strings"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
	"github.com/samber/lo"
)

func SyncBlacklistsToBadger(ctx context.Context, repo repository.BlacklistRepository) error {
	bdb := GetBadgerInstance()

	ch := make(chan entries.EntryStream)

	// Add debug logging before starting
	log.Debug().Msg("Starting to stream entries from repository")

	go func() {
		err := repo.StreamEntries(ctx, ch)
		if err != nil {
			log.Error().Err(err).Msg("Error while streaming entries")
		}
		log.Debug().Msg("Finished streaming entries")
	}()

	count := 0
	for {
		select {
		case <-ctx.Done():
			log.Debug().Int("processed_count", count).Msg("Sync interrupted by context cancellation")
			return ctx.Err()
		case entry, ok := <-ch:
			if !ok {
				log.Debug().Int("processed_count", count).Msg("Finished syncing blacklists to Badger")
				return nil
			}

			count++
			if count%100 == 0 {
				log.Trace().Int("processed_count", count).Msg("Processing blacklist entries")
			}

			// Make sure we're using the correct field name
			if err := UpsertEntryStream(bdb, entry); err != nil {
				log.Error().Err(err).Str("key", entry.SourceUrl).Msg("Failed to upsert entry")
				return err
			}
		}
	}

}

func UpsertEntryStream(bdb *badger.DB, entryStream entries.EntryStream) error {
	// Use Info level instead of Trace to make sure it's visible
	log.Trace().
		Str("source_url", entryStream.SourceUrl).
		Int("ids_count", len(entryStream.IDs)).
		Msg("Upserting entry stream")

	key := []byte(entryStream.SourceUrl)

	return bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)

		if err != nil {
			if err == badger.ErrKeyNotFound {
				IDs := []byte(strings.Join(entryStream.IDs, ","))
				return txn.Set(key, IDs)
			}
			return err
		}

		return item.Value(func(existingVal []byte) error {
			IDs := strings.Split(string(existingVal), ",")
			IDs = append(IDs, entryStream.IDs...)

			uniqIDs := lo.Uniq(IDs)
			formattedIDs := strings.Join(uniqIDs, ",")

			return txn.Set(key, []byte(formattedIDs))
		})
	})
}
