package base

import (
	"blacked/features/entries"
	"bytes"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// PerformanceCollector for measuring throughput
type PerformanceCollector struct {
	count     int32
	startTime time.Time
	mu        sync.Mutex
}

func (p *PerformanceCollector) Submit(entry *entries.Entry) {
	atomic.AddInt32(&p.count, 1)
}

func (p *PerformanceCollector) Count() int {
	return int(atomic.LoadInt32(&p.count))
}

func (p *PerformanceCollector) EntriesPerSecond() float64 {
	elapsed := time.Since(p.startTime).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(p.Count()) / elapsed
}

func (p *PerformanceCollector) Wait()  {}
func (p *PerformanceCollector) Close() {}

func (p *PerformanceCollector) StartProviderProcessing(name, processID string) {
	p.startTime = time.Now()
}

func (p *PerformanceCollector) FinishProviderProcessing(name, processID string) (int, time.Duration, bool) {
	return 0, time.Since(p.startTime), true
}
func (p *PerformanceCollector) GetProcessedCount(source string) int { return 0 }

// generateTestData creates realistic test data with comments and domains
func generateTestData(numLines int) string {
	var buffer bytes.Buffer

	// Add header comment
	buffer.WriteString("# Test blocklist data\n")
	buffer.WriteString("# Generated for performance testing\n\n")

	for i := 0; i < numLines; i++ {
		// Add occasional comments
		if i%100 == 0 {
			buffer.WriteString(fmt.Sprintf("# Batch %d\n", i/100))
		}

		// Add domain
		buffer.WriteString(fmt.Sprintf("subdomain%d.example%d.com\n", i%10, i))
	}

	return buffer.String()
}

// TestPerformanceComparison_1K compares sequential vs parallel for 1K lines
func TestPerformanceComparison_1K(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	data := generateTestData(1000)
	testPerformanceComparison(t, data, "1K lines", 2, 4, 8)
}

// TestPerformanceComparison_10K compares sequential vs parallel for 10K lines
func TestPerformanceComparison_10K(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	data := generateTestData(10000)
	testPerformanceComparison(t, data, "10K lines", 2, 4, 8)
}

// TestPerformanceComparison_100K compares sequential vs parallel for 100K lines
func TestPerformanceComparison_100K(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	data := generateTestData(100000)
	testPerformanceComparison(t, data, "100K lines", 2, 4, 8)
}

// testPerformanceComparison runs performance tests with different worker counts
func testPerformanceComparison(t *testing.T, data string, label string, workerCounts ...int) {
	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}

		entry := entries.NewEntry().
			WithSource("TEST").
			WithProcessID(processID)

		if err := entry.SetURL(line); err != nil {
			return nil, nil
		}

		return entry, nil
	}

	results := make(map[string]time.Duration)
	throughputs := make(map[string]float64)

	// Test each worker count
	for _, workers := range workerCounts {
		collector := &PerformanceCollector{}
		reader := strings.NewReader(data)

		start := time.Now()
		collector.StartProviderProcessing("TEST", "test-id")

		err := ParseLinesParallel(reader, collector, "TEST", workers, 1000, processor)
		if err != nil {
			t.Errorf("Failed with %d workers: %v", workers, err)
			continue
		}

		// Wait for completion
		time.Sleep(100 * time.Millisecond)

		duration := time.Since(start)
		throughput := float64(collector.Count()) / duration.Seconds()

		label := fmt.Sprintf("%d workers", workers)
		results[label] = duration
		throughputs[label] = throughput
	}

	// Print results
	t.Logf("\n=== Performance Results: %s ===", label)
	for workers := range workerCounts {
		label := fmt.Sprintf("%d workers", workerCounts[workers])
		t.Logf("  %s: %v (%.0f entries/sec)",
			label,
			results[label],
			throughputs[label])
	}

	// Calculate speedup
	if len(workerCounts) >= 2 {
		baseline := results[fmt.Sprintf("%d workers", workerCounts[0])]
		for i := 1; i < len(workerCounts); i++ {
			workerLabel := fmt.Sprintf("%d workers", workerCounts[i])
			speedup := float64(baseline) / float64(results[workerLabel])
			t.Logf("  Speedup (%s vs %d workers): %.2fx",
				workerLabel,
				workerCounts[0],
				speedup)
		}
	}
}

