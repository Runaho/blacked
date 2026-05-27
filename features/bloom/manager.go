package bloom

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
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
		sets: make(map[BloomType]*BloomSet, 7), // capacity for all bloom types
	}

	allTypes := []BloomType{
		BloomDomain, BloomHost, BloomHostPath,
		BloomFile, BloomFullURL, BloomLogin, BloomIP,
	}

	for _, t := range allTypes {
		bm.sets[t] = NewBloomSet(t, expectedItemsPerSet, 0)
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

// PopulateEntry writes a source entry into exactly one bloom type.
// The target is determined by determineBloomTarget — each entry lives in a
// single bloom type (the most specific one the provider gave us).
func (bm *BloomManager) PopulateEntry(sourceID string, keys *URLKeys) {
	bt, key := determineBloomTarget(keys)
	if bt == "" || key == "" {
		return
	}

	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bs, ok := bm.sets[bt]; ok && bs != nil {
		bs.Add(sourceID, key)
	}
}

// determineBloomTarget picks the single bloom type for a provider entry.
// Decision tree (most specific first):
//  1. IP address → IP bloom (IP is absolute, path irrelevant)
//  2. File + Query → FullURL (most specific)
//  3. File (extension present) → File bloom
//  4. HostPath (no file extension) → HostPath bloom
//  5. Host only (subdomain ≠ domain) → Host bloom
//  6. Bare domain (host == domain) → Domain bloom
func determineBloomTarget(keys *URLKeys) (BloomType, string) {
	// 0. IP → IP bloom (takes priority — IP is absolute identification)
	if keys.IP != "" {
		return BloomIP, keys.IP
	}
	// 1. File + Query → FullURL
	if keys.File != "" && path.Ext(keys.File) != "" && keys.Query != "" && keys.Host != "" && keys.Path != "" {
		return BloomFullURL, keys.Host + keys.Path + "?" + keys.Query
	}

	// 2. File → File bloom
	if keys.File != "" && path.Ext(keys.File) != "" && keys.Host != "" && keys.Path != "" {
		return BloomFile, keys.File
	}

	// 3. HostPath (no file extension)
	if keys.HostPath != "" {
		return BloomHostPath, keys.HostPath
	}

	// 4. Bare domain: host == domain (no subdomain)
	if keys.Host != "" && keys.Domain != "" && keys.Host == keys.Domain {
		return BloomDomain, keys.Domain
	}

	// 5. Host only (subdomain present)
	if keys.Host != "" {
		return BloomHost, keys.Host
	}

	// 6. Domain only (fallback)
	if keys.Domain != "" {
		return BloomDomain, keys.Domain
	}

	// 7. IP
	if keys.IP != "" {
		return BloomIP, keys.IP
	}

	return "", ""
}

// Likely checks a URL against all applicable bloom types in parallel.
// Check order: Domain → Host → HostPath → File → FullURL.
// First hit wins — other goroutines are cancelled via errgroup context.
func (bm *BloomManager) Likely(urlStr string) (*BloomResult, error) {
	keys, err := ParseURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	checkKeys := keys.GenerateCheckKeys()
	if len(checkKeys) == 0 {
		return &BloomResult{Likely: false, Matches: nil}, nil
	}

	bm.mu.RLock()
	defer bm.mu.RUnlock()

	// errgroup with context for true first-hit-wins semantics
	g, ctx := errgroup.WithContext(context.Background())

	var (
		resultMu sync.Mutex
		matches  []BloomMatch
	)

	// Spawn goroutines for each check key
	for _, ck := range checkKeys {
		ck := ck // capture loop variable
		g.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err() // cancelled by another goroutine
			default:
			}

			bs, ok := bm.sets[ck.Type]
			if !ok || bs == nil {
				return nil // no set for this type, continue checking others
			}

			if !bs.Test(ck.Key) {
				return nil // not a match, continue checking others
			}

			// Hit — find source and record result
			for _, sid := range bs.GetSourceIDs() {
				if bs.TestSource(sid, ck.Key) {
					resultMu.Lock()
					matches = append(matches, BloomMatch{
						Type:     ck.Type,
						SourceID: sid,
						Key:      ck.Key,
					})
					resultMu.Unlock()

					// Signal cancellation to other goroutines
					return errors.New("match found")
				}
			}
			return nil
		})
	}

	// Wait for all goroutines. We ignore the error since our sentinel
	// "match found" error is just for cancellation signaling.
	_ = g.Wait()

	if len(matches) > 0 {
		result := &BloomResult{
			Likely:  true,
			Matches: matches,
		}
		for _, m := range matches {
			if w, ok := DepthWeight[m.Type]; ok {
				scaled := int(w * 100)
				if scaled > result.MaxDepth {
					result.MaxDepth = scaled
				}
			}
		}
		return result, nil
	}

	return &BloomResult{Likely: false, Matches: nil}, nil
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

	// Re-add each entry via PopulateEntry (single-bloom logic)
	for _, e := range entries {
		keys := entryToKeys(e)
		bt, key := determineBloomTarget(keys)
		if bt == "" || key == "" {
			continue
		}
		if bs, ok := bm.sets[bt]; ok && bs != nil {
			bs.Add(sourceID, key)
		}
	}

	log.Info().
		Str("source_id", sourceID).
		Int("entries", len(entries)).
		Msg("Rebuilt source bloom filters")

	return nil
}

// entryToKeys converts an Entry into URLKeys.
func entryToKeys(e Entry) *URLKeys {
	return EntryToKeys(e)
}

// EntryToKeys converts an Entry into URLKeys. Public for use outside the bloom package.
// Uses stored entry fields (Domain, Host, Path, RawQuery) to reconstruct bloom keys
// without re-parsing the URL — saves ~310MB alloc during provider sync.
func EntryToKeys(e Entry) *URLKeys {
	hp := ""
	if e.Host != "" && e.Path != "" && e.Path != "/" {
		hp = e.Host + e.Path
	}

	base := path.Base(e.Path)	
	file := ""
	if base != "" && base != "/" && base != "." {
		if ext := path.Ext(base); ext != "" && len(ext) > 1 {
			file = base
		}
	}

	return &URLKeys{
		Domain:   e.Domain,
		Host:     e.Host,
		HostPath: hp,
		Path:     e.Path,
		File:     file,
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
