package base

import (
	"blacked/features/entries"
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockCollector implements entry_collector.Collector for testing
type MockCollector struct {
	entries []*entries.Entry
	mu      sync.Mutex
	count   int32
}

func (m *MockCollector) Submit(entry *entries.Entry) {
	atomic.AddInt32(&m.count, 1)
	m.mu.Lock()
	m.entries = append(m.entries, entry)
	m.mu.Unlock()
}

func (m *MockCollector) GetEntries() []*entries.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries
}

func (m *MockCollector) Count() int {
	return int(atomic.LoadInt32(&m.count))
}

func (m *MockCollector) Wait()                                          {}
func (m *MockCollector) Close()                                         {}
func (m *MockCollector) StartProviderProcessing(name, processID string) {}
func (m *MockCollector) FinishProviderProcessing(name, processID string) (int, time.Duration, bool) {
	return 0, 0, true
}
func (m *MockCollector) GetProcessedCount(source string) int { return 0 }

// TestParseLinesParallel_BasicFunctionality tests that parallel parsing works correctly
func TestParseLinesParallel_BasicFunctionality(t *testing.T) {
	collector := &MockCollector{}

	// Create sample data
	data := `example1.com
example2.com
example3.com
example4.com
example5.com`

	reader := strings.NewReader(data)

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" {
			return nil, nil
		}

		entry := entries.NewEntry().
			WithSource("TEST").
			WithProcessID(processID)

		if err := entry.SetURL(line); err != nil {
			return nil, err
		}

		return entry, nil
	}

	err := ParseLinesParallel(reader, collector, "TEST", 2, 2, processor)
	require.NoError(t, err)

	// Wait a bit for all goroutines to finish
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 5, collector.Count(), "Should process all 5 lines")
}

// TestParseLinesParallel_EmptyInput tests handling of empty input
func TestParseLinesParallel_EmptyInput(t *testing.T) {
	collector := &MockCollector{}
	reader := strings.NewReader("")

	processor := func(line, processID string) (*entries.Entry, error) {
		return nil, nil
	}

	err := ParseLinesParallel(reader, collector, "TEST", 2, 10, processor)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, collector.Count(), "Should process 0 lines from empty input")
}

// TestParseLinesParallel_SkipsComments tests that comments are skipped
func TestParseLinesParallel_SkipsComments(t *testing.T) {
	collector := &MockCollector{}

	data := `# This is a comment
example1.com
# Another comment
example2.com

example3.com`

	reader := strings.NewReader(data)

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil, nil // Skip
		}

		entry := entries.NewEntry().
			WithSource("TEST").
			WithProcessID(processID)

		if err := entry.SetURL(line); err != nil {
			return nil, nil // Skip invalid
		}

		return entry, nil
	}

	err := ParseLinesParallel(reader, collector, "TEST", 2, 3, processor)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 3, collector.Count(), "Should only process 3 valid domain lines")
}

// TestParseLinesParallel_LargeDataset tests with a larger dataset
func TestParseLinesParallel_LargeDataset(t *testing.T) {
	collector := &MockCollector{}

	// Generate 10,000 lines
	var buffer bytes.Buffer
	expectedCount := 10000
	for i := 1; i <= expectedCount; i++ {
		buffer.WriteString(fmt.Sprintf("example%d.com\n", i))
	}

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" {
			return nil, nil
		}

		entry := entries.NewEntry().
			WithSource("TEST").
			WithProcessID(processID)

		// Simple domain assignment without full URL parsing for speed
		entry.Domain = line

		return entry, nil
	}

	start := time.Now()
	err := ParseLinesParallel(&buffer, collector, "TEST", 4, 1000, processor)
	duration := time.Since(start)

	require.NoError(t, err)

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	t.Logf("Processed %d lines in %v", collector.Count(), duration)
	assert.Equal(t, expectedCount, collector.Count(), "Should process all lines")
}

// TestParseLinesParallel_ConcurrentSafety tests that parallel processing is thread-safe
func TestParseLinesParallel_ConcurrentSafety(t *testing.T) {
	collector := &MockCollector{}

	// Generate data
	var buffer bytes.Buffer
	for i := 0; i < 1000; i++ {
		buffer.WriteString("example.com\n")
	}

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" {
			return nil, nil
		}

		entry := entries.NewEntry().
			WithSource("TEST").
			WithProcessID(processID)
		entry.Domain = line

		return entry, nil
	}

	// Run with 8 workers to stress test concurrency
	err := ParseLinesParallel(&buffer, collector, "TEST", 8, 100, processor)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1000, collector.Count(), "Should safely process all entries concurrently")
}

