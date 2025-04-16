package badger_provider

import (
	"blacked/features/cache/cache_errors"
	"context"
	"strings"
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

// BadgerProvider implements the EntryCache interface using Badger
type BadgerProvider struct {
	db          *badger.DB
	bloomFilter *bloom.BloomFilter
	bloomMutex  sync.RWMutex
	initialized bool
	txn         *badger.Txn
}

// NewBadgerProvider creates a new Badger provider
func NewBadgerProvider() *BadgerProvider {
	return &BadgerProvider{}
}

// Initialize sets up the Badger instance
func (p *BadgerProvider) Initialize(ctx context.Context) error {
	if p.initialized {
		return nil
	}

	opts := badger.DefaultOptions("").WithInMemory(true)

	db, err := badger.Open(opts)
	if err != nil {
		log.Error().Err(err).Msg("Failed to open Badger database")
		return err
	}

	p.db = db
	p.initialized = true
	log.Info().Msg("Badger initialized successfully")

	return nil
}

// Close releases Badger resources
func (p *BadgerProvider) Close() error {
	if p.db != nil {
		err := p.db.Close()
		p.db = nil
		p.initialized = false
		return err
	}
	return nil
}

// Get retrieves IDs associated with a key
func (p *BadgerProvider) Get(key string) ([]string, error) {
	if !p.initialized {
		return nil, cache_errors.ErrCacheNotInitialized
	}

	var value []byte
	var ids []string

	err := p.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return cache_errors.ErrKeyNotFound
			}
			return err
		}

		return item.Value(func(val []byte) error {
			value = make([]byte, len(val))
			copy(value, val)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	if len(value) > 0 {
		ids = strings.Split(string(value), ",")
	}

	return ids, nil
}

// Set stores IDs associated with a key
func (p *BadgerProvider) Set(key string, ids string) error {
	if !p.initialized {
		return cache_errors.ErrCacheNotInitialized
	}

	if p.txn == nil {
		p.txn = p.db.NewTransaction(true)
	}

	err := p.txn.Set([]byte(key), []byte(ids))

	if err == badger.ErrTxnTooBig {
		if log.Debug().Enabled() {
			log.Debug().
				Str("key", key).
				Int("bytes", len(ids)).
				Msg("Transaction item limit reached, committing and retrying")
		}

		_ = p.txn.Commit()
		p.txn = p.db.NewTransaction(true)
		err = p.txn.Set([]byte(key), []byte(ids))

		if err == badger.ErrTxnTooBig {
			log.Error().
				Str("key", key).
				Msg("Transaction too big, skipping even after commit")
			p.txn.Discard()
			p.txn = nil
			return err
		}

		return nil
	}

	return err
}

func (p *BadgerProvider) Commit() error {
	if p.txn != nil {
		err := p.txn.Commit()
		p.txn = nil
		return err
	}
	return nil
}

// Delete removes a key from the cache
func (p *BadgerProvider) Delete(key string) error {
	if !p.initialized {
		return cache_errors.ErrCacheNotInitialized
	}

	return p.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}

func (p *BadgerProvider) Iterate(ctx context.Context, fn func(key string) error) error {
	if !p.initialized {
		return cache_errors.ErrCacheNotInitialized
	}

	return p.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 1000
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())
			if err := fn(key); err != nil {
				return err
			}
		}
		return nil
	})
}
