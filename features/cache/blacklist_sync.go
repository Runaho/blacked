package cache

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/config"
	"blacked/internal/db"
	"context"
	"unsafe"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
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
	txn := bdb.NewTransaction(true)

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
			if err := txn.Commit(); err != nil {
				log.Error().Err(err).Msg("Failed to commit transaction")
			}
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

			txn, err = UpsertEntryStream(bdb, txn, entry)

			if err != nil {
				log.Error().Err(err).Str("key", entry.SourceUrl).Msg("Failed to upsert entry")
				return err
			}
		}
	}
}

func UpsertEntryStream(db *badger.DB, txn *badger.Txn, entryStream entries.EntryStream) (*badger.Txn, error) {
	if log.Trace().Enabled() {
		log.Trace().
			Str("source_url", entryStream.SourceUrl).
			Int("ids_count", len(entryStream.IDs)).
			Msg("Upserting entry stream")
	}

	sourceKey := unsafe.Slice(unsafe.StringData(entryStream.SourceUrl), len(entryStream.SourceUrl))
	idsValue := unsafe.Slice(unsafe.StringData(entryStream.IDsRaw), len(entryStream.IDsRaw))

	err := txn.Set(sourceKey, idsValue)

	if err == badger.ErrTxnTooBig {
		if log.Debug().Enabled() {
			log.Debug().
				Str("key", entryStream.SourceUrl).
				Int("bytes", len(idsValue)).
				Msg("Transaction item limit reached, committing and retrying")
		}

		_ = txn.Commit()
		txn = db.NewTransaction(true)
		err = txn.Set(sourceKey, idsValue)

		if err == badger.ErrTxnTooBig {
			log.Error().Str("key", entryStream.SourceUrl).Msg("Transaction too big, skipping even after commit")
			return txn, err
		}

		return txn, nil
	}

	if err != nil && err != badger.ErrTxnTooBig {
		log.Error().Err(err).Str("key", entryStream.SourceUrl).Msg("Failed to set entry")
	}

	if log.Trace().Enabled() {
		log.Trace().
			Str("key", entryStream.SourceUrl).
			Int("bytes", len(idsValue)).
			Msg("Setting new key")
	}

	return txn, err
}
