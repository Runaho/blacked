package benchmark

import (
	"blacked/features/cache"
	"blacked/features/entries/enums"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

type BenchmarkReport struct {
	Meta struct {
		URLsTested      int     `json:"urls_tested"`
		Iterations      int     `json:"iterations_per_url"`
		TotalQueries    int     `json:"total_queries_executed"`
		ExecutionTimeNs int64   `json:"execution_time_ns"`
		ExecutionTimeMs float64 `json:"execution_time_ms"`
	} `json:"meta"`

	Methods []MethodPerformance `json:"methods"`

	Summary struct {
		FastestMethod  string  `json:"fastest_method"`
		SlowestMethod  string  `json:"slowest_method"`
		SpeedupFactor  float64 `json:"speedup_factor"` // Fastest vs slowest
		Recommendation string  `json:"recommendation"`
	} `json:"summary"`

	URLDetails []URLPerformance `json:"url_details,omitempty"`
}

type MethodPerformance struct {
	Name          string   `json:"name"`
	AverageTimeNs int64    `json:"average_time_ns"`
	AverageTimeMs float64  `json:"average_time_ms"` // For human readability
	TotalTimeNs   int64    `json:"total_time_ns"`
	HitCount      int      `json:"hit_count"`      // Number of URLs found in blacklist
	MissCount     int      `json:"miss_count"`     // Number of URLs not found
	RelativeSpeed float64  `json:"relative_speed"` // 1.0 is baseline (fastest)
	Components    []string `json:"components"`     // Which components were used
}

type URLPerformance struct {
	URL           string                 `json:"url"`
	MethodResults []MethodURLPerformance `json:"method_results"`
	InBlacklist   bool                   `json:"in_blacklist"`
	FastestMethod string                 `json:"fastest_method_for_url"`
}

type MethodURLPerformance struct {
	Method        string  `json:"method"`
	AverageTimeNs int64   `json:"average_time_ns"`
	AverageTimeMs float64 `json:"average_time_ms"` // For human readability
	InBlacklist   bool    `json:"found_in_blacklist"`
}

// CompareAllMethods runs a comprehensive benchmark comparing all methods
func (h *BenchmarkHandler) CompareAllMethods(c echo.Context) error {
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

	ctx := c.Request().Context()

	startTime := time.Now()
	report := h.runComprehensiveBenchmark(ctx, input)
	executionTime := time.Since(startTime)

	report.Meta.ExecutionTimeNs = executionTime.Nanoseconds()
	report.Meta.ExecutionTimeMs = float64(executionTime.Nanoseconds()) / 1_000_000.0

	return c.JSON(http.StatusOK, report)
}

func (h *BenchmarkHandler) runComprehensiveBenchmark(ctx context.Context, input *BenchmarkInput) BenchmarkReport {
	report := BenchmarkReport{}
	report.Meta.URLsTested = len(input.URLs)
	report.Meta.Iterations = input.Iterations
	report.Meta.TotalQueries = len(input.URLs) * input.Iterations * 6 // 6 methods being tested

	// Define all methods to test
	methods := []struct {
		name       string
		components []string
		execFunc   func(context.Context, string) (bool, time.Duration)
	}{
		{
			name:       "Repository Only",
			components: []string{"Repository"},
			execFunc:   h.benchmarkRepositoryOnly,
		},
		{
			name:       "Badger Only",
			components: []string{"Badger"},
			execFunc:   h.benchmarkBadgerOnly,
		},
		{
			name:       "Bloom Only (Check Only)",
			components: []string{"Bloom"},
			execFunc:   h.benchmarkBloomOnly,
		},
		{
			name:       "Badger + Repository",
			components: []string{"Badger", "Repository"},
			execFunc:   h.benchmarkBadgerRepo,
		},
		{
			name:       "Bloom + Badger",
			components: []string{"Bloom", "Badger"},
			execFunc:   h.benchmarkBloomBadger,
		},
		{
			name:       "Bloom + Badger + Repository (Full Flow)",
			components: []string{"Bloom", "Badger", "Repository"},
			execFunc:   h.benchmarkFullFlow,
		},
	}

	// Initialize method performance data
	methodData := make([]MethodPerformance, len(methods))
	for i, m := range methods {
		methodData[i] = MethodPerformance{
			Name:       m.name,
			Components: m.components,
		}
	}

	// Initialize URL performance data
	urlData := make([]URLPerformance, len(input.URLs))
	for i, url := range input.URLs {
		urlData[i] = URLPerformance{
			URL:           url,
			MethodResults: make([]MethodURLPerformance, len(methods)),
		}
	}

	// Run benchmark for each URL and method
	for urlIdx, url := range input.URLs {
		for methodIdx, method := range methods {
			totalDuration := int64(0)
			hitCount := 0

			for i := 0; i < input.Iterations; i++ {
				found, duration := method.execFunc(ctx, url)
				totalDuration += duration.Nanoseconds()
				if found {
					hitCount++
				}
			}

			// Calculate average for this URL and method
			avgDurationNs := totalDuration / int64(input.Iterations)
			avgDurationMs := float64(avgDurationNs) / 1_000_000.0

			// Update method summary data
			methodData[methodIdx].TotalTimeNs += totalDuration
			if hitCount > input.Iterations/2 { // Consider as hit if found more than 50% of the time
				methodData[methodIdx].HitCount++
			} else {
				methodData[methodIdx].MissCount++
			}

			// Update URL-specific data
			urlData[urlIdx].MethodResults[methodIdx] = MethodURLPerformance{
				Method:        method.name,
				AverageTimeNs: avgDurationNs,
				AverageTimeMs: avgDurationMs,
				InBlacklist:   hitCount > input.Iterations/2,
			}

			// Update URL blacklist status based on repository results (most authoritative)
			if method.name == "Repository Only" && hitCount > input.Iterations/2 {
				urlData[urlIdx].InBlacklist = true
			}
		}
	}

	// Calculate averages for method data
	for i := range methodData {
		totalUrls := int64(len(input.URLs))
		if totalUrls > 0 {
			methodData[i].AverageTimeNs = methodData[i].TotalTimeNs / totalUrls
			methodData[i].AverageTimeMs = float64(methodData[i].AverageTimeNs) / 1_000_000.0
		}
	}

	// Find fastest method for each URL
	for i := range urlData {
		var fastestTime int64 = 0
		fastestMethod := ""

		for j, result := range urlData[i].MethodResults {
			if fastestTime == 0 || result.AverageTimeNs < fastestTime {
				fastestTime = result.AverageTimeNs
				fastestMethod = methods[j].name
			}
		}

		urlData[i].FastestMethod = fastestMethod
	}

	// Find fastest and slowest methods overall
	var fastestIdx int = 0
	var slowestIdx int = 0
	for i := range methodData {
		if i == 0 || methodData[i].AverageTimeNs < methodData[fastestIdx].AverageTimeNs {
			fastestIdx = i
		}
		if i == 0 || methodData[i].AverageTimeNs > methodData[slowestIdx].AverageTimeNs {
			slowestIdx = i
		}
	}

	// Calculate relative speeds
	fastestTime := methodData[fastestIdx].AverageTimeNs
	if fastestTime > 0 {
		for i := range methodData {
			methodData[i].RelativeSpeed = float64(methodData[i].AverageTimeNs) / float64(fastestTime)
		}
	} else {
		for i := range methodData {
			methodData[i].RelativeSpeed = 1.0
		}
	}

	// Fill in summary data
	report.Methods = methodData
	report.URLDetails = urlData
	report.Summary.FastestMethod = methodData[fastestIdx].Name
	report.Summary.SlowestMethod = methodData[slowestIdx].Name

	// Calculate speedup factor (prevent division by zero)
	if methodData[fastestIdx].AverageTimeNs > 0 && methodData[slowestIdx].AverageTimeNs > 0 {
		report.Summary.SpeedupFactor = float64(methodData[slowestIdx].AverageTimeNs) / float64(methodData[fastestIdx].AverageTimeNs)
	} else {
		report.Summary.SpeedupFactor = 1.0
	}

	// Add recommendation based on results
	foundUrlsPercent := 0.0
	if len(input.URLs) > 0 {
		foundUrlsPercent = float64(methodData[fastestIdx].HitCount) / float64(len(input.URLs)) * 100.0
	}

	// Create human-readable recommendation
	if methodData[fastestIdx].Name == "Bloom + Badger + Repository (Full Flow)" {
		report.Summary.Recommendation = fmt.Sprintf(
			"The full flow with Bloom + Badger + Repository is the most efficient, with a %.2fx speedup over %s. "+
				"Average query time: %.3f ms (%.0f ns). "+
				"This configuration is optimal for both hits (%.1f%% of URLs) and misses.",
			report.Summary.SpeedupFactor,
			report.Summary.SlowestMethod,
			methodData[fastestIdx].AverageTimeMs,
			float64(methodData[fastestIdx].AverageTimeNs),
			foundUrlsPercent,
		)
	} else if methodData[fastestIdx].Name == "Bloom Only (Check Only)" {
		report.Summary.Recommendation = fmt.Sprintf(
			"Bloom filter checks alone are fastest at %.3f ms (%.0f ns), but remember these are just preliminary checks. "+
				"For a production system, the complete flow with verification is recommended.",
			methodData[fastestIdx].AverageTimeMs,
			float64(methodData[fastestIdx].AverageTimeNs),
		)
	} else if methodData[fastestIdx].Name == "Bloom + Badger" {
		report.Summary.Recommendation = fmt.Sprintf(
			"Using Bloom + Badger without repository queries is fastest at %.3f ms (%.0f ns), with a %.2fx speedup. "+
				"This suggests your cache is working effectively for the tested URLs.",
			methodData[fastestIdx].AverageTimeMs,
			float64(methodData[fastestIdx].AverageTimeNs),
			report.Summary.SpeedupFactor,
		)
	} else {
		report.Summary.Recommendation = fmt.Sprintf(
			"The %s method is fastest for your workload at %.3f ms (%.0f ns). "+
				"This is unexpected - typically the full flow with bloom filter should be fastest. "+
				"Consider reviewing your cache configuration.",
			report.Summary.FastestMethod,
			methodData[fastestIdx].AverageTimeMs,
			float64(methodData[fastestIdx].AverageTimeNs),
		)
	}

	// Log summary of benchmark
	log.Info().
		Int("urls_tested", len(input.URLs)).
		Int("iterations", input.Iterations).
		Str("fastest_method", report.Summary.FastestMethod).
		Float64("fastest_avg_ms", methodData[fastestIdx].AverageTimeMs).
		Int64("fastest_avg_ns", methodData[fastestIdx].AverageTimeNs).
		Float64("speedup_factor", report.Summary.SpeedupFactor).
		Msg("Benchmark completed")

	return report
}

// Individual benchmark methods

func (h *BenchmarkHandler) benchmarkRepositoryOnly(ctx context.Context, url string) (bool, time.Duration) {
	start := time.Now()

	queryType := enums.QueryTypeFull
	hits, _ := h.Service.Query(ctx, url, &queryType)

	duration := time.Since(start)
	return len(hits) > 0, duration
}

func (h *BenchmarkHandler) benchmarkBadgerOnly(ctx context.Context, url string) (bool, time.Duration) {
	start := time.Now()

	entryStream, _ := cache.SearchBlacklistEntryStream(url)

	duration := time.Since(start)
	return len(entryStream.IDs) > 0, duration
}

func (h *BenchmarkHandler) benchmarkBloomOnly(ctx context.Context, url string) (bool, time.Duration) {
	start := time.Now()

	isLikely, _ := cache.CheckURL(url)

	duration := time.Since(start)
	return isLikely, duration
}

func (h *BenchmarkHandler) benchmarkBadgerRepo(ctx context.Context, url string) (bool, time.Duration) {
	start := time.Now()

	// First check badger
	entryStream, err := cache.SearchBlacklistEntryStream(url)
	if err != nil && err != badger.ErrKeyNotFound && err != cache.ErrBloomKeyNotFound {
		return false, time.Since(start)
	}

	found := len(entryStream.IDs) > 0

	// If not found in badger, check repository
	if !found {
		queryType := enums.QueryTypeFull
		hits, _ := h.Service.Query(ctx, url, &queryType)
		found = len(hits) > 0
	}

	duration := time.Since(start)
	return found, duration
}

func (h *BenchmarkHandler) benchmarkBloomBadger(ctx context.Context, url string) (bool, time.Duration) {
	start := time.Now()

	// First check bloom
	isLikely, err := cache.CheckURL(url)
	if err != nil {
		return false, time.Since(start)
	}

	found := false

	// If bloom says it might be there, check badger
	if isLikely {
		entryStream, _ := cache.SearchBlacklistEntryStream(url)
		found = len(entryStream.IDs) > 0
	}

	duration := time.Since(start)
	return found, duration
}

func (h *BenchmarkHandler) benchmarkFullFlow(ctx context.Context, url string) (bool, time.Duration) {
	start := time.Now()

	// First check bloom
	isLikely, err := cache.CheckURL(url)
	if err != nil {
		return false, time.Since(start)
	}

	found := false

	// If bloom says it might be there, check badger
	if isLikely {
		entryStream, err := cache.SearchBlacklistEntryStream(url)

		if err != nil && err != badger.ErrKeyNotFound && err != cache.ErrBloomKeyNotFound {
			return false, time.Since(start)
		}

		found = len(entryStream.IDs) > 0

		// If not found in badger, check repository
		if !found {
			queryType := enums.QueryTypeFull
			hits, _ := h.Service.Query(ctx, url, &queryType)
			found = len(hits) > 0
		}
	}

	duration := time.Since(start)
	return found, duration
}
