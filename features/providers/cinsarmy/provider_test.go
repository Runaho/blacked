package cinsarmy

import (
	"strings"
	"testing"
	"time"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCollector implements entry_collector.Collector
type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry)       { c.entries = append(c.entries, entry) }
func (c *testCollector) Wait()                               {}
func (c *testCollector) Close()                              {}
func (c *testCollector) GetProcessedCount(source string) int { return len(c.entries) }
func (c *testCollector) StartProviderProcessing(_, _ string) {}
func (c *testCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

const testPID = "test-process-id"
const testSourceURL = "https://cinsscore.com/list/ci-badguys.txt"

// --- Parse tests ---

func TestParseCINSData_FiveValidIPs(t *testing.T) {
	data := []byte("1.1.1.1\n2.2.2.2\n3.3.3.3\n4.4.4.4\n5.5.5.5\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 5, len(collector.entries))

	for i, e := range collector.entries {
		assert.Equal(t, providerName, e.Source)
		assert.Equal(t, testPID, e.ProcessID)
		assert.Equal(t, "scanner", e.Category)
		assert.Equal(t, testSourceURL, e.SourceURL)
		assert.Equal(t, e.Host, e.Domain, "Domain must equal Host for IP entries")
		assert.Empty(t, e.SubDomains)
		assert.Empty(t, e.Scheme)
		assert.Empty(t, e.Path)
		if i == 0 {
			assert.Equal(t, "1.1.1.1", e.Host)
		}
	}
}

func TestParseCINSData_Empty(t *testing.T) {
	collector := &testCollector{}
	err := parseCINSData([]byte(""), collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

func TestParseCINSData_MixedValidInvalid(t *testing.T) {
	data := []byte("1.1.1.1\n\n2.2.2.2\ninvalid\nnot-an-ip\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
	assert.Equal(t, "1.1.1.1", collector.entries[0].Host)
	assert.Equal(t, "2.2.2.2", collector.entries[1].Host)
}

func TestParseCINSData_IPv6Skip(t *testing.T) {
	data := []byte("1.1.1.1\n::1\n2001:db8::1\n2.2.2.2\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
	assert.Equal(t, "1.1.1.1", collector.entries[0].Host)
	assert.Equal(t, "2.2.2.2", collector.entries[1].Host)
}

func TestParseCINSData_CategoryAlwaysScanner(t *testing.T) {
	data := []byte("1.1.1.1\n2.2.2.2\n3.3.3.3\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)

	for _, e := range collector.entries {
		assert.Equal(t, "scanner", e.Category, "all CINS entries must be category 'scanner'")
	}
}

func TestParseCINSData_CRLFNormalize(t *testing.T) {
	data := []byte("1.1.1.1\r\n2.2.2.2\r\n3.3.3.3\r\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))
}

func TestParseCINSData_WhitespaceTrim(t *testing.T) {
	data := []byte(" 1.1.1.1 \n\t2.2.2.2\t\n 3.3.3.3 \n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))
	assert.Equal(t, "1.1.1.1", collector.entries[0].Host)
	assert.Equal(t, "2.2.2.2", collector.entries[1].Host)
	assert.Equal(t, "3.3.3.3", collector.entries[2].Host)
}

func TestParseCINSData_CommentSkip(t *testing.T) {
	data := []byte("# CINS Army list\n1.1.1.1\n# another comment\n2.2.2.2\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
}

func TestParseCINSData_InvalidIP(t *testing.T) {
	data := []byte("999.999.999.999\n256.256.256.256\n1.1.1\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

func TestParseCINSData_HostDomainConsistency(t *testing.T) {
	// Verify Host == Domain for all entries (no SetURL called)
	data := []byte("1.1.1.1\n192.168.0.1\n10.0.0.1\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)

	for _, e := range collector.entries {
		assert.Equal(t, e.Host, e.Domain, "Host and Domain must be the same IP string")
		assert.Empty(t, e.SubDomains, "SubDomains must be nil for IP-only entries")
		assert.Empty(t, e.Scheme, "Scheme must be empty for IP-only entries")
		assert.Empty(t, e.Path, "Path must be empty for IP-only entries")
	}
}

func TestParseCINSData_TrailingNewline(t *testing.T) {
	// Trailing newline shouldn't produce an empty entry
	data := []byte("1.1.1.1\n2.2.2.2\n")
	collector := &testCollector{}

	err := parseCINSData(data, collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
}

func TestParseCINSData_LargeSet(t *testing.T) {
	// Simulate ~1000 valid IPs
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("1.1.1.1\n")
	}
	collector := &testCollector{}

	err := parseCINSData([]byte(sb.String()), collector, providerName, testSourceURL, testPID)
	require.NoError(t, err)
	assert.Equal(t, 1000, len(collector.entries))
}
