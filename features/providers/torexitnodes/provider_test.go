package torexitnodes

import (
	"strings"
	"testing"
	"time"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Mock Collector ----------

type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry)                   { c.entries = append(c.entries, entry) }
func (c *testCollector) Wait()                                          {}
func (c *testCollector) Close()                                         {}
func (c *testCollector) GetProcessedCount(_ string) int                 { return len(c.entries) }
func (c *testCollector) StartProviderProcessing(_, _ string)            {}
func (c *testCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

const testPID = "test-process-id"
const testSourceURL = "https://check.torproject.org/torbulkexitlist"
const testCategory = "anonymizer"

// ---------- Happy path: valid IPv4 entries ----------

func TestParseTorExitNodes_FiveValidIPs(t *testing.T) {
	data := []byte("1.2.3.4\n5.6.7.8\n9.10.11.12\n10.0.0.1\n192.168.1.1\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 5, len(collector.entries))

	for i, e := range collector.entries {
		assert.Equal(t, providerName, e.Source)
		assert.Equal(t, testPID, e.ProcessID)
		assert.Equal(t, testCategory, e.Category)
		assert.Equal(t, testSourceURL, e.SourceURL)
		assert.Equal(t, e.Host, e.Domain, "Domain must equal Host for IP entries")
		assert.Empty(t, e.SubDomains)
		assert.Empty(t, e.Scheme)
		assert.Empty(t, e.Path)
		if i == 0 {
			assert.Equal(t, "1.2.3.4", e.Host)
		}
	}
}

// ---------- Empty input ----------

func TestParseTorExitNodes_Empty(t *testing.T) {
	collector := &testCollector{}
	err := parseTorExitNodes([]byte(""), collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

// ---------- Invalid IPs ----------

func TestParseTorExitNodes_MixedValidInvalid(t *testing.T) {
	data := []byte("1.2.3.4\n\n5.6.7.8\nnot-valid\nnot-an-ip\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
	assert.Equal(t, "1.2.3.4", collector.entries[0].Host)
	assert.Equal(t, "5.6.7.8", collector.entries[1].Host)
}

// ---------- IPv6 skip ----------

func TestParseTorExitNodes_IPv6Skip(t *testing.T) {
	data := []byte("1.2.3.4\n::1\n2001:db8::1\n5.6.7.8\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries))
	assert.Equal(t, "1.2.3.4", collector.entries[0].Host)
	assert.Equal(t, "5.6.7.8", collector.entries[1].Host)
}

// ---------- Category always "anonymizer" ----------

func TestParseTorExitNodes_CategoryAlwaysAnonymizer(t *testing.T) {
	data := []byte("1.2.3.4\n5.6.7.8\n9.10.11.12\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)

	for _, e := range collector.entries {
		assert.Equal(t, "anonymizer", e.Category, "all Tor exit nodes must be category 'anonymizer'")
	}
}

// ---------- CRLF normalize ----------

func TestParseTorExitNodes_CRLFNormalize(t *testing.T) {
	data := []byte("1.2.3.4\r\n5.6.7.8\r\n9.10.11.12\r\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))
}

// ---------- Whitespace trim ----------

func TestParseTorExitNodes_WhitespaceTrim(t *testing.T) {
	data := []byte(" 1.2.3.4 \n\t5.6.7.8\t\n 9.10.11.12 \n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 3, len(collector.entries))
	assert.Equal(t, "1.2.3.4", collector.entries[0].Host)
	assert.Equal(t, "5.6.7.8", collector.entries[1].Host)
	assert.Equal(t, "9.10.11.12", collector.entries[2].Host)
}

// ---------- Invalid IPs (edge cases) ----------

func TestParseTorExitNodes_InvalidIP(t *testing.T) {
	data := []byte("999.999.999.999\n256.256.256.256\n1.2.3\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

// ---------- Host/Domain consistency ----------

func TestParseTorExitNodes_HostDomainConsistency(t *testing.T) {
	data := []byte("1.2.3.4\n192.168.0.1\n10.0.0.1\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)

	for _, e := range collector.entries {
		assert.Equal(t, e.Host, e.Domain, "Host and Domain must be the same IP string")
		assert.Empty(t, e.SubDomains, "SubDomains must be nil for IP-only entries")
		assert.Empty(t, e.Scheme, "Scheme must be empty for IP-only entries")
		assert.Empty(t, e.Path, "Path must be empty for IP-only entries")
	}
}

// ---------- Trailing newline ----------

func TestParseTorExitNodes_TrailingNewline(t *testing.T) {
	data := []byte("1.2.3.4\n5.6.7.8\n")
	collector := &testCollector{}

	err := parseTorExitNodes(data, collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 2, len(collector.entries), "trailing newline should not produce empty entry")
}

// ---------- Large dataset (~1270 entries) ----------

func TestParseTorExitNodes_LargeSet(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 1270; i++ {
		sb.WriteString("1.2.3.4\n")
	}
	collector := &testCollector{}

	err := parseTorExitNodes([]byte(sb.String()), collector, providerName, testSourceURL, testPID, testCategory)
	require.NoError(t, err)
	assert.Equal(t, 1270, len(collector.entries))
}
