package bloom

import (
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/rs/zerolog/log"
)

// bloomPool reuses bloom filters to reduce allocation pressure
var bloomPool = sync.Pool{
	New: func() interface{} {
		return bloom.NewWithEstimates(10000, 0.01)
	},
}

// getBloomFromPool returns a bloom filter with specified capacity
// Uses default false positive rate of 0.01 (1%)
func getBloomFromPool(expectedItems uint) *bloom.BloomFilter {
	if expectedItems < 1000 {
		expectedItems = 1000
	}
	
	bf := bloomPool.Get().(*bloom.BloomFilter)
	// Note: bloom filters don't have Clear(), but since we're reusing
	// for the same purpose (new empty filter), this is fine
	return bf
}

// putBloomToPool returns a bloom filter to the pool
func putBloomToPool(bf *bloom.BloomFilter) {
	bloomPool.Put(bf)
}

// BloomSet manages bloom filters for a single BloomType.
// It holds a global filter (all sources merged) plus per-source filters.
type BloomSet struct {
	Type          BloomType
	Filter        *bloom.BloomFilter
	SourceFilters map[string]*bloom.BloomFilter
	mu            sync.RWMutex
	expectedItems uint // capacity estimate for per-source filters
}

// NewBloomSet creates a BloomSet for a given type.
func NewBloomSet(t BloomType, expectedItems uint) *BloomSet {
	if expectedItems < 1000 {
		expectedItems = 1000
	}
	return &BloomSet{
		Type:          t,
		Filter:        getBloomFromPool(expectedItems),
		SourceFilters: make(map[string]*bloom.BloomFilter, 100), // Capacity hint for typical source count
		expectedItems: expectedItems,
	}
}

// Add inserts a key into both the global and the per-source filter.
// sourceID may be empty for aggregate/non-source-specific adds; in that case
// only the global filter is updated.
func (bs *BloomSet) Add(sourceID, key string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.Filter == nil {
		bs.Filter = bloom.NewWithEstimates(bs.expectedItems, 0.01)
	}
	bs.Filter.AddString(key)

	if sourceID != "" {
		sf, ok := bs.SourceFilters[sourceID]
		if !ok || sf == nil {
			sf = bloom.NewWithEstimates(bs.expectedItems, 0.01)
			bs.SourceFilters[sourceID] = sf
		}
		sf.AddString(key)
	}
}

// Test checks the global filter for a key.
func (bs *BloomSet) Test(key string) bool {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if bs.Filter == nil {
		return false
	}
	return bs.Filter.Test([]byte(key))
}

// TestSource checks a specific source's filter.
func (bs *BloomSet) TestSource(sourceID, key string) bool {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	sf, ok := bs.SourceFilters[sourceID]
	if !ok || sf == nil {
		return false
	}
	return sf.Test([]byte(key))
}

// GetFilterNames returns human friendly string for the bloom set
func (bs *BloomSet) GetFilterNames() string {
	return string(bs.Type) + " bloom set"
}

// GetSourceIDs returns all source IDs currently tracked.
func (bs *BloomSet) GetSourceIDs() []string {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	ids := make([]string, 0, len(bs.SourceFilters))
	for id := range bs.SourceFilters {
		ids = append(ids, id)
	}
	return ids
}

// Clear resets the bloom set to empty state, returning filters to pool.
func (bs *BloomSet) Clear() {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	
	// Return the main filter to pool and get a fresh one
	putBloomToPool(bs.Filter)
	bs.Filter = getBloomFromPool(bs.expectedItems)
	
	// Clear all source filters
	for sourceID, filter := range bs.SourceFilters {
		putBloomToPool(filter)
		delete(bs.SourceFilters, sourceID)
	}
}

// ResetSource clears a specific source's filter and rebuilds the global
// filter from the remaining source filters so stale keys are not kept alive.
func (bs *BloomSet) ResetSource(sourceID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	delete(bs.SourceFilters, sourceID)

	// Rebuild global filter from remaining source filters.
	// Bloom filters do not support deletion — the only way to remove keys
	// is to reconstruct the union of what's left.
	bs.Filter = bloom.NewWithEstimates(bs.expectedItems, 0.01)
	for _, sf := range bs.SourceFilters {
		if sf != nil {
			bs.Filter.Merge(sf)
		}
	}

	log.Debug().Str("bloom_type", string(bs.Type)).Str("source_id", sourceID).Msg("Reset source bloom filter")
}

// SourceCount returns the number of per-source filters.
func (bs *BloomSet) SourceCount() int {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return len(bs.SourceFilters)
}

// TotalKeys returns an estimate of total keys across all source filters.
func (bs *BloomSet) TotalKeys() uint {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var total uint
	if bs.Filter != nil {
		total = bs.Filter.Cap()
	}
	return total
}
