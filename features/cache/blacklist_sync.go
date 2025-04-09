package cache

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/config"
	"blacked/internal/db"
	"context"
	"strings"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
	"github.com/samber/lo"
)

var (
	concurrencyLock = make(chan struct{}, 1)
	queueLock       = make(chan struct{}, 1)
)

//  1. FireAndForgetSync is the entry point for the sync process.
//     It tries to occupy the single "waiting" slot and spawns a goroutine
//     that will block until it can become the active request (if/when the active one finishes).
//     If the queue is full, it drops the request.
//
// This is a non-blocking function.
func FireAndForgetSync() {
	// Try to occupy the single "waiting" slot if the sync is already in progress
	select {
	case queueLock <- struct{}{}:
		// We got into the queue. Now spawn a goroutine that will block until
		// it can become the active request (if/when the active one finishes).
		go runSyncWhenActive()
	default:
		// queueLock was full => there's already 1 active + 1 waiting => drop
		return
	}
}

// runSyncWhenActive attempts to grab concurrencyLock (the “active slot”).
// If an active sync is running, it blocks until that finishes.
func runSyncWhenActive() {
	// Block here until we can place a token in concurrencyLock
	// => i.e. until the currently-running request (if any) completes.
	concurrencyLock <- struct{}{}

	// Now we are the active worker => remove ourselves from the queue
	<-queueLock

	// Actually run the sync
	SyncBlacklistsToBadger(context.Background()) // or pass a context if you prefer

	// Release the active slot so the next “waiting” request can proceed
	<-concurrencyLock
}

func SyncBlacklistsToBadger(ctx context.Context) error {
	_db, err := db.GetDB()
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to database")
		return err
	}

	repo := repository.NewSQLiteRepository(_db)
	bdb, err := GetBadgerInstance()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get badger instance")
		return err
	}

	count := 0

	ch := make(chan entries.EntryStream)

	log.Debug().Msg("Starting to stream entries from repository")

	go func() {
		err := repo.StreamEntries(ctx, ch)
		if err != nil {
			log.Error().Err(err).Msg("Error while streaming entries")
		}
		log.Debug().Msg("Finished streaming entries")
	}()

	for {
		select {
		case <-ctx.Done():
			log.Debug().Int("processed_count", count).Msg("Sync interrupted by context cancellation")
			return ctx.Err()
		case entry, ok := <-ch:
			if !ok {
				log.Debug().Int("processed_count", count).Msg("Finished syncing blacklists to Badger")

				if config.GetConfig().Cache.UseBloom {
					if err := BuildBloomFilterFromBadger(ctx, bdb, count); err != nil {
						log.Error().Err(err).Msg("Failed to build bloom filter")
						return err
					}
				}

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
	log.Trace().
		Str("source_url", entryStream.SourceUrl).
		Int("new_ids_count", len(entryStream.IDs)).
		Msg("Upserting entry stream")

	key := []byte(entryStream.SourceUrl)

	return bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)

		if err == badger.ErrKeyNotFound {

			if len(entryStream.IDs) == 0 {
				return nil
			}

			var builder strings.Builder
			for i, id := range entryStream.IDs {
				builder.WriteString(id)
				if i < len(entryStream.IDs)-1 {
					builder.WriteByte(',')
				}
			}
			valBytes := []byte(builder.String())
			log.Trace().Str("key", entryStream.SourceUrl).Int("bytes", len(valBytes)).Msg("Setting new key")
			return txn.Set(key, valBytes)
		}
		if err != nil {
			return err
		}

		return item.Value(func(existingVal []byte) error {
			if len(existingVal) == 0 && len(entryStream.IDs) == 0 {
				return nil
			}

			var existingIDs []string
			if len(existingVal) > 0 {
				existingIDs = strings.Split(string(existingVal), ",")
			}

			estimatedSize := len(existingIDs) + len(entryStream.IDs)
			seenIDs := make(map[string]struct{}, estimatedSize)

			var builder strings.Builder
			builder.Grow(len(existingVal) + len(entryStream.IDs)*10)

			first := true

			for _, id := range existingIDs {
				if id == "" {
					continue
				}
				if _, seen := seenIDs[id]; !seen {
					seenIDs[id] = struct{}{}
					if !first {
						builder.WriteByte(',')
					}
					builder.WriteString(id)
					first = false
				}
			}

			for _, id := range entryStream.IDs {
				if id == "" {
					continue
				}
				if _, seen := seenIDs[id]; !seen {
					seenIDs[id] = struct{}{}
					if !first {
						builder.WriteByte(',')
					}
					builder.WriteString(id)
					first = false
				}
			}

			finalValBytes := []byte(builder.String())

			if string(finalValBytes) == string(existingVal) {
				log.Trace().Str("key", entryStream.SourceUrl).Msg("Skipping set, value unchanged")
				return nil
			}

			log.Trace().Str("key", entryStream.SourceUrl).Int("old_bytes", len(existingVal)).Int("new_bytes", len(finalValBytes)).Int("unique_ids", len(seenIDs)).Msg("Setting updated key")
			return txn.Set(key, finalValBytes)
		})
	})
}

func UpsertEntryStreamOld(bdb *badger.DB, entryStream entries.EntryStream) error {
	// Use Info level instead of Trace to make sure it's visible
	log.Trace().
		Str("source_url", entryStream.SourceUrl).
		Int("ids_count", len(entryStream.IDs)).
		Msg("Upserting entry stream")

	key := []byte(entryStream.SourceUrl)

	return bdb.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)

		if err == badger.ErrKeyNotFound {

			if len(entryStream.IDs) == 0 {
				return nil
			}

			var builder strings.Builder
			for i, id := range entryStream.IDs {
				builder.WriteString(id)
				if i < len(entryStream.IDs)-1 {
					builder.WriteByte(',')
				}
			}

			valBytes := []byte(builder.String())

			log.Trace().Str("key", entryStream.SourceUrl).Int("bytes", len(valBytes)).Msg("Setting new key")
			return txn.Set(key, valBytes)
		}

		if err != nil {
			return err
		}

		return item.Value(func(existingVal []byte) error {

			if len(existingVal) == 0 && len(entryStream.IDs) == 0 {
				return nil
			}

			IDs := strings.Split(string(existingVal), ",")
			IDs = append(IDs, entryStream.IDs...)

			uniqIDs := lo.Uniq(IDs)
			formattedIDs := strings.Join(uniqIDs, ",")

			return txn.Set(key, []byte(formattedIDs))
		})
	})
}
