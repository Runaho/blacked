//go:build integration

package toreexitnodes

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

const torURL = "https://check.torproject.org/torbulkexitlist"

// TestTorExitNodesIntegration fetches the real Tor exit node list, parses it,
// and verifies all entries are valid IPv4 with category "anonymizer".
//
// Run: go test -tags=integration -run '^TestTorExitNodesIntegration$' ./features/providers/torexitnodes/
func TestTorExitNodesIntegration(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, torURL, nil)
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

	collector := &integrationCollector{}
	err = parseTorExitNodes(data, collector, providerName, torURL, "intg-process-id", "anonymizer")
	require.NoError(t, err)

	count := len(collector.entries)
	assert.Greater(t, count, 1000, "expected >1000 Tor exit nodes, got %d", count)
	t.Logf("Parsed %d entries", count)

	// Verify every entry
	for i, e := range collector.entries {
		assert.Equal(t, providerName, e.Source, "[%d] source mismatch", i)
		assert.Equal(t, "anonymizer", e.Category, "[%d] category must be anonymizer", i)
		assert.Equal(t, "intg-process-id", e.ProcessID, "[%d] process ID mismatch", i)
		assert.Equal(t, torURL, e.SourceURL, "[%d] source URL mismatch", i)

		// Must be valid IPv4
		parsed := net.ParseIP(e.Host)
		require.NotNil(t, parsed, "[%d] invalid IP: %s", i, e.Host)
		assert.NotNil(t, parsed.To4(), "[%d] must be IPv4: %s", i, e.Host)

		// Host == Domain
		assert.Equal(t, e.Host, e.Domain, "[%d] Domain must equal Host", i)
		assert.Empty(t, e.SubDomains, "[%d] SubDomains must be nil", i)
		assert.Empty(t, e.Scheme)
		assert.Empty(t, e.Path)
		assert.NotEmpty(t, e.ID)

		// Dedup check — no duplicates
		if i > 0 {
			assert.NotEqual(t, collector.entries[i-1].Host, e.Host,
				"[%d] duplicate IP detected: %s", i, e.Host)
		}
	}
}

// integrationCollector is a minimal collector for integration tests.
type integrationCollector struct{ entries []*entries.Entry }

func (c *integrationCollector) Submit(e *entries.Entry) { c.entries = append(c.entries, e) }
func (c *integrationCollector) Wait()                    {}
func (c *integrationCollector) Close()                   {}
func (c *integrationCollector) GetProcessedCount(_ string) int { return len(c.entries) }
func (c *integrationCollector) StartProviderProcessing(_, _ string) {}
func (c *integrationCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

// Detect duplicate IPs by checking the raw data — simple dedup test.
func TestTorExitNodesIntegration_NoDuplicatesInSource(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, torURL, nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	seen := make(map[string]bool)
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		ip := strings.TrimSpace(line)
		if ip == "" {
			continue
		}
		if seen[ip] {
			t.Errorf("duplicate IP in source: %s", ip)
		}
		seen[ip] = true
	}
	t.Logf("Source has %d unique IPs", len(seen))
}
