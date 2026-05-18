//go:build integration

package threatfox

import (
	"os"
	"strings"
	"testing"
	"time"

	"blacked/features/entries"
	"blacked/features/entries/repository"
	testutil "blacked/internal/testutil"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// intgCollector implements entry_collector.Collector for the integration test.
type intgCollector struct{ entries []*entries.Entry }

func (c *intgCollector) Submit(e *entries.Entry)                        { c.entries = append(c.entries, e) }
func (c *intgCollector) Wait()                                          {}
func (c *intgCollector) Close()                                         {}
func (c *intgCollector) GetProcessedCount(_ string) int                 { return len(c.entries) }
func (c *intgCollector) StartProviderProcessing(_, _ string)            {}
func (c *intgCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) { return len(c.entries), 0, true }
func (c *intgCollector) ScheduleCacheSync(_ bool) bool                  { return true }

// TestThreatFoxIntegration fetches real data from the ThreatFox API,
// parses it, saves to a test DB, and confirms entries are queryable.
//
// Run:  go test -tags=integration -run '^TestThreatFoxIntegration$' ./features/providers/threatfox/
// Env:  THREATFOX_TOKEN=<your_token>
func TestThreatFoxIntegration(t *testing.T) {
	token := os.Getenv("THREATFOX_TOKEN")
	if token == "" {
		t.Skip("THREATFOX_TOKEN not set — skipping")
	}

	ctx, db, cc, err := testutil.Initialize(t)
	require.NoError(t, err)
	defer db.Close()

	repo := repository.NewSQLiteRepository(db)
	source := "threatfox-online"

	fetchURL := strings.ReplaceAll(
		"https://threatfox-api.abuse.ch/v2/files/exports/{token}/recent.json",
		"{token}", token,
	)
	t.Logf("Fetching: %s", fetchURL)

	// Clone colly client and allow large responses (ThreatFox recent feed can exceed 1MB).
	c := cc.Clone()
	c.MaxBodySize = 50 * 1024 * 1024 // 50 MB — should cover full dump too
	var body []byte
	var fetchErr error
	c.OnResponse(func(r *colly.Response) { body = r.Body })
	c.OnError(func(_ *colly.Response, err error) { fetchErr = err })
	require.NoError(t, c.Visit(fetchURL))
	c.Wait()
	require.NoError(t, fetchErr)
	require.NotEmpty(t, body)
	t.Logf("Fetched %d bytes", len(body))

	// Parse via our internal parser
	col := &intgCollector{}
	require.NoError(t, parseThreatFoxResponse(body, col, source))
	require.Greater(t, len(col.entries), 0, "should have parsed at least one IOC")
	t.Logf("Parsed %d entries", len(col.entries))

	// Save to test DB
	for _, e := range col.entries {
		require.NoError(t, repo.SaveEntry(ctx, *e))
	}

	// Confirm entries were stored (count may be slightly lower than parsed
	// due to UPSERT deduplication by (source_url, source)).
	storedCount, err := repo.StreamEntriesCountBySource(ctx, source)
	require.NoError(t, err)
	t.Logf("Parsed=%d Stored=%d (dedup difference=%d)", len(col.entries), storedCount, len(col.entries)-storedCount)
	assert.GreaterOrEqual(t, len(col.entries), storedCount, "stored count should not exceed parsed count")
	require.Greater(t, storedCount, 0, "should have stored at least one entry")

	// Verify a sample of parsed entries have valid fields — we can't
	// directly QueryLink here (bloom filter is still bootstrapping),
	// but the DB write + stream count confirms the storage pipeline.
	t.Log("Sample parsed entries:")
	for i, e := range col.entries {
		if i >= 3 {
			break
		}
		assert.NotEmpty(t, e.ID, "entry ID should not be empty")
		assert.NotEmpty(t, e.Host, "entry Host should not be empty")
		assert.Equal(t, source, e.Source, "entry Source should match provider name")
		t.Logf("  [%d] host=%s domain=%s path=%s category=%s", i, e.Host, e.Domain, e.Path, e.Category)
	}

	// Classify parsed entries by IOC type.
	var ipN, domainN, urlN int
	for _, e := range col.entries {
		if strings.HasPrefix(e.SourceURL, "//") {
			ipN++
		} else if e.Path != "" {
			urlN++
		} else {
			domainN++
		}
	}
	t.Logf("Summary: IPs=%d Domains=%d URLs=%d Total=%d", ipN, domainN, urlN, len(col.entries))
}
