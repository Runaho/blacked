package threatfox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Mock RepositoryProvider ----------

type mockRepo struct {
	count       int
	maxCreatedAt int64
}

func (m *mockRepo) StreamEntriesCountBySource(_ context.Context, _ string) (int, error) {
	return m.count, nil
}

func (m *mockRepo) GetMaxCreatedAtBySource(_ context.Context, _ string) (int64, error) {
	return m.maxCreatedAt, nil
}

// ---------- Mock Collector ----------

type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry)                     { c.entries = append(c.entries, entry) }
func (c *testCollector) Wait()                                           {}
func (c *testCollector) Close()                                          {}
func (c *testCollector) GetProcessedCount(source string) int             { return len(c.entries) }
func (c *testCollector) StartProviderProcessing(_, _ string)             {}
func (c *testCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

const testPID = "test-process-id"

// ---------- Dual-fetch strategy tests ----------

func TestEmptyDB_UsesDump(t *testing.T) {
	repo := &mockRepo{count: 0}
	recentURL := "https://example.com/{token}/recent.json"
	dumpURL := "https://example.com/{token}/full.json.zip"

	result := determineFetchURL(recentURL, dumpURL, "mykey", repo)

	assert.Contains(t, result, "full.json.zip")
	assert.Contains(t, result, "mykey")
}

func TestWithEntriesNoGap_UsesRecent(t *testing.T) {
	repo := &mockRepo{count: 100, maxCreatedAt: time.Now().Add(-1 * time.Hour).UnixNano()}
	recentURL := "https://example.com/{token}/recent.json"
	dumpURL := "https://example.com/{token}/full.json.zip"

	result := determineFetchURL(recentURL, dumpURL, "mykey", repo)

	assert.Contains(t, result, "recent.json")
	assert.Contains(t, result, "mykey")
}

func TestGapDetected_UsesDump(t *testing.T) {
	repo := &mockRepo{count: 100, maxCreatedAt: time.Now().Add(-72 * time.Hour).UnixNano()}
	recentURL := "https://example.com/{token}/recent.json"
	dumpURL := "https://example.com/{token}/full.json.zip"

	result := determineFetchURL(recentURL, dumpURL, "mykey", repo)

	assert.Contains(t, result, "full.json.zip")
}

func TestEmptyKey_Fallback_RecentIsUsed(t *testing.T) {
	repo := &mockRepo{count: 100, maxCreatedAt: time.Now().UnixNano()}
	recentURL := "https://example.com/{token}/recent.json"

	result := determineFetchURL(recentURL, "no-important", "", repo)

	assert.Contains(t, result, "{token}")
}

func TestDumpWithEmptyKey_TemplatePreserved(t *testing.T) {
	repo := &mockRepo{count: 0}
	dumpURL := "https://example.com/{token}/full.json.zip"

	result := determineFetchURL("recent-url", dumpURL, "", repo)

	assert.Contains(t, result, "{token}")
	assert.Contains(t, result, "full.json.zip")
}

// ---------- JSON parsing tests ----------

func TestParseThreatFoxJSON(t *testing.T) {
	mockJSON, err := json.Marshal(map[string][]threatFoxIOC{
		"1001": {
			{IOCValue: "118.31.114.149:443", IOCType: "ip:port", ThreatType: "botnet_cc", MalwarePrintable: "Cobalt Strike", ConfidenceLevel: 100},
			{IOCValue: "evil.com", IOCType: "domain", ThreatType: "payload_delivery", MalwarePrintable: "ClearFake", ConfidenceLevel: 75},
			{IOCValue: "https://evil.com/path", IOCType: "url", ThreatType: "payload_delivery", MalwarePrintable: "Vidar", ConfidenceLevel: 50},
			{IOCValue: "10.0.0.5:8080", IOCType: "ip:port", ThreatType: "botnet_cc", ConfidenceLevel: 100},
			{IOCValue: "another-malware.com", IOCType: "domain", ThreatType: "payload_delivery", MalwarePrintable: "Vidar", ConfidenceLevel: 100},
			{IOCValue: "abc123hash...", IOCType: "sha256_hash", ThreatType: "payload", ConfidenceLevel: 100},
		},
	})
	require.NoError(t, err)

	collector := &testCollector{}
	err = parseThreatFoxJSON(mockJSON, collector, "threatfox-online", testPID)
	require.NoError(t, err)
	assert.Equal(t, 5, len(collector.entries), "should parse 5 entries, skip 1 sha256_hash")

	// ip:port → Host, Domain are both the IP (after fix), subdomains nil
	assert.Equal(t, "118.31.114.149", collector.entries[0].Host)
	assert.Equal(t, "118.31.114.149", collector.entries[0].Domain,
		"IP entries should have Domain = Host (not octet garbage)")
	assert.Empty(t, collector.entries[0].SubDomains,
		"IP entries should have empty SubDomains")
	assert.Equal(t, "botnet_cc", collector.entries[0].Category)

	// domain: SetURL("evil.com") sets both Host and Domain
	assert.Equal(t, "evil.com", collector.entries[1].Host)
	assert.Equal(t, "evil.com", collector.entries[1].Domain)
	assert.Equal(t, "payload_delivery", collector.entries[1].Category)

	// url
	assert.Equal(t, "evil.com", collector.entries[2].Domain)
	assert.Contains(t, collector.entries[2].Path, "/path")

	// second ip:port
	assert.Equal(t, "10.0.0.5", collector.entries[3].Host)
	assert.Equal(t, "10.0.0.5", collector.entries[3].Domain)

	// second domain
	assert.Equal(t, "another-malware.com", collector.entries[4].Domain)

	for _, e := range collector.entries {
		assert.Equal(t, "threatfox-online", e.Source)
		assert.Equal(t, testPID, e.ProcessID,
			"all entries should have the same process ID")
	}

	// Verify category uses threat_type (not provider-level category)
	assert.Equal(t, "botnet_cc", collector.entries[0].Category)
	assert.Equal(t, "payload_delivery", collector.entries[1].Category)
}

func TestParseThreatFoxJSON_Empty(t *testing.T) {
	collector := &testCollector{}
	err := parseThreatFoxJSON([]byte(`{}`), collector, "threatfox-online", testPID)
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

func TestParseThreatFoxJSON_Malformed(t *testing.T) {
	collector := &testCollector{}
	err := parseThreatFoxJSON([]byte(`{invalid`), collector, "threatfox-online", testPID)
	require.Error(t, err)
}

// ---------- Zip detection tests ----------

func TestIsZip(t *testing.T) {
	assert.True(t, isZip([]byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00}))
	assert.False(t, isZip([]byte{0x7B, 0x22, 0x6B, 0x65, 0x79})) // "{"key…
	assert.False(t, isZip(nil))
	assert.True(t, isZip([]byte{0x50, 0x4B, 0x03, 0x04}), "exactly 4 bytes should still be detected as zip")
}

// ---------- URL resolution tests ----------

func TestResolveThreatFoxURL(t *testing.T) {
	result := resolveThreatFoxURL("https://example.com/{token}/path", "abc123")
	assert.Equal(t, "https://example.com/abc123/path", result)

	result2 := resolveThreatFoxURL("", "key")
	assert.Equal(t, "", result2)
}

// ---------- IOC to Entry mapping tests ----------

func TestIOCToEntry_ipPort_Invalid(t *testing.T) {
	entry, err := iocToEntry(&threatFoxIOC{
		IOCValue: "invalid-value",
		IOCType:  "ip:port",
	}, "test", testPID)
	assert.NoError(t, err)
	assert.Nil(t, entry, "invalid ip:port should return nil entry")

	entry2, err := iocToEntry(&threatFoxIOC{
		IOCValue: "a.b.c.d:80",
		IOCType:  "ip:port",
	}, "test", testPID)
	assert.NoError(t, err)
	assert.Nil(t, entry2, "four-label host:port should be skipped — not a valid IP")
}

func TestIOCToEntry_SkipHash(t *testing.T) {
	entry, err := iocToEntry(&threatFoxIOC{
		IOCValue: "abc123...",
		IOCType:  "sha256_hash",
	}, "test", testPID)
	assert.NoError(t, err)
	assert.Nil(t, entry, "hash should be skipped")
}

func TestIOCToEntry_IPHasDomainSet(t *testing.T) {
	// Verify IP entries have Domain = Host (no octet garbage from PSL)
	for _, tc := range []struct {
		input string
		host  string
	}{
		{"118.31.114.149:443", "118.31.114.149"},
		{"10.0.0.5:8080", "10.0.0.5"},
		{"192.168.1.1:80", "192.168.1.1"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			entry, err := iocToEntry(&threatFoxIOC{
				IOCValue: tc.input,
				IOCType:  "ip:port",
			}, "test", testPID)
			require.NoError(t, err)
			require.NotNil(t, entry)
			assert.Equal(t, tc.host, entry.Host)
			assert.Equal(t, tc.host, entry.Domain, "IP Domain should equal Host")
			assert.Empty(t, entry.SubDomains, "IP SubDomains should be empty")
		})
	}
}

