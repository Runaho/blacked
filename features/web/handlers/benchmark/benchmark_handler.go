// Created via Claude 3.7 Sonnet
package benchmark

import (
	"blacked/features/cache"
	"blacked/features/cache/cache_errors"
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"blacked/features/entries/services"
	"blacked/features/web/handlers/response"
	"context"
	"errors"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// Error variables
var (
	ErrInvalidInput         = errors.New("invalid benchmark input")
	ErrValidationFailed     = errors.New("validation failed")
	ErrNoURLsProvided       = errors.New("no URLs provided for benchmark")
	Errcache_providerAccess = errors.New("error accessing cache_provider database")
	ErrBloomAccess          = errors.New("error accessing bloom filter")
	ErrRepositoryAccess     = errors.New("error accessing repository")
	ErrBenchmarkExecution   = errors.New("error executing benchmark")
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
	BloomOnlyTimeNs          int64 `json:"bloom_only_time_ns"`
	BloomResult              bool  `json:"bloom_result"`
	cache_providerOnlyTimeNs int64 `json:"cache_provider_only_time_ns"`
	FoundIncache_provider    bool  `json:"found_in_cache_provider"`
	RepositoryOnlyTimeNs     int64 `json:"repository_only_time_ns"`
	FoundInRepository        bool  `json:"found_in_repository"`

	// Complete flows (in nanoseconds)
	BloomThencache_providerTimeNs       int64 `json:"bloom_then_cache_provider_time_ns"`
	Bloomcache_providerRepositoryTimeNs int64 `json:"bloom_cache_provider_repository_time_ns"`
	cache_providerRepositoryTimeNs      int64 `json:"cache_provider_repository_time_ns"`

	// Summary
	FastestMethod string  `json:"fastest_method"`
	SpeedupFactor float64 `json:"speedup_factor"`
}

func (h *BenchmarkHandler) BenchmarkURL(c echo.Context) error {
	input := new(BenchmarkInput)
	if err := c.Bind(input); err != nil {
		log.Trace().Err(err).Msg("Failed to bind benchmark input")
		return response.BadRequest(c, "Failed to bind benchmark input")
	}

	if err := c.Validate(input); err != nil {
		log.Trace().Err(err).Msg("Validation error")
		return response.BadRequest(c, "Validation failed: ")
	}

	if len(input.URLs) == 0 {
		log.Trace().Msg("No URLs provided for benchmark")
		return response.BadRequest(c, "At least one URL must be provided")
	}

	ctx := c.Request().Context()

	// Run benchmark for each URL
	results := make([]BenchmarkResult, len(input.URLs))

	for i, url := range input.URLs {
		results[i] = h.benchmarkSingleURL(ctx, url, input.Iterations)
	}

	return response.Success(c, results)
}

func (h *BenchmarkHandler) benchmarkSingleURL(ctx context.Context, url string, iterations int) BenchmarkResult {
	result := BenchmarkResult{
		URL:        url,
		Iterations: iterations,
	}

	// 1. Benchmark Bloom Filter only
	var bloomTotalTime int64
	isLikely := false

	for range iterations {
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

	// 2. Benchmark cache_provider only
	var cache_providerTotalTime int64
	foundIncache_provider := false
	var entryStream entries.EntryStream

	for range iterations {
		start := time.Now()
		var err error
		entryStream, err = cache.GetEntryStream(url)

		cache_providerTotalTime += time.Since(start).Nanoseconds()

		if err == nil && len(entryStream.IDs) > 0 {
			foundIncache_provider = true
		}
	}

	result.cache_providerOnlyTimeNs = cache_providerTotalTime / int64(iterations) // Average nanoseconds
	result.FoundIncache_provider = foundIncache_provider

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

	// 4. Benchmark Bloom + cache_provider flow (no repository)
	var bloomcache_providerTotalTime int64

	for i := 0; i < iterations; i++ {
		start := time.Now()

		// First check bloom filter
		isLikely, _ = cache.CheckURL(url)

		// If likely, check cache_provider
		if isLikely {
			_, _ = cache.GetEntryStream(url)
		}

		bloomcache_providerTotalTime += time.Since(start).Nanoseconds()
	}

	result.BloomThencache_providerTimeNs = bloomcache_providerTotalTime / int64(iterations)

	// 5. Benchmark cache_provider + Repository flow (no bloom)
	var cache_providerRepoTotalTime int64

	for i := 0; i < iterations; i++ {
		start := time.Now()

		// First check cache_provider
		entryStream, err := cache.GetEntryStream(url)

		// If not found in cache_provider, check repository
		if err == cache_errors.ErrKeyNotFound || (err == nil && len(entryStream.IDs) == 0) {
			queryType := enums.QueryTypeFull
			_, _ = h.Service.Query(ctx, url, &queryType)
		}

		cache_providerRepoTotalTime += time.Since(start).Nanoseconds()
	}

	result.cache_providerRepositoryTimeNs = cache_providerRepoTotalTime / int64(iterations)

	// 6. Benchmark full flow (Bloom + cache_provider + Repository)
	var fullFlowTotalTime int64

	for range iterations {
		start := time.Now()

		// First check bloom filter
		isLikely, _ = cache.CheckURL(url)

		// If likely, check cache_provider
		if isLikely {
			entryStream, err := cache.GetEntryStream(url)

			// If not found in cache_provider, check repository
			if err == cache_errors.ErrKeyNotFound || (err == nil && len(entryStream.IDs) == 0) {
				queryType := enums.QueryTypeFull
				_, _ = h.Service.Query(ctx, url, &queryType)
			}
		}

		fullFlowTotalTime += time.Since(start).Nanoseconds()
	}

	result.Bloomcache_providerRepositoryTimeNs = fullFlowTotalTime / int64(iterations)

	// Determine fastest method and speedup factor
	methods := map[string]int64{
		"Repository Only":               result.RepositoryOnlyTimeNs,
		"cache_provider + Repository":   result.cache_providerRepositoryTimeNs,
		"Bloom + cache_provider":        result.BloomThencache_providerTimeNs,
		"Bloom + cache_provider + Repo": result.Bloomcache_providerRepositoryTimeNs,
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
