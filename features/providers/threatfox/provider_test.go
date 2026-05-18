package threatfox

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	err = parseThreatFoxJSON(mockJSON, collector, "threatfox-online")
	require.NoError(t, err)
	assert.Equal(t, 5, len(collector.entries), "should parse 5 entries, skip 1 sha256_hash")

	// ip:port → Host is set after SetURL("//IP"), Domain uses naive fallback
	assert.Equal(t, "118.31.114.149", collector.entries[0].Host)
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

	// second domain
	assert.Equal(t, "another-malware.com", collector.entries[4].Domain)

	for _, e := range collector.entries {
		assert.Equal(t, "threatfox-online", e.Source)
	}
}

func TestParseThreatFoxJSON_Empty(t *testing.T) {
	collector := &testCollector{}
	err := parseThreatFoxJSON([]byte(`{}`), collector, "threatfox-online")
	require.NoError(t, err)
	assert.Equal(t, 0, len(collector.entries))
}

func TestParseThreatFoxJSON_Malformed(t *testing.T) {
	collector := &testCollector{}
	err := parseThreatFoxJSON([]byte(`{invalid`), collector, "threatfox-online")
	require.Error(t, err)
}

func TestIsZip(t *testing.T) {
	assert.True(t, isZip([]byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00}))
	assert.False(t, isZip([]byte{0x7B, 0x22, 0x6B, 0x65, 0x79})) // "{"key…
	assert.False(t, isZip(nil))
	assert.False(t, isZip([]byte{0x50, 0x4B})) // too short
}

func TestResolveThreatFoxURL(t *testing.T) {
	result := resolveThreatFoxURL("https://example.com/{token}/path", "abc123")
	assert.Equal(t, "https://example.com/abc123/path", result)

	result2 := resolveThreatFoxURL("", "key")
	assert.Equal(t, "", result2)
}

func TestIOCToEntry_ipPort_Invalid(t *testing.T) {
	entry, err := iocToEntry(&threatFoxIOC{
		IOCValue: "invalid-value",
		IOCType:  "ip:port",
	}, "test")
	assert.NoError(t, err)
	assert.Nil(t, entry, "invalid ip:port should return nil entry")

	// Valid but not an IP after split → a.b.c.d is not a valid IP
	entry2, err := iocToEntry(&threatFoxIOC{
		IOCValue: "a.b.c.d:80",
		IOCType:  "ip:port",
	}, "test")
	assert.NoError(t, err)
	assert.Nil(t, entry2, "four-label host:port should be skipped — not a valid IP")
}

func TestIOCToEntry_SkipHash(t *testing.T) {
	entry, err := iocToEntry(&threatFoxIOC{
		IOCValue: "abc123...",
		IOCType:  "sha256_hash",
	}, "test")
	assert.NoError(t, err)
	assert.Nil(t, entry, "hash should be skipped")
}

func TestParseThreatFoxResponse_PlainJSON(t *testing.T) {
	data := []byte(`{"1":[{"ioc_value":"evil.com","ioc_type":"domain","threat_type":"malware"}]}`)
	collector := &testCollector{}
	err := parseThreatFoxResponse(data, collector, "threatfox-online")
	require.NoError(t, err)
	assert.Equal(t, 1, len(collector.entries))
}

func TestParseThreatFoxResponse_Zip(t *testing.T) {
	// Build an in-memory zip with a full.json inside
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	f, _ := zipWriter.Create("full.json")
	f.Write([]byte(`{"1":[{"ioc_value":"evil.com","ioc_type":"domain","threat_type":"malware"}]}`))
	zipWriter.Close()

	collector := &testCollector{}
	err := parseThreatFoxResponse(zipBuf.Bytes(), collector, "threatfox-online")
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
	err := parseThreatFoxResponse(zipBuf.Bytes(), collector, "threatfox-online")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full.json not found")
}