func TestIOCToEntry_ProcessID(t *testing.T) {
	entry, err := iocToEntry(&threatFoxIOC{
		IOCValue: "evil.com",
		IOCType:  "domain",
	}, "test", testPID)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, testPID, entry.ProcessID)

	entry2, err := iocToEntry(&threatFoxIOC{
		IOCValue: "118.31.114.149:443",
		IOCType:  "ip:port",
	}, "test", "another-pid")
	require.NoError(t, err)
	require.NotNil(t, entry2)
	assert.Equal(t, "another-pid", entry2.ProcessID)
}

func TestIOCToEntry_ConfidenceScore(t *testing.T) {
	entry, err := iocToEntry(&threatFoxIOC{
		IOCValue:        "evil.com",
		IOCType:         "domain",
		ThreatType:      "payload_delivery",
		ConfidenceLevel: 75,
	}, "test", testPID)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "payload_delivery", entry.Category)
}

// ---------- Full response parsing tests ----------

func TestParseThreatFoxResponse_PlainJSON(t *testing.T) {
	data := []byte(`{"1":[{"ioc_value":"evil.com","ioc_type":"domain","threat_type":"malware"}]}`)
	collector := &testCollector{}
	err := parseThreatFoxResponse(data, collector, "threatfox-online", testPID)
	require.NoError(t, err)
	assert.Equal(t, 1, len(collector.entries))
}

