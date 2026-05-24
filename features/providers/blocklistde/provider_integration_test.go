//go:build integration

package blocklistde

import (
	"io"
	"testing"
	"time"

	"blacked/features/entries"
	"blacked/features/providers/base"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationCollector is a minimal in-memory collector for integration tests.
type integrationCollector struct {
	entries []*entries.Entry
}

func (c *integrationCollector) Submit(entry *entries.Entry) { c.entries = append(c.entries, entry) }
func (c *integrationCollector) Wait()                        {}
func (c *integrationCollector) Close()                       {}
func (c *integrationCollector) GetProcessedCount(source string) int {
	return len(c.entries)
}
func (c *integrationCollector) StartProviderProcessing(_, _ string) {}
func (c *integrationCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

func TestBlocklistDeIntegration_FetchAndParse(t *testing.T) {
	// Real end-to-end: fetch all.txt from live blocklist.de, parse, verify.
	// Run manually with: go test -tags=integration -run TestBlocklistDeIntegration -count=1
	//
	// This validates:
	//   - Live feed is reachable
	//   - ~24K valid IPv4 entries parsed
	//   - All entries have Host==Domain, nil SubDomains, empty Scheme/Path
	t.Skip("requires network — run manually with -tags=integration -count=1")

	fetcher := base.NewHTTPFetcher(60*time.Second, "", 3)
	rc, err := fetcher.Fetch("https://lists.blocklist.de/lists/all.txt")
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	collector := &integrationCollector{}
	err = parseBlocklistDeData(
		data, collector,
		"https://lists.blocklist.de/lists/all.txt",
		providerName, "integration-test",
		nil, // no sublist resolution for basic integration test
	)
	require.NoError(t, err)

	t.Logf("parsed %d entries from live all.txt", len(collector.entries))
	assert.Greater(t, len(collector.entries), 20000, "should have ~24K entries")

	for _, e := range collector.entries {
		assert.NotEmpty(t, e.Host, "every entry must have Host set")
		assert.Equal(t, e.Host, e.Domain, "Host and Domain must match for IP entries")
		assert.Nil(t, e.SubDomains, "IP entries must have nil SubDomains")
		assert.Empty(t, e.Scheme, "IP entries must have empty Scheme")
		assert.Empty(t, e.Path, "IP entries must have empty Path")
		assert.Regexp(t, `^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`, e.Host, "Host must be a valid IPv4")
		assert.Equal(t, providerName, e.Source)
		assert.Equal(t, "integration-test", e.ProcessID)
	}
}
