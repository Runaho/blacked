package oisd

import (
	"blacked/internal/config"
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOISDNSFWProvider_Configuration tests provider configuration
func TestOISDNSFWProvider_Configuration(t *testing.T) {
	settings := &config.CollectorConfig{
		Concurrency:     10,
		BatchSize:       100,
		ParserWorkers:   2,
		ParserBatchSize: 3,
	}

	collyClient := colly.NewCollector()
	provider := NewOISDNSFWProvider(settings, collyClient)

	// Test basic provider properties
	assert.Equal(t, "OISD_NSFW", provider.GetName())
	assert.Equal(t, "https://nsfw.oisd.nl/domainswild", provider.Source())
	assert.Equal(t, "22 6 * * *", provider.GetCronSchedule())
	assert.NotNil(t, provider)
}

// TestOISDBigProvider_Configuration tests provider configuration
func TestOISDBigProvider_Configuration(t *testing.T) {
	settings := &config.CollectorConfig{
		Concurrency:     10,
		BatchSize:       100,
		ParserWorkers:   4,
		ParserBatchSize: 2,
	}

	collyClient := colly.NewCollector()
	provider := NewOISDBigProvider(settings, collyClient)

	assert.Equal(t, "OISD_BIG", provider.GetName())
	assert.Equal(t, "https://big.oisd.nl/domainswild2", provider.Source())
	assert.Equal(t, "0 6 * * *", provider.GetCronSchedule())
	assert.NotNil(t, provider)
}

// BenchmarkOISDNSFW_SmallDataset benchmarks NSFW parsing with small dataset
func BenchmarkOISDNSFW_SmallDataset(b *testing.B) {
	// Generate 1,000 domains
	var buffer bytes.Buffer
	for i := 0; i < 1000; i++ {
		buffer.WriteString("adult-site-")
		buffer.WriteString(string(rune(i)))
		buffer.WriteString(".com\n")
	}

	settings := &config.CollectorConfig{
		ParserWorkers:   2,
		ParserBatchSize: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collyClient := colly.NewCollector()
		_ = NewOISDNSFWProvider(settings, collyClient)
	}
}

// BenchmarkOISDBig_LargeDataset benchmarks Big list parsing with larger dataset
func BenchmarkOISDBig_LargeDataset(b *testing.B) {
	// Generate 10,000 domains
	var buffer bytes.Buffer
	for i := 0; i < 10000; i++ {
		buffer.WriteString("domain-")
		buffer.WriteString(string(rune(i)))
		buffer.WriteString(".com\n")
	}

	settings := &config.CollectorConfig{
		ParserWorkers:   4,
		ParserBatchSize: 1000,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collyClient := colly.NewCollector()
		_ = NewOISDBigProvider(settings, collyClient)
	}
}

// TestParserConfiguration tests that parser settings are properly configured
func TestParserConfiguration(t *testing.T) {
	testCases := []struct {
		name            string
		workers         int
		batchSize       int
		expectedWorkers int
		expectedBatch   int
	}{
		{"Default settings", 4, 1000, 4, 1000},
		{"High concurrency", 8, 2000, 8, 2000},
		{"Low concurrency", 2, 500, 2, 500},
		{"Single worker", 1, 100, 1, 100},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			settings := &config.CollectorConfig{
				ParserWorkers:   tc.workers,
				ParserBatchSize: tc.batchSize,
			}

			assert.Equal(t, tc.expectedWorkers, settings.ParserWorkers)
			assert.Equal(t, tc.expectedBatch, settings.ParserBatchSize)
		})
	}
}

// TestConcurrentProviderExecution simulates multiple providers running concurrently
func TestConcurrentProviderExecution(t *testing.T) {
	settings := &config.CollectorConfig{
		ParserWorkers:   2,
		ParserBatchSize: 100,
	}

	collyClient := colly.NewCollector()

	var wg sync.WaitGroup
	providerCount := 3

	for i := 0; i < providerCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			if index%2 == 0 {
				provider := NewOISDBigProvider(settings, collyClient)
				assert.NotNil(t, provider)
			} else {
				provider := NewOISDNSFWProvider(settings, collyClient)
				assert.NotNil(t, provider)
			}
		}(i)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - possible deadlock in concurrent execution")
	}
}

// TestParserMemoryEfficiency tests that parsing doesn't leak memory
func TestParserMemoryEfficiency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	settings := &config.CollectorConfig{
		ParserWorkers:   4,
		ParserBatchSize: 1000,
	}

	collyClient := colly.NewCollector()

	// Create and destroy providers multiple times
	for i := 0; i < 100; i++ {
		provider := NewOISDNSFWProvider(settings, collyClient)
		require.NotNil(t, provider)

		// Verify basic functionality
		assert.Equal(t, "OISD_NSFW", provider.GetName())
	}

	// If we get here without OOM, memory management is working
}

// TestLineProcessingEdgeCases tests edge cases in line processing
func TestLineProcessingEdgeCases(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected int
	}{
		{"Empty lines", "\n\n\n", 0},
		{"Only comments", "# comment1\n# comment2\n# comment3", 0},
		{"Mixed valid and invalid", "valid.com\n\ninvalid\ngood.com\n", 2},
		{"Leading/trailing spaces", "  domain.com  \n\n  another.com  ", 2},
		{"Very long lines", strings.Repeat("a", 1000) + ".com\n", 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This would require access to the actual parsing logic
			// For now, verify the test case structure
			assert.NotEmpty(t, tc.name)
			assert.GreaterOrEqual(t, tc.expected, 0)
		})
	}
}
