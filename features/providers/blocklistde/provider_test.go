package blocklistde

import (
	"context"
	"testing"
	"time"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock sublistFetcher ---

type mockSublistFetcher struct {
	data map[string]string // url → body
	err  map[string]error  // url → fetch error
}

func (m *mockSublistFetcher) Fetch(url string) ([]byte, error) {
	if m.err != nil && m.err[url] != nil {
		return nil, m.err[url]
	}
	if m.data == nil || m.data[url] == "" {
		return []byte{}, nil
	}
	return []byte(m.data[url]), nil
}

// --- Mock Collector ---

type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry) { c.entries = append(c.entries, entry) }
func (c *testCollector) Wait()                        {}
func (c *testCollector) RemoveStaleEntriesAndSyncBloom(ctx context.Context, providerName, processID string) error { return nil }
func (c *testCollector) Close()                       {}
func (c *testCollector) GetProcessedCount(source string) int {
	return len(c.entries)
}
func (c *testCollector) StartProviderProcessing(_, _ string)                       {}
func (c *testCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) { return len(c.entries), 0, true }

const testSourceURL = "https://lists.blocklist.de/lists/all.txt"
const testPID = "test-process-id"

// --- Phase 1: Basic parse tests ---

func TestParse_ValidIPs(t *testing.T) {
	data := []byte("1.1.1.1\n2.2.2.2\n3.3.3.3\n4.4.4.4\n5.5.5.5")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, len(collector.entries))

	for _, e := range collector.entries {
		assert.Equal(t, providerName, e.Source)
		assert.Equal(t, testPID, e.ProcessID)
		assert.Equal(t, "attacker", e.Category, "fallback should be attacker")
		assert.NotEmpty(t, e.Host)
		assert.Equal(t, e.Host, e.Domain, "Host and Domain should match for IP")
		assert.Nil(t, e.SubDomains)
		assert.Empty(t, e.Scheme)
		assert.Empty(t, e.Path)
	}
}

func TestParse_Empty(t *testing.T) {
	collector := &testCollector{}
	err := parseBlocklistDeData([]byte(""), collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

func TestParse_Mixed(t *testing.T) {
	data := []byte("1.1.1.1\n\n2.2.2.2\ninvalid\n 3.3.3.3 ")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))

	hosts := make([]string, len(collector.entries))
	for i, e := range collector.entries {
		hosts[i] = e.Host
	}
	assert.Contains(t, hosts, "1.1.1.1")
	assert.Contains(t, hosts, "2.2.2.2")
	assert.Contains(t, hosts, "3.3.3.3")
}

func TestParse_IPv6(t *testing.T) {
	data := []byte("1.1.1.1\n::1\n2.2.2.2")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))
	assert.Equal(t, "1.1.1.1", collector.entries[0].Host)
	assert.Equal(t, "::1", collector.entries[1].Host)
	assert.Equal(t, "2.2.2.2", collector.entries[2].Host)

	// Verify IPv6 entry has Host, Domain, and IP populated
	e := collector.entries[1]
	assert.Equal(t, "::1", e.Host)
	assert.Equal(t, "::1", e.Domain)
	assert.Equal(t, "::1", e.IP)
}

func TestParse_CommentLines(t *testing.T) {
	data := []byte("# this is a comment\n1.1.1.1\n# another comment\n2.2.2.2")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
}

func TestParse_CRLF(t *testing.T) {
	data := []byte("1.1.1.1\r\n2.2.2.2\r\n3.3.3.3")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))
}

func TestParse_Whitespace(t *testing.T) {
	data := []byte(" 1.1.1.1 \n\t2.2.2.2\t\n  3.3.3.3  ")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))
	assert.Equal(t, "1.1.1.1", collector.entries[0].Host)
	assert.Equal(t, "2.2.2.2", collector.entries[1].Host)
	assert.Equal(t, "3.3.3.3", collector.entries[2].Host)
}

// --- Phase 2: Category resolution tests ---

func TestCategoryResolution_SingleCategory(t *testing.T) {
	// IP 1.1.1.1 is only in ssh.txt → brute-force
	sf := &mockSublistFetcher{data: map[string]string{
		"https://lists.blocklist.de/lists/ssh.txt": "1.1.1.1\n",
	}}

	data := []byte("1.1.1.1\n2.2.2.2")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, sf)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
	assert.Equal(t, "brute-force", collector.entries[0].Category)
	assert.Equal(t, "attacker", collector.entries[1].Category, "not in any sublist → fallback")
}

func TestCategoryResolution_Mail(t *testing.T) {
	sf := &mockSublistFetcher{data: map[string]string{
		"https://lists.blocklist.de/lists/mail.txt": "1.1.1.1\n",
	}}

	data := []byte("1.1.1.1")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, sf)
	require.NoError(t, err)
	require.Equal(t, 1, len(collector.entries))
	assert.Equal(t, "spam", collector.entries[0].Category)
}

func TestCategoryResolution_WebAttack(t *testing.T) {
	sf := &mockSublistFetcher{data: map[string]string{
		"https://lists.blocklist.de/lists/apache.txt": "2.2.2.2\n",
	}}

	data := []byte("2.2.2.2")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, sf)
	require.NoError(t, err)
	require.Equal(t, 1, len(collector.entries))
	assert.Equal(t, "web-attack", collector.entries[0].Category)
}

