package query

import (
	"context"
	"fmt"
	"net/url"
	"sync"
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
		// Route DB confirmation by bloom match type:
		//   domain → ExistsByDomain (covers all subdomains)
		//   host   → ExistsByHost
		//   ip     → ExistsByIP
		//   other  → ExistsByHost (hostname from URL)
		confirmed := true
		if qs.repo != nil {
			confirmed = false
			for _, m := range matches {
				var exists bool
				var err error
				switch m.Type {
				case "domain":
					exists, err = qs.repo.ExistsByDomain(ctx, m.Key)
				case "host":
					exists, err = qs.repo.ExistsByHost(ctx, m.Key)
				case "ip":
					exists, err = qs.repo.ExistsByIP(ctx, m.Key)
				case "file", "full_url", "host_path":
					// Bloom keys for these types carry their own identity —
					// file by filename, host_path by host+path prefix, full_url by host+path+query.
					exists, err = qs.repo.ExistsByBloomType(ctx, m.Type, m.Key)
				default:
					host := hostname(urlStr)
					if host == "" {
						continue
					}
					exists, err = qs.repo.ExistsByHost(ctx, host)
				}
				if err == nil && exists {
					confirmed = true
					break
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
// Uses parallel goroutines with a semaphore to limit concurrency.
func (qs *QueryService) BulkCheck(ctx context.Context, urls []string) ([]LikelyResponse, error) {
	return bulkCheckParallel(ctx, qs, urls, 20) // 20 concurrent workers
}

// bulkCheckParallel runs Likely checks in parallel with bounded concurrency.
func bulkCheckParallel(ctx context.Context, qs *QueryService, urls []string, concurrency int) ([]LikelyResponse, error) {
	type result struct {
		resp LikelyResponse
		err  error
		i    int
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	results := make([]result, len(urls))
	resultChan := make(chan result, len(urls))

	for i, u := range urls {
		wg.Add(1)
		go func(u string, i int) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

		resp, err := qs.Likely(ctx, u)
		if err != nil {
			resultChan <- result{resp: LikelyResponse{}, err: err, i: i}
		} else {
			resultChan <- result{resp: *resp, err: err, i: i}
		}
		}(u, i)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for r := range resultChan {
		if r.err != nil {
			return nil, fmt.Errorf("bulk check url=%s: %w", urls[r.i], r.err)
		}
		results[r.i] = r
	}

	// Reassemble in original order
	out := make([]LikelyResponse, len(urls))
	for _, r := range results {
		out[r.i] = r.resp
	}
	return out, nil
}

// BulkHit performs full lookups (bloom + DB + score) for multiple URLs.
// Uses parallel goroutines with a semaphore to limit concurrency.
func (qs *QueryService) BulkHit(ctx context.Context, urls []string) ([]QueryResponse, error) {
	return bulkHitParallel(ctx, qs, urls, 20) // 20 concurrent workers
}

// bulkHitParallel runs Hit checks in parallel with bounded concurrency.
func bulkHitParallel(ctx context.Context, qs *QueryService, urls []string, concurrency int) ([]QueryResponse, error) {
	type result struct {
		resp QueryResponse
		err  error
		i    int
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	results := make([]result, len(urls))
	resultChan := make(chan result, len(urls))

	for i, u := range urls {
		wg.Add(1)
		go func(u string, i int) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

		resp, err := qs.Hit(ctx, u)
		if err != nil {
			resultChan <- result{resp: QueryResponse{}, err: err, i: i}
		} else {
			resultChan <- result{resp: *resp, err: err, i: i}
		}
		}(u, i)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for r := range resultChan {
		if r.err != nil {
			return nil, fmt.Errorf("bulk hit url=%s: %w", urls[r.i], r.err)
		}
		results[r.i] = r
	}

	// Reassemble in original order
	out := make([]QueryResponse, len(urls))
	for _, r := range results {
		out[r.i] = r.resp
	}
	return out, nil
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