// TestParseLinesParallel_AutoWorkerCount tests auto-detection of worker count
func TestParseLinesParallel_AutoWorkerCount(t *testing.T) {
	collector := &MockCollector{}
	data := strings.NewReader("example1.com\nexample2.com\n")

	processor := func(line, processID string) (*entries.Entry, error) {
		line = strings.TrimSpace(line)
		if line == "" {
			return nil, nil
		}
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	// Use 0 or negative to trigger auto-detection
	err := ParseLinesParallel(data, collector, "TEST", 0, 10, processor)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 2, collector.Count())
}

// TestProcessEntriesParallel_BasicFunctionality tests parallel slice processing
func TestProcessEntriesParallel_BasicFunctionality(t *testing.T) {
	collector := &MockCollector{}

	// Sample data
	items := []string{"example1.com", "example2.com", "example3.com", "example4.com"}

	processor := func(item string, processID string) (*entries.Entry, error) {
		entry := entries.NewEntry().
			WithSource("TEST").
			WithProcessID(processID)
		entry.Domain = item
		return entry, nil
	}

	err := ProcessEntriesParallel(items, collector, 2, processor, "test-id")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 4, collector.Count(), "Should process all 4 items")
}

// TestProcessEntriesParallel_EmptySlice tests empty slice handling
func TestProcessEntriesParallel_EmptySlice(t *testing.T) {
	collector := &MockCollector{}
	items := []string{}

	processor := func(item string, processID string) (*entries.Entry, error) {
		return nil, nil
	}

	err := ProcessEntriesParallel(items, collector, 2, processor, "test-id")
	require.NoError(t, err)

	assert.Equal(t, 0, collector.Count())
}

// TestProcessEntriesParallel_WithFiltering tests filtering nil entries
func TestProcessEntriesParallel_WithFiltering(t *testing.T) {
	collector := &MockCollector{}

	items := []string{"valid.com", "invalid", "another.com", "bad"}

	processor := func(item string, processID string) (*entries.Entry, error) {
		// Only process items with ".com"
		if !strings.Contains(item, ".com") {
			return nil, nil // Filter out
		}

		entry := entries.NewEntry()
		entry.Domain = item
		return entry, nil
	}

	err := ProcessEntriesParallel(items, collector, 2, processor, "test-id")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 2, collector.Count(), "Should only process 2 valid items")
}

// BenchmarkParseLinesSequential benchmarks sequential line parsing
func BenchmarkParseLinesSequential(b *testing.B) {
	// Generate test data
	var buffer bytes.Buffer
	for i := 0; i < 10000; i++ {
		buffer.WriteString("example.com\n")
	}
	data := buffer.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector := &MockCollector{}
		reader := bytes.NewReader(data)

		// Sequential processing
		scanner := io.Reader(reader)
		buf := make([]byte, len(data))
		_, _ = scanner.Read(buf)
		lines := bytes.Split(buf, []byte("\n"))

		for _, line := range lines {
			if len(line) == 0 {
				continue
			}
			entry := entries.NewEntry()
			entry.Domain = string(line)
			collector.Submit(entry)
		}
	}
}

// BenchmarkParseLinesParallel2Workers benchmarks parallel parsing with 2 workers
func BenchmarkParseLinesParallel2Workers(b *testing.B) {
	var buffer bytes.Buffer
	for i := 0; i < 10000; i++ {
		buffer.WriteString("example.com\n")
	}
	data := buffer.Bytes()

	processor := func(line, processID string) (*entries.Entry, error) {
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector := &MockCollector{}
		reader := bytes.NewReader(data)
		_ = ParseLinesParallel(reader, collector, "TEST", 2, 1000, processor)
	}
}

// BenchmarkParseLinesParallel4Workers benchmarks parallel parsing with 4 workers
func BenchmarkParseLinesParallel4Workers(b *testing.B) {
	var buffer bytes.Buffer
	for i := 0; i < 10000; i++ {
		buffer.WriteString("example.com\n")
	}
	data := buffer.Bytes()

	processor := func(line, processID string) (*entries.Entry, error) {
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector := &MockCollector{}
		reader := bytes.NewReader(data)
		_ = ParseLinesParallel(reader, collector, "TEST", 4, 1000, processor)
	}
}

// BenchmarkParseLinesParallel8Workers benchmarks parallel parsing with 8 workers
func BenchmarkParseLinesParallel8Workers(b *testing.B) {
	var buffer bytes.Buffer
	for i := 0; i < 10000; i++ {
		buffer.WriteString("example.com\n")
	}
	data := buffer.Bytes()

	processor := func(line, processID string) (*entries.Entry, error) {
		entry := entries.NewEntry()
		entry.Domain = line
		return entry, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector := &MockCollector{}
		reader := bytes.NewReader(data)
		_ = ParseLinesParallel(reader, collector, "TEST", 8, 1000, processor)
	}
}