func TestCategoryResolution_Priority(t *testing.T) {
	// 1.1.1.1 is in both ssh.txt (brute-force) and strongips.txt (attacker)
	// strongips has higher priority
	sf := &mockSublistFetcher{data: map[string]string{
		"https://lists.blocklist.de/lists/strongips.txt": "1.1.1.1\n",
		"https://lists.blocklist.de/lists/ssh.txt":       "1.1.1.1\n",
	}}

	data := []byte("1.1.1.1")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, sf)
	require.NoError(t, err)
	require.Equal(t, 1, len(collector.entries))
	assert.Equal(t, "attacker", collector.entries[0].Category, "strongips.txt wins over ssh.txt")
}

func TestCategoryResolution_Fallback(t *testing.T) {
	// IP not in any sublist
	sf := &mockSublistFetcher{data: map[string]string{
		"https://lists.blocklist.de/lists/ssh.txt": "10.0.0.1\n",
	}}

	data := []byte("1.1.1.1")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, sf)
	require.NoError(t, err)
	require.Equal(t, 1, len(collector.entries))
	assert.Equal(t, "attacker", collector.entries[0].Category)
}

func TestCategoryResolution_MultiCategory(t *testing.T) {
	// Multiple IPs from different sublists
	sf := &mockSublistFetcher{data: map[string]string{
		"https://lists.blocklist.de/lists/strongips.txt":           "1.1.1.1\n",
		"https://lists.blocklist.de/lists/bots.txt":                "2.2.2.2\n",
		"https://lists.blocklist.de/lists/bruteforcelogin.txt":     "3.3.3.3\n",
		"https://lists.blocklist.de/lists/mail.txt":                "4.4.4.4\n",
		"https://lists.blocklist.de/lists/apache.txt":              "5.5.5.5\n",
	}}

	data := []byte("1.1.1.1\n2.2.2.2\n3.3.3.3\n4.4.4.4\n5.5.5.5\n9.9.9.9")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, sf)
	require.NoError(t, err)
	require.Equal(t, 6, len(collector.entries))

	assert.Equal(t, "attacker", collector.entries[0].Category)
	assert.Equal(t, "botnet", collector.entries[1].Category)
	assert.Equal(t, "brute-force", collector.entries[2].Category)
	assert.Equal(t, "spam", collector.entries[3].Category)
	assert.Equal(t, "web-attack", collector.entries[4].Category)
	assert.Equal(t, "attacker", collector.entries[5].Category, "fallback")
}

func TestCategoryResolution_SublistError(t *testing.T) {
	// All sublist fetches fail → all entries fallback to "attacker"
	sf := &mockSublistFetcher{err: map[string]error{
		"https://lists.blocklist.de/lists/strongips.txt": assert.AnError,
	}}

	data := []byte("1.1.1.1\n2.2.2.2")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, sf)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
	// strongips.txt failed but other sublists succeed (empty data)
	// Since no IP is in any working sublist, fallback to "attacker"
	assert.Equal(t, "attacker", collector.entries[0].Category)
	assert.Equal(t, "attacker", collector.entries[1].Category)
}

// --- Entry field correctness ---

func TestEntryFields(t *testing.T) {
	data := []byte("1.1.1.1")
	collector := &testCollector{}

	err := parseBlocklistDeData(data, collector, testSourceURL, providerName, testPID, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(collector.entries))

	e := collector.entries[0]
	assert.Equal(t, "1.1.1.1", e.Host)
	assert.Equal(t, "1.1.1.1", e.Domain)
	assert.Equal(t, "1.1.1.1", e.IP)
	assert.Nil(t, e.SubDomains)
	assert.Empty(t, e.Scheme)
	assert.Empty(t, e.Path)
	assert.Empty(t, e.RawQuery)
	assert.Equal(t, testSourceURL, e.SourceURL)
	assert.Equal(t, providerName, e.Source)
	assert.NotEmpty(t, e.ID)
	assert.NotEmpty(t, e.ProcessID)
}

// --- resolveCategory unit ---

func TestResolveCategory_NilSets(t *testing.T) {
	assert.Equal(t, "attacker", resolveCategory("1.1.1.1", nil))
}

func TestResolveCategory_EmptySets(t *testing.T) {
	assert.Equal(t, "attacker", resolveCategory("1.1.1.1", map[string]map[string]bool{}))
}

func TestResolveCategory_NotFound(t *testing.T) {
	sets := map[string]map[string]bool{
		"brute-force": {"10.0.0.1": true},
	}
	assert.Equal(t, "attacker", resolveCategory("1.1.1.1", sets))
}

func TestResolveCategory_PriorityOrder(t *testing.T) {
	sets := map[string]map[string]bool{
		"attacker":    {"1.1.1.1": true},
		"brute-force": {"1.1.1.1": true},
		"spam":        {"1.1.1.1": true},
	}
	// attacker comes first in categoryPriority
	assert.Equal(t, "attacker", resolveCategory("1.1.1.1", sets))
}

// --- parseIPsToSet ---

func TestParseIPsToSet_Valid(t *testing.T) {
	set := parseIPsToSet([]byte("1.1.1.1\n2.2.2.2\n3.3.3.3"))
	assert.Equal(t, 3, len(set))
	assert.True(t, set["1.1.1.1"])
	assert.True(t, set["2.2.2.2"])
	assert.True(t, set["3.3.3.3"])
}

func TestParseIPsToSet_Empty(t *testing.T) {
	set := parseIPsToSet([]byte(""))
	assert.Equal(t, 0, len(set))
}

func TestParseIPsToSet_SkipsInvalid(t *testing.T) {
	set := parseIPsToSet([]byte("1.1.1.1\ninvalid\n::1\n2.2.2.2"))
	assert.Equal(t, 2, len(set))
	assert.True(t, set["1.1.1.1"])
	assert.True(t, set["2.2.2.2"])
	assert.False(t, set["invalid"])
	assert.False(t, set["::1"])
}
