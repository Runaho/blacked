package bloom

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// ErrManagerNotReady is returned when the bloom manager has no initialized BloomSets.
var ErrManagerNotReady = errors.New("bloom manager not initialized")

// SourceEntryStream provides entries for a specific source.
type SourceEntryStream interface {
	StreamEntriesBySource(ctx context.Context, sourceID string) ([]Entry, error)
}

// BloomManager manages all BloomSets, one per BloomType.
// It is safe for concurrent use.
type BloomManager struct {
	sets map[BloomType]*BloomSet
	mu   sync.RWMutex
}

// NewBloomManager creates a manager with all supported BloomSets.
func NewBloomManager(expectedItemsPerSet uint) *BloomManager {
	bm := &BloomManager{
		sets: make(map[BloomType]*BloomSet),
	}

	allTypes := []BloomType{
		BloomDomain, BloomHost, BloomHostPath, BloomPath,
		BloomQuery, BloomFile, BloomLogin, BloomIP,
	}

	for _, t := range allTypes {
		bm.sets[t] = NewBloomSet(t, expectedItemsPerSet)
	}

	return bm
}

// GetSet returns a BloomSet by type, or nil if not found.
func (bm *BloomManager) GetSet(t BloomType) *BloomSet {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.sets[t]
}

// Sets returns all types currently managed.
func (bm *BloomManager) Sets() []BloomType {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	types := make([]BloomType, 0, len(bm.sets))
	for t := range bm.sets {
		types = append(types, t)
	}
	return types
}

// Add inserts an entry's components into the appropriate bloom filters.
func (bm *BloomManager) Add(sourceID string, keys *URLKeys, bloomTypes []BloomType) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	typed := keys.KeysFor(bloomTypes)
	for t, key := range typed {
		if bs, ok := bm.sets[t]; ok {
			bs.Add(sourceID, key)
		}
	}
}

// Likely checks a URL against all applicable bloom types and returns matches.
// On cold start (no filters ready), it returns Likely=false and empty matches.
func (bm *BloomManager) Likely(urlStr string) (*BloomResult, error) {
	keys, err := ParseURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	checkTypes := []BloomType{
		BloomDomain, BloomHost, BloomHostPath, BloomPath,
		BloomQuery, BloomFile, BloomLogin, BloomIP,
	}

	result := &BloomResult{
		Likely:  false,
		Matches: make([]BloomMatch, 0),
	}

	bm.mu.RLock()
	defer bm.mu.RUnlock()

	for _, t := range checkTypes {
		key := ""
		switch t {
		case BloomDomain:
			key = keys.Domain
		case BloomHost:
			key = keys.Host
		case BloomHostPath:
			key = keys.HostPath
		case BloomPath:
			key = keys.Path
		case BloomQuery:
			key = keys.Query
		case BloomFile:
			key = keys.File
		case BloomLogin:
			key = keys.Login
		case BloomIP:
			key = keys.IP
		}
		if key == "" {
			continue
		}

		bs, ok := bm.sets[t]
		if !ok || bs == nil {
			continue
		}

		if bs.Test(key) {
			result.Likely = true
			// Determine which sources match via per-source filters
			for _, sid := range bs.GetSourceIDs() {
				if bs.TestSource(sid, key) {
					result.Matches = append(result.Matches, BloomMatch{
						Type:     t,
						SourceID: sid,
						Key:      key,
					})
				}
			}
		}
	}

	if len(result.Matches) > 0 {
		for _, m := range result.Matches {
			if w, ok := DepthWeight[m.Type]; ok {
				scaled := int(w * 100)
				if scaled > result.MaxDepth {
					result.MaxDepth = scaled
				}
			}
		}
	}

	return result, nil
}

// RebuildSource rebuilds only the bloom filters for a specific source.
func (bm *BloomManager) RebuildSource(
	ctx context.Context,
	sourceID string,
	stream SourceEntryStream,
	bloomTypes []BloomType,
) error {
	entries, err := stream.StreamEntriesBySource(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("fetch entries for source %s: %w", sourceID, err)
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Reset this source from all BloomSets
	for _, bs := range bm.sets {
		bs.ResetSource(sourceID)
	}

	// Re-add each entry
	for _, e := range entries {
		keys := entryToKeys(e)
		typed := keys.KeysFor(bloomTypes)
		for t, key := range typed {
			if bs, ok := bm.sets[t]; ok && bs != nil {
				bs.Add(sourceID, key)
			}
		}
	}

	log.Info().
		Str("source_id", sourceID).
		Int("entries", len(entries)).
		Int("bloom_types", len(bloomTypes)).
		Msg("Rebuilt source bloom filters")

	return nil
}

// entryToKeys converts an Entry into URLKeys.
func entryToKeys(e Entry) *URLKeys {
	hp := ""
	if e.Host != "" && e.Path != "" && e.Path != "/" {
		hp = e.Host + e.Path
	}
	return &URLKeys{
		Domain:   e.Domain,
		Host:     e.Host,
		HostPath: hp,
		Path:     e.Path,
		File:     e.File,
		Query:    e.Query,
		Login:    e.Login,
		IP:       e.IP,
	}
}

// ColdStart returns true if no BloomSets have any entries.
func (bm *BloomManager) ColdStart() bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	for _, bs := range bm.sets {
		if bs != nil && bs.SourceCount() > 0 {
			return false
		}
	}
	return true
}

// Stats returns per-set stats for debugging.
func (bm *BloomManager) Stats() map[string]int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	stats := make(map[string]int, len(bm.sets))
	for t, bs := range bm.sets {
		if bs != nil {
			stats[string(t)] = bs.SourceCount()
		} else {
			stats[string(t)] = 0
		}
	}
	return stats
}
