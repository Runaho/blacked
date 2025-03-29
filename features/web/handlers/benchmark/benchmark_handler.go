package benchmark

import (
	"blacked/features/cache"
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/features/entries/services"
	"context"
	"net/http"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

type BenchmarkHandler struct {
	Service *services.QueryService
}

func NewBenchmarkHandler(service *services.QueryService) *BenchmarkHandler {
	return &BenchmarkHandler{Service: service}
}

type BenchmarkInput struct {
	URLs       []string `json:"urls" validate:"required"`
	Iterations int      `json:"iterations" validate:"required,min=1,max=1000"`
}

type BenchmarkResult struct {
	URL        string `json:"url"`
	Iterations int    `json:"iterations"`

	// Component timings (in nanoseconds)
	BloomOnlyTimeNs      int64 `json:"bloom_only_time_ns"`
	BloomResult          bool  `json:"bloom_result"`
	BadgerOnlyTimeNs     int64 `json:"badger_only_time_ns"`
	FoundInBadger        bool  `json:"found_in_badger"`
	RepositoryOnlyTimeNs int64 `json:"repository_only_time_ns"`
	FoundInRepository    bool  `json:"found_in_repository"`

	// Complete flows (in nanoseconds)
	BloomThenBadgerTimeNs       int64 `json:"bloom_then_badger_time_ns"`
	BloomBadgerRepositoryTimeNs int64 `json:"bloom_badger_repository_time_ns"`
	BadgerRepositoryTimeNs      int64 `json:"badger_repository_time_ns"`

	// Summary
	FastestMethod string  `json:"fastest_method"`
	SpeedupFactor float64 `json:"speedup_factor"`
}

func (h *BenchmarkHandler) BenchmarkURL(c echo.Context) error {
	input := new(BenchmarkInput)
	if err := c.Bind(input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error":   "Failed to parse benchmark input",
			"details": err.Error(),
		})
	}

	if err := c.Validate(input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"validation_error": err.Error(),
		})
	}

	if len(input.URLs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "At least one URL must be provided",
		})
	}

	ctx := c.Request().Context()

	// Run benchmark for each URL
	results := make([]BenchmarkResult, len(input.URLs))

	for i, url := range input.URLs {
		results[i] = h.benchmarkSingleURL(ctx, url, input.Iterations)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"results": results,
	})
}

func (h *BenchmarkHandler) benchmarkSingleURL(ctx context.Context, url string, iterations int) BenchmarkResult {
	result := BenchmarkResult{
		URL:        url,
		Iterations: iterations,
	}

	// 1. Benchmark Bloom Filter only
	var bloomTotalTime int64
	isLikely := false

	for i := 0; i < iterations; i++ {
		start := time.Now()
		var err error
		isLikely, err = cache.CheckURL(url)
		if err != nil {
			log.Error().Err(err).Str("url", url).Msg("Error checking bloom filter")
		}
		bloomTotalTime += time.Since(start).Nanoseconds()
	}

	result.BloomOnlyTimeNs = bloomTotalTime / int64(iterations) // Average nanoseconds
	result.BloomResult = isLikely

	// 2. Benchmark Badger only
	var badgerTotalTime int64
	foundInBadger := false
	var entryStream entries.EntryStream

	for i := 0; i < iterations; i++ {
		start := time.Now()
		var err error
		entryStream, err = cache.SearchBlacklistEntryStream(url)

		badgerTotalTime += time.Since(start).Nanoseconds()

		if err == nil && len(entryStream.IDs) > 0 {
			foundInBadger = true
		}
	}

	result.BadgerOnlyTimeNs = badgerTotalTime / int64(iterations) // Average nanoseconds
	result.FoundInBadger = foundInBadger

	// 3. Benchmark Repository only
	var repoTotalTime int64
	foundInRepo := false

	for i := 0; i < iterations; i++ {
		start := time.Now()
		queryType := enums.QueryTypeFull
		hits, err := h.Service.Query(ctx, url, &queryType)

		repoTotalTime += time.Since(start).Nanoseconds()

		if err == nil && len(hits) > 0 {
			foundInRepo = true
		}
	}

	result.RepositoryOnlyTimeNs = repoTotalTime / int64(iterations) // Average nanoseconds
	result.FoundInRepository = foundInRepo

	// 4. Benchmark Bloom + Badger flow (no repository)
	var bloomBadgerTotalTime int64

	for i := 0; i < iterations; i++ {
		start := time.Now()

		// First check bloom filter
		isLikely, _ = cache.CheckURL(url)

		// If likely, check badger
		if isLikely {
			_, _ = cache.SearchBlacklistEntryStream(url)
		}

		bloomBadgerTotalTime += time.Since(start).Nanoseconds()
	}

	result.BloomThenBadgerTimeNs = bloomBadgerTotalTime / int64(iterations)

	// 5. Benchmark Badger + Repository flow (no bloom)
	var badgerRepoTotalTime int64

	for i := 0; i < iterations; i++ {
		start := time.Now()

		// First check badger
		entryStream, err := cache.SearchBlacklistEntryStream(url)

		// If not found in badger, check repository
		if err == badger.ErrKeyNotFound || (err == nil && len(entryStream.IDs) == 0) {
			queryType := enums.QueryTypeFull
			_, _ = h.Service.Query(ctx, url, &queryType)
		}

		badgerRepoTotalTime += time.Since(start).Nanoseconds()
	}

	result.BadgerRepositoryTimeNs = badgerRepoTotalTime / int64(iterations)

	// 6. Benchmark full flow (Bloom + Badger + Repository)
	var fullFlowTotalTime int64

	for i := 0; i < iterations; i++ {
		start := time.Now()

		// First check bloom filter
		isLikely, _ = cache.CheckURL(url)

		// If likely, check badger
		if isLikely {
			entryStream, err := cache.SearchBlacklistEntryStream(url)

			// If not found in badger, check repository
			if err == badger.ErrKeyNotFound || (err == nil && len(entryStream.IDs) == 0) {
				queryType := enums.QueryTypeFull
				_, _ = h.Service.Query(ctx, url, &queryType)
			}
		}

		fullFlowTotalTime += time.Since(start).Nanoseconds()
	}

	result.BloomBadgerRepositoryTimeNs = fullFlowTotalTime / int64(iterations)

	// Determine fastest method and speedup factor
	methods := map[string]int64{
		"Repository Only":       result.RepositoryOnlyTimeNs,
		"Badger + Repository":   result.BadgerRepositoryTimeNs,
		"Bloom + Badger":        result.BloomThenBadgerTimeNs,
		"Bloom + Badger + Repo": result.BloomBadgerRepositoryTimeNs,
	}

	// Find fastest and slowest methods
	var fastest int64 = result.RepositoryOnlyTimeNs
	var slowest int64 = result.RepositoryOnlyTimeNs
	fastestMethod := "Repository Only"

	for method, time := range methods {
		if time < fastest {
			fastest = time
			fastestMethod = method
		}
		if time > slowest {
			slowest = time
		}
	}

	result.FastestMethod = fastestMethod

	// Calculate speedup factor (avoid division by zero)
	if fastest > 0 {
		result.SpeedupFactor = float64(slowest) / float64(fastest)
	} else {
		result.SpeedupFactor = 1.0
	}

	return result
}
