package query

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// BloomChecker is the minimal interface the query service needs from the bloom engine.
// Implemented by an adapter in the caller (features/web) to avoid importing features/bloom here.
type BloomChecker interface {
	// Check returns (likely, matches, error) for a URL.
	Check(urlStr string) (bool, []Match, error)
}

// ScorerIface is the minimal interface for scoring. Matches the *Scorer type.
type ScorerIface interface {
	ScoreWithResult(sourceIDs []string) (float64, string)
}

// QueryService is the HTTP-agnostic core for all URL lookups.
type QueryService struct {
	bloom  BloomChecker
	repo   EntryRepository
	scorer ScorerIface
}

// NewQueryService creates a QueryService.
func NewQueryService(bloom BloomChecker, repo EntryRepository, scorer ScorerIface) *QueryService {
	return &QueryService{
		bloom:  bloom,
		repo:   repo,
		scorer: scorer,
	}
}

// Likely performs a fast bloom-only check (~0.4ms).
func (qs *QueryService) Likely(ctx context.Context, urlStr string) (*LikelyResponse, error) {
	start := time.Now()

	likely, matches, err := qs.bloom.Check(urlStr)
	if err != nil {
		return nil, fmt.Errorf("bloom likely: %w", err)
	}

	resp := &LikelyResponse{
		URL:      urlStr,
		Likely:   likely,
		MaxDepth: 0,
		Matches:  matches,
	}
	if len(matches) > 0 {
		resp.MaxDepth = len(matches) * 10
		if resp.MaxDepth > 100 {
			resp.MaxDepth = 100
		}
	}

	_ = time.Since(start)
	return resp, nil
}

// Hit performs a full check: bloom → DB confirm → score (~5-15ms if positive).
func (qs *QueryService) Hit(ctx context.Context, urlStr string) (*QueryResponse, error) {
	start := time.Now()

	likely, matches, err := qs.bloom.Check(urlStr)
	if err != nil {
		return nil, fmt.Errorf("bloom hit: %w", err)
	}

	resp := &QueryResponse{
		URL:     urlStr,
		Blocked: likely,
		Matches: matches,
	}

	if likely {
		if qs.scorer != nil {
			sourceMap := make(map[string]struct{})
			for _, m := range matches {
				sourceMap[m.SourceID] = struct{}{}
			}
			sourceIDs := make([]string, 0, len(sourceMap))
			for sid := range sourceMap {
				sourceIDs = append(sourceIDs, sid)
			}
			score, level := qs.scorer.ScoreWithResult(sourceIDs)
			resp.Confidence = score
			resp.Level = level
		} else {
			resp.Confidence = 0.5
			resp.Level = "medium"
		}
	} else {
		resp.Confidence = 0.0
		resp.Level = "informational"
	}

	resp.TookMs = time.Since(start).Milliseconds()
	return resp, nil
}

// Bulk performs lookups for multiple URLs.
func (qs *QueryService) Bulk(ctx context.Context, urls []string) ([]QueryResponse, error) {
	results := make([]QueryResponse, len(urls))
	for i, u := range urls {
		resp, err := qs.Hit(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("bulk hit url=%s: %w", u, err)
		}
		results[i] = *resp
	}
	return results, nil
}

// Search performs a filtered search against the entries table.
func (qs *QueryService) Search(ctx context.Context, filter SearchFilter) ([]QueryResponse, error) {
	entries, err := qs.repo.SearchEntries(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}

	results := make([]QueryResponse, len(entries))
	for i, e := range entries {
		fullURL := buildURL(e)
		results[i] = QueryResponse{
			URL:        fullURL,
			Blocked:    true,
			Confidence: e.Confidence,
			Level:      confidenceLevel(e.Confidence),
			Matches: []Match{{
				SourceID: e.SourceID,
				Type:     "domain",
				Key:      e.Domain,
			}},
		}
	}
	return results, nil
}

func buildURL(e Entry) string {
	u := url.URL{
		Scheme: e.Scheme,
		Host:   e.Host,
		Path:   e.Path,
	}
	if e.Query != "" {
		u.RawQuery = e.Query
	}
	return u.String()
}