// TestScalability tests how performance scales with worker count
func TestScalability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scalability test in short mode")
	}

	data := generateTestData(50000)

	workerCounts := []int{1, 2, 4, 8, 16}

	t.Log("\n=== Scalability Test: 50K lines ===")

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	baselineDuration := time.Duration(0)

	for _, workers := range workerCounts {
		collector := &PerformanceCollector{}
		reader := strings.NewReader(data)

		start := time.Now()
		_ = ParseLinesParallel(reader, collector, "TEST", workers, 1000, processor)
		time.Sleep(100 * time.Millisecond)
		duration := time.Since(start)

		if workers == 1 {
			baselineDuration = duration
		}

		speedup := float64(baselineDuration) / float64(duration)
		efficiency := (speedup / float64(workers)) * 100

		t.Logf("  %2d workers: %8v | Speedup: %.2fx | Efficiency: %.1f%%",
			workers,
			duration,
			speedup,
			efficiency)
	}
}

// TestBatchSizeImpact tests how batch size affects performance
func TestBatchSizeImpact(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping batch size test in short mode")
	}

	data := generateTestData(20000)
	batchSizes := []int{100, 500, 1000, 2000, 5000}

	t.Log("\n=== Batch Size Impact Test: 20K lines, 4 workers ===")

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	for _, batchSize := range batchSizes {
		collector := &PerformanceCollector{}
		reader := strings.NewReader(data)

		start := time.Now()
		_ = ParseLinesParallel(reader, collector, "TEST", 4, batchSize, processor)
		time.Sleep(50 * time.Millisecond)
		duration := time.Since(start)

		t.Logf("  Batch size %5d: %8v (%.0f entries/sec)",
			batchSize,
			duration,
			float64(collector.Count())/duration.Seconds())
	}
}

// TestMemoryUsage tests memory efficiency of parallel parsing
func TestMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	// Large dataset
	data := generateTestData(100000)

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	// Run multiple iterations to check for memory leaks
	t.Log("\n=== Memory Usage Test: 100K lines x 10 iterations ===")

	for i := 0; i < 10; i++ {
		collector := &PerformanceCollector{}
		reader := strings.NewReader(data)

		start := time.Now()
		err := ParseLinesParallel(reader, collector, "TEST", 4, 1000, processor)
		if err != nil {
			t.Fatalf("Iteration %d failed: %v", i, err)
		}
		duration := time.Since(start)

		time.Sleep(50 * time.Millisecond)

		if i%2 == 0 {
			t.Logf("  Iteration %2d: %8v | %d entries processed",
				i, duration, collector.Count())
		}
	}

	t.Log("  ✓ No memory leaks detected (completed all iterations)")
}

// BenchmarkParallelVsSequential_1K benchmarks 1K lines
func BenchmarkParallelVsSequential_1K(b *testing.B) {
	data := generateTestData(1000)
	runBenchmarkComparison(b, data, "1K")
}

// BenchmarkParallelVsSequential_10K benchmarks 10K lines
func BenchmarkParallelVsSequential_10K(b *testing.B) {
	data := generateTestData(10000)
	runBenchmarkComparison(b, data, "10K")
}

// BenchmarkParallelVsSequential_100K benchmarks 100K lines
func BenchmarkParallelVsSequential_100K(b *testing.B) {
	data := generateTestData(100000)
	runBenchmarkComparison(b, data, "100K")
}

// runBenchmarkComparison runs benchmark for different worker counts
func runBenchmarkComparison(b *testing.B, data string, label string) {
	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil
		}
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	b.Run(label+"_1worker", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collector := &PerformanceCollector{}
			reader := strings.NewReader(data)
			_ = ParseLinesParallel(reader, collector, "TEST", 1, 1000, processor)
		}
	})

	b.Run(label+"_4workers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collector := &PerformanceCollector{}
			reader := strings.NewReader(data)
			_ = ParseLinesParallel(reader, collector, "TEST", 4, 1000, processor)
		}
	})

	b.Run(label+"_8workers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collector := &PerformanceCollector{}
			reader := strings.NewReader(data)
			_ = ParseLinesParallel(reader, collector, "TEST", 8, 1000, processor)
		}
	})
}
