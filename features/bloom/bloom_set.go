package bloom

import (
	"sync"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/rs/zerolog/log"
)

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
		Filter:        bloom.NewWithEstimates(expectedItems, 0.01),
		SourceFilters: make(map[string]*bloom.BloomFilter),
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
	if bs == nil {
		return false
	}
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if bs.Filter == nil {
		return false
	}
	return bs.Filter.Test([]byte(key))
}

// TestSource checks a specific source's filter.
func (bs *BloomSet) TestSource(sourceID, key string) bool {
	if bs == nil {
		return false
	}
	
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if bs.SourceFilters == nil {
		return false
	}
	
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
	if bs == nil {
		return []string{}
	}
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	ids := make([]string, 0, len(bs.SourceFilters))
	for id := range bs.SourceFilters {
		ids = append(ids, id)
	}
	return ids
}

// ResetSource clears a specific source's filter and rebuilds the global
// filter from the remaining source filters so stale keys are not kept alive.
// Uses double-buffering pattern to avoid race conditions during rebuild.
func (bs *BloomSet) ResetSource(sourceID string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	delete(bs.SourceFilters, sourceID)

	// Rebuild global filter from remaining source filters using double-buffering.
	// Bloom filters do not support deletion — the only way to remove keys
	// is to reconstruct the union of what's left.
	newFilter := bloom.NewWithEstimates(bs.expectedItems, 0.01)
	for _, sf := range bs.SourceFilters {
		if sf != nil {
			newFilter.Merge(sf)
		}
	}
	// Atomic swap - readers will see either old or new filter, never partial state
	bs.Filter = newFilter

	log.Debug().Str("bloom_type", string(bs.Type)).Str("source_id", sourceID).Msg("Reset source bloom filter")
}

// SourceCount returns the number of per-source filters.
func (bs *BloomSet) SourceCount() int {
	if bs == nil {
		return 0
	}
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return len(bs.SourceFilters)
}

// TotalKeys returns an estimate of total keys across all source filters.
func (bs *BloomSet) TotalKeys() uint {
	if bs == nil {
		return 0
	}
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	var total uint
	if bs.Filter != nil {
		total = bs.Filter.Cap()
	}
	return total
}
