//go:build integration

package emergingthreats

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// intgCollector implements entry_collector.Collector for the integration test.
type intgCollector struct{ entries []*entries.Entry }

func (c *intgCollector) Submit(e *entries.Entry)     { c.entries = append(c.entries, e) }
func (c *intgCollector) Wait()                        {}
func (c *intgCollector) Close()                       {}
func (c *intgCollector) GetProcessedCount(_ string) int { return len(c.entries) }

func (c *intgCollector) StartProviderProcessing(_, _ string) {}
func (c *intgCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

// TestEmergingThreatsIntegration fetches the real compromised IP list and
// verifies we parse ~476 valid IPv4 entries.
//
// Run: go test -tags=integration -run '^TestEmergingThreatsIntegration$' ./features/providers/emergingthreats/
func TestEmergingThreatsIntegration(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, defaultSourceURL, nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	t.Logf("Fetched %d bytes", len(data))

	col := &intgCollector{}
	err = parseIPList(strings.NewReader(string(data)), col, providerName, "intg-test")
	require.NoError(t, err)

	t.Logf("Fetched and parsed %d entries", len(col.entries))

	// Expected ~476 entries. Allow a reasonable range (400–550) since the
	// feed may fluctuate slightly between runs.
	assert.Greater(t, len(col.entries), 400, "should parse at least 400 IPs")
	assert.Less(t, len(col.entries), 550, "should not exceed expected range by too much")

	// Verify every entry is a valid IPv4 with correct metadata.
	for i, e := range col.entries {
		assert.NotEmpty(t, e.Host, "[%d] Host is empty", i)
		assert.NotEmpty(t, e.Domain, "[%d] Domain is empty", i)
		assert.Equal(t, e.Host, e.Domain, "[%d] IP entries: host and domain must match", i)
		assert.Equal(t, providerName, e.Source, "[%d] source mismatch", i)
		assert.Equal(t, "compromised", e.Category, "[%d] category must be compromised", i)

		// Host must be a valid IPv4
		parsed := net.ParseIP(e.Host)
		require.NotNil(t, parsed, "[%d] invalid IP: %s", i, e.Host)
		assert.NotNil(t, parsed.To4(), "[%d] must be IPv4: %s", i, e.Host)
	}
}
