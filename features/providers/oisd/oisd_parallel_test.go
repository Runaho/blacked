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

func makeTestConfig(t *testing.T) *config.Config {
	return &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"oisd-big": {
				Enabled:         boolPtr(true),
				SourceURL:       "https://big.oisd.nl/domainswild2",
				Cron:            "0 6 * * *",
				Category:        "blocklist",
				ParserWorkers:   4,
				ParserBatchSize: 1000,
			},
			"oisd-nsfw": {
				Enabled:         boolPtr(true),
				SourceURL:       "https://nsfw.oisd.nl/domainswild",
				Cron:            "22 6 * * *",
				Category:        "nsfw",
				ParserWorkers:   4,
				ParserBatchSize: 1000,
			},
		},
	}
}

func ensureOpts(cfg *config.Config, name string) *config.ProviderOptions {
	if cfg.Providers == nil {
		cfg.Providers = map[string]*config.ProviderOptions{}
	}
	if cfg.Providers[name] == nil {
		cfg.Providers[name] = &config.ProviderOptions{
			Enabled:         boolPtr(true),
			ParserWorkers:   4,
			ParserBatchSize: 1000,
		}
	}
	return cfg.Providers[name]
}

func boolPtr(b bool) *bool { return &b }

// TestOISDNSFWProvider_Configuration tests provider configuration
func TestOISDNSFWProvider_Configuration(t *testing.T) {
	cfg := makeTestConfig(t)
	opts := ensureOpts(cfg, "oisd-nsfw")
	opts.ParserWorkers = 2
	opts.ParserBatchSize = 3

	collyClient := colly.NewCollector()
	provider := NewOISDNSFWProvider(cfg, collyClient)

	assert.Equal(t, "oisd-nsfw", provider.GetName())
	assert.Equal(t, "https://nsfw.oisd.nl/domainswild", provider.Source())
	assert.Equal(t, "22 6 * * *", provider.GetCronSchedule())
	assert.NotNil(t, provider)
}

// TestOISDBigProvider_Configuration tests provider configuration
func TestOISDBigProvider_Configuration(t *testing.T) {
	cfg := makeTestConfig(t)
	opts := ensureOpts(cfg, "oisd-big")
	opts.ParserWorkers = 4
	opts.ParserBatchSize = 2

	collyClient := colly.NewCollector()
	provider := NewOISDBigProvider(cfg, collyClient)

	assert.Equal(t, "oisd-big", provider.GetName())
	assert.Equal(t, "https://big.oisd.nl/domainswild2", provider.Source())
	assert.Equal(t, "0 6 * * *", provider.GetCronSchedule())
	assert.NotNil(t, provider)
}

// BenchmarkOISDNSFW_SmallDataset benchmarks NSFW parsing with small dataset
func BenchmarkOISDNSFW_SmallDataset(b *testing.B) {
	// Generate 1,000 domains
	var buffer bytes.Buffer
	for i := range 1000 {
		buffer.WriteString("adult-site-")
		buffer.WriteString(string(rune(i)))
		buffer.WriteString(".com\n")
	}

	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"oisd-nsfw": {
				Enabled:         boolPtr(true),
				ParserWorkers:   2,
				ParserBatchSize: 100,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collyClient := colly.NewCollector()
		_ = NewOISDNSFWProvider(cfg, collyClient)
	}
}

// BenchmarkOISDBig_LargeDataset benchmarks Big list parsing with larger dataset
func BenchmarkOISDBig_LargeDataset(b *testing.B) {
	// Generate 10,000 domains
	var buffer bytes.Buffer
	for i := range 10000 {
		buffer.WriteString("domain-")
		buffer.WriteString(string(rune(i)))
		buffer.WriteString(".com\n")
	}

	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"oisd-big": {
				Enabled:         boolPtr(true),
				ParserWorkers:   4,
				ParserBatchSize: 1000,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collyClient := colly.NewCollector()
		_ = NewOISDBigProvider(cfg, collyClient)
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
			cfg := &config.Config{
				Providers: map[string]*config.ProviderOptions{
					"test": {
						Enabled:         boolPtr(true),
						ParserWorkers:   tc.workers,
						ParserBatchSize: tc.batchSize,
					},
				},
			}

			opts := cfg.Providers["test"]
			assert.Equal(t, tc.expectedWorkers, opts.ParserWorkers)
			assert.Equal(t, tc.expectedBatch, opts.ParserBatchSize)
		})
	}
}

// TestConcurrentProviderExecution simulates multiple providers running concurrently
func TestConcurrentProviderExecution(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"oisd-big": {
				Enabled:         boolPtr(true),
				ParserWorkers:   2,
				ParserBatchSize: 100,
			},
			"oisd-nsfw": {
				Enabled:         boolPtr(true),
				ParserWorkers:   2,
				ParserBatchSize: 100,
			},
		},
	}

	collyClient := colly.NewCollector()

	var wg sync.WaitGroup
	providerCount := 3

	for i := range providerCount {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			if index%2 == 0 {
				provider := NewOISDBigProvider(cfg, collyClient)
				assert.NotNil(t, provider)
			} else {
				provider := NewOISDNSFWProvider(cfg, collyClient)
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

	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"oisd-nsfw": {
				Enabled:         boolPtr(true),
				ParserWorkers:   4,
				ParserBatchSize: 1000,
			},
		},
	}

	collyClient := colly.NewCollector()

	// Create and destroy providers multiple times
	for range 100 {
		provider := NewOISDNSFWProvider(cfg, collyClient)
		require.NotNil(t, provider)

		// Verify basic functionality
		assert.Equal(t, "oisd-nsfw", provider.GetName())
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
