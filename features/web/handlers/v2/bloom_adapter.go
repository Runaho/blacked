package v2

import (
	"blacked/features/bloom"
	"blacked/internal/query"
)

// bloomAdapter adapts bloom.BloomManager to query.BloomChecker.
// Lives in the web layer so that internal/query stays HTTP-free.
type bloomAdapter struct {
	mgr *bloom.BloomManager
}

func NewBloomAdapter(mgr *bloom.BloomManager) query.BloomChecker {
	return &bloomAdapter{mgr: mgr}
}

func (ba *bloomAdapter) Check(urlStr string) (bool, []query.Match, error) {
	result, err := ba.mgr.Likely(urlStr)
	if err != nil {
		return false, nil, err
	}
	matches := make([]query.Match, 0, len(result.Matches))
	for _, m := range result.Matches {
		matches = append(matches, query.Match{
			SourceID: m.SourceID,
			Type:     string(m.Type),
			Key:      m.Key,
		})
	}
	return result.Likely, matches, nil
}
