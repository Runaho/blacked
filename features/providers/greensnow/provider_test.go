package greensnow

import (
	"testing"

	"blacked/features/entries"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

const testPID = "test-process-id"

// testCollector is a minimal mock satisfying entry_collector.Collector.
type testCollector struct {
	entries []*entries.Entry
}

func (c *testCollector) Submit(entry *entries.Entry) { c.entries = append(c.entries, entry) }
func (c *testCollector) Wait()                        {}
func (c *testCollector) Close()                       {}
func (c *testCollector) GetProcessedCount(source string) int {
	return len(c.entries)
}
func (c *testCollector) StartProviderProcessing(_, _ string)  {}
func (c *testCollector) FinishProviderProcessing(_, _ string) (int, time.Duration, bool) {
	return len(c.entries), 0, true
}

func TestParseGreenSnowLine_FiveValidIPs(t *testing.T) {
	ips := []string{
		"1.2.3.4",
		"5.6.7.8",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
	}

	for _, ip := range ips {
		entry, err := parseGreenSnowLine(ip, testPID)
		require.NoError(t, err)
		require.NotNil(t, entry)

		assert.Equal(t, ip, entry.Host)
		assert.Equal(t, ip, entry.Domain)
		assert.Nil(t, entry.SubDomains)
		assert.Equal(t, providerName, entry.Source)
		assert.Equal(t, testPID, entry.ProcessID)
		assert.Equal(t, providerCategory, entry.Category)
		assert.Empty(t, entry.Scheme)
		assert.Empty(t, entry.Path)
	}
}

func TestParseGreenSnowLine_EmptyLine(t *testing.T) {
	entry, err := parseGreenSnowLine("", testPID)
	require.NoError(t, err)
	assert.Nil(t, entry, "empty line should return nil entry")
}

func TestParseGreenSnowLine_WhitespaceOnly(t *testing.T) {
	entry, err := parseGreenSnowLine("  \t  ", testPID)
	require.NoError(t, err)
	assert.Nil(t, entry, "whitespace-only line should return nil entry")
}

func TestParseGreenSnowLine_InvalidIP(t *testing.T) {
	tests := []string{
		"not-an-ip",
		"256.256.256.256",
		"1.2.3",
		"abc.def.ghi.jkl",
		"1.2.3.4.5",
	}

	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			entry, err := parseGreenSnowLine(tc, testPID)
			require.NoError(t, err)
			assert.Nil(t, entry, "invalid IP %q should return nil entry", tc)
		})
	}
}

func TestParseGreenSnowLine_IPv6Skip(t *testing.T) {
	ipv6Addrs := []string{
		"::1",
		"2001:db8::1",
		"fe80::1",
		"2001:4860:4860::8888",
		"2a00:1450:4001:830::200e",
	}

	for _, ipv6 := range ipv6Addrs {
		t.Run(ipv6, func(t *testing.T) {
			entry, err := parseGreenSnowLine(ipv6, testPID)
			require.NoError(t, err)
			assert.Nil(t, entry, "IPv6 address %q should be skipped", ipv6)
		})
	}
}

func TestParseGreenSnowLine_IPv4MappedIPv6(t *testing.T) {
	// ::ffff:x.x.x.x is an IPv4-mapped IPv6 address. Go's To4() extracts
	// the IPv4 portion, so our parser treats it as a valid IPv4 entry.
	entry, err := parseGreenSnowLine("::ffff:192.168.1.1", testPID)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "::ffff:192.168.1.1", entry.Host)
}

func TestParseGreenSnowLine_PublicIPv4(t *testing.T) {
	entry, err := parseGreenSnowLine("8.8.8.8", testPID)
	require.NoError(t, err)
	require.NotNil(t, entry)

	assert.Equal(t, "8.8.8.8", entry.Host)
	assert.Equal(t, "8.8.8.8", entry.Domain)
	assert.Equal(t, providerCategory, entry.Category)
}

func TestParseGreenSnowLine_MixedInput(t *testing.T) {
	inputs := []string{
		"1.2.3.4",           // valid
		"",                  // empty
		"not-an-ip",         // invalid
		"5.6.7.8",           // valid
		"::1",               // IPv6 — skip
		"2001:db8::1",       // IPv6 — skip
		"  10.0.0.1  ",      // valid with whitespace
		"256.256.256.256",   // invalid
	}

	validCount := 0
	for _, input := range inputs {
		entry, err := parseGreenSnowLine(input, testPID)
		require.NoError(t, err)
		if entry != nil {
			validCount++
		}
	}

	assert.Equal(t, 3, validCount, "should have exactly 3 valid entries")
}

func TestParseGreenSnowLine_SpecialAddresses(t *testing.T) {
	// Loopback and link-local are still valid IPs — they parse correctly.
	entry, err := parseGreenSnowLine("127.0.0.1", testPID)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "127.0.0.1", entry.Host)

	entry, err = parseGreenSnowLine("169.254.169.254", testPID)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "169.254.169.254", entry.Host)
}

func TestParseGreenSnowLine_NoSetURL(t *testing.T) {
	// Verify SetURL is NOT called — URL-derived fields remain empty.
	// WithIP is used instead for raw IP handling.
	entry, err := parseGreenSnowLine("1.2.3.4", testPID)
	require.NoError(t, err)
	require.NotNil(t, entry)

	assert.Empty(t, entry.Scheme, "Scheme should be empty — SetURL not called")
	assert.Empty(t, entry.Path, "Path should be empty — SetURL not called")
	assert.Empty(t, entry.RawQuery, "RawQuery should be empty — SetURL not called")
	// SourceURL is set manually in the provider for UNIQUE constraint, not via SetURL
	assert.Equal(t, "https://blocklist.greensnow.co/greensnow.txt", entry.SourceURL,
		"SourceURL should be the provider's default URL — set manually for uniqueness")
}
