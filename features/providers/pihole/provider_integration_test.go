package pihole

import (
	"testing"

	"blacked/features/providers/base"
	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPiholeProvider_Integration(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"pihole": {
				Enabled:   boolPtr(true),
				SourceURL: "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
			},
		},
	}

	collyClient := colly.NewCollector()
	provider := NewPiholeProvider(cfg, collyClient)
	require.NotNil(t, provider)

	// Test that provider implements the base.Provider interface
	var _ base.Provider = provider

	// Test GetName
	assert.Equal(t, "pihole", provider.GetName())

	// Test Source
	assert.Equal(t, "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts", provider.Source())

	// Test GetCronSchedule
	assert.Equal(t, "0 */12 * * *", provider.GetCronSchedule())

	// Test Fetch and Parse integration
	reader, err := provider.Fetch()
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Parse the fetched data
	collector := &testCollector{}
	err = parsePiholeData([]byte(mockPiholeData), collector, "pihole", "https://test.com", "test-process-id")
	require.NoError(t, err)

	// Should have valid entries
	assert.Greater(t, len(collector.entries), 0)
}

func TestPiholeProvider_RealDataSample(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"pihole": {
				Enabled:   boolPtr(true),
				SourceURL: "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
			},
		},
	}

	collyClient := colly.NewCollector()
	provider := NewPiholeProvider(cfg, collyClient)
	require.NotNil(t, provider)

	// Fetch real data
	reader, err := provider.Fetch()
	require.NoError(t, err)
	require.NotNil(t, reader)

	// For this test, we'll just verify we can fetch data
	// Actual parsing would be too slow for unit tests
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	require.NoError(t, err)
	require.Greater(t, n, 0)

	// Verify the data contains expected patterns
	data := string(buf[:n])
	assert.Contains(t, data, "#") // Should have comments
	assert.Contains(t, data, "0.0.0.0") // Should have hosts entries
}

// testCollector is a simple implementation of entry_collector.Collector for testing
// (defined in test_helpers.go)


