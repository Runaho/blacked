package pihole

import (
	"testing"

	"blacked/internal/config"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mockPiholeData = `# Title: StevenBlack/hosts
# Date: 20 May 2026 20:35:28 (UTC)
# Number of unique domains: 84,230

127.0.0.1 localhost
127.0.0.1 localhost.localdomain
0.0.0.0 ad-assets.futurecdn.net
0.0.0.0 ck.getcookiestxt.com
127.0.0.1 example.com
::1 ip6-localhost
255.255.255.255 broadcasthost
0.0.0.0 malicious-domain.com
127.0.0.1 another-bad-site.net`

func TestParsePiholeData(t *testing.T) {
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

	// Test with mock data
	collector := &testCollector{}
	err := parsePiholeData([]byte(mockPiholeData), collector, "pihole", "https://test.com", "test-process-id")
	require.NoError(t, err)

	// Should have 2 valid entries (ad-assets.futurecdn.net and malicious-domain.com)
	// Skipped: comments, localhost entries, 0.0.0.0 entries, IPv6 entries
	assert.Equal(t, 2, len(collector.entries))

	// Verify the entries
	for _, entry := range collector.entries {
		assert.Equal(t, "pihole", entry.Source)
		assert.Equal(t, "test-process-id", entry.ProcessID)
		assert.Equal(t, "adlist", entry.Category)
		assert.NotEmpty(t, entry.Domain)
		assert.NotEmpty(t, entry.Host)
		assert.Equal(t, "https://test.com", entry.SourceURL)
	}
}

func TestParsePiholeData_EdgeCases(t *testing.T) {
	// Test empty data
	collector := &testCollector{}
	err := parsePiholeData([]byte(""), collector, "pihole", "https://test.com", "test-process-id")
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))

	// Test only comments
	commentData := `# This is a comment
# Another comment`
	collector = &testCollector{}
	err = parsePiholeData([]byte(commentData), collector, "pihole", "https://test.com", "test-process-id")
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))

	// Test malformed lines
	malformedData := `0.0.0.0
127.0.0.1
invalid line
# comment`
	collector = &testCollector{}
	err = parsePiholeData([]byte(malformedData), collector, "pihole", "https://test.com", "test-process-id")
	require.NoError(t, err)
	assert.Equal(t, 1, len(collector.entries))
}

func TestNewPiholeProvider(t *testing.T) {
	// Test disabled provider
	cfg := &config.Config{
		Providers: map[string]*config.ProviderOptions{
			"pihole": {
				Enabled: boolPtr(false),
			},
		},
	}

	collyClient := colly.NewCollector()
	provider := NewPiholeProvider(cfg, collyClient)
	assert.Nil(t, provider)
}

func TestPiholeProvider_Fetch(t *testing.T) {
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

	// Test Fetch method
	reader, err := provider.Fetch()
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Read some data to verify it's not empty
	buf := make([]byte, 100)
	n, err := reader.Read(buf)
	require.NoError(t, err)
	require.Greater(t, n, 0)
}

// testCollector is a simple implementation of entry_collector.Collector for testing
// (defined in test_helpers.go)