func TestParseThreatFoxResponse_Zip(t *testing.T) {
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	f, _ := zipWriter.Create("full.json")
	f.Write([]byte(`{"1":[{"ioc_value":"evil.com","ioc_type":"domain","threat_type":"malware"}]}`))
	zipWriter.Close()

	collector := &testCollector{}
	err := parseThreatFoxResponse(zipBuf.Bytes(), collector, "threatfox-online", testPID)
	require.NoError(t, err)
	assert.Equal(t, 1, len(collector.entries))
}

func TestParseThreatFoxResponse_Zip_NoFullJSON(t *testing.T) {
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	f, _ := zipWriter.Create("other.json")
	f.Write([]byte(`{}`))
	zipWriter.Close()

	collector := &testCollector{}
	err := parseThreatFoxResponse(zipBuf.Bytes(), collector, "threatfox-online", testPID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full.json not found")
}

// ---------- Edge case: empty IOCs ----------

func TestIOCToEntry_EmptyValues(t *testing.T) {
	entry, err := iocToEntry(&threatFoxIOC{
		IOCValue: "",
		IOCType:  "domain",
	}, "test", testPID)
	require.NoError(t, err)
	assert.Nil(t, entry, "empty domain should produce nil")

	entry2, err := iocToEntry(&threatFoxIOC{
		IOCValue: ":80",
		IOCType:  "ip:port",
	}, "test", testPID)
	require.NoError(t, err)
	assert.Nil(t, entry2, "empty host with port should produce nil")
}

// ---------- shouldUseDump direct tests ----------

func TestShouldUseDump_EmptyRepo(t *testing.T) {
	repo := &mockRepo{count: 0}
	assert.True(t, shouldUseDump("test", repo), "empty repo should use dump")
}

func TestShouldUseDump_RecentOnly(t *testing.T) {
	repo := &mockRepo{count: 100, maxCreatedAt: time.Now().UnixNano()}
	assert.False(t, shouldUseDump("test", repo), "recent data should skip dump")
}

func TestShouldUseDump_Gap(t *testing.T) {
	repo := &mockRepo{count: 100, maxCreatedAt: time.Now().Add(-72 * time.Hour).UnixNano()}
	assert.True(t, shouldUseDump("test", repo), "72h gap should use dump")
}

// ---------- shouldUseDump direct tests ----------
