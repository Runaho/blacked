package query

import (
	"context"
	"fmt"
	"net/url"
)

// BloomChecker is the minimal interface the query service needs from the bloom engine.
// Implemented by an adapter in the caller (features/web) to avoid importing features/bloom here.
type BloomChecker interface {
	// Check returns (likely, matches, error) for a URL.
	Check(urlStr string) (bool, []Match, error)
}

// ScorerIface is the minimal interface for scoring. Matches the *Scorer type.
type ScorerIface interface {
	Score(matches []Match) (float64, string)
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

// Likely performs a fast bloom-only check.
func (qs *QueryService) Likely(ctx context.Context, urlStr string) (*LikelyResponse, error) {
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
		resp.MaxDepth = min(len(matches)*10, 100)
	}

	return resp, nil
}

// Hit performs a full check: bloom → DB confirmation → scorer.
func (qs *QueryService) Hit(ctx context.Context, urlStr string) (*QueryResponse, error) {
	likely, matches, err := qs.bloom.Check(urlStr)
	if err != nil {
		return nil, fmt.Errorf("bloom hit: %w", err)
	}

	resp := &QueryResponse{
		URL:     urlStr,
		Blocked: false,
		Matches: matches,
	}

	if likely {
		// Bloom says "yes" — confirm with DB if a repository is available.
		// When repo is nil (tests), trust the bloom directly.
		confirmed := true
		if qs.repo != nil {
			host := hostname(urlStr)
			if host != "" {
				exists, err := qs.repo.ExistsByHost(ctx, host)
				if err != nil || !exists {
					confirmed = false
				}
			}
		}

		if confirmed {
			resp.Blocked = true

			if qs.scorer != nil {
				// Use Score(matches) for full depth-weighted formula:
				// confidence = Σ(trust_score × depth_weight) / Σ(trust_score)
				score, level := qs.scorer.Score(matches)
				resp.Confidence = score
				resp.Level = level
			} else {
				resp.Confidence = 0.5
				resp.Level = "medium"
			}
		} else {
			// Bloom positive, DB negative → false positive. Not blocked.
			resp.Blocked = false
			resp.Confidence = 0.0
			resp.Level = "informational"
		}
	} else {
		resp.Confidence = 0.0
		resp.Level = "informational"
	}

	return resp, nil
}

// hostname extracts the hostname from a URL string.
func hostname(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// BulkCheck performs fast bloom-only checks for multiple URLs (~0.4ms per URL).
func (qs *QueryService) BulkCheck(ctx context.Context, urls []string) ([]LikelyResponse, error) {
	results := make([]LikelyResponse, len(urls))
	for i, u := range urls {
		resp, err := qs.Likely(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("bulk check url=%s: %w", u, err)
		}
		results[i] = *resp
	}
	return results, nil
}

// BulkHit performs full lookups (bloom + DB + score) for multiple URLs.
func (qs *QueryService) BulkHit(ctx context.Context, urls []string) ([]QueryResponse, error) {
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
	return u.String()
}
