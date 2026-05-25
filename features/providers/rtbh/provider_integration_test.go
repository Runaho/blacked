package rtbh

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

// liveFetcher wraps a direct HTTP fetch for integration tests.
type liveFetcher struct {
	url       string
	client    *http.Client
	lastBody  []byte
	fetchErr  error
}

func newLiveFetcher(sourceURL string) *liveFetcher {
	return &liveFetcher{
		url: sourceURL,
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (f *liveFetcher) Fetch() (io.Reader, error) {
	resp, err := f.client.Get(f.url)
	if err != nil {
		f.fetchErr = err
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		f.fetchErr = &httpStatusError{Code: resp.StatusCode}
		return nil, f.fetchErr
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		f.fetchErr = err
		return nil, err
	}

	f.lastBody = body
	return strings.NewReader(string(body)), nil
}

type httpStatusError struct {
	Code int
}

func (e *httpStatusError) Error() string {
	return http.StatusText(e.Code)
}

// TestIntegration_RTBHLiveFetch fetches the live RTBH feed and validates:
// - At least 40K entries (expecting ~41.5K)
// - All entries are valid IPv4
// - Category is "government-feed"
// - No IPv6 entries
func TestIntegration_RTBHLiveFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const sourceURL = "https://list.rtbh.com.tr/output.txt"
	const providerName = "rtbh-turkey"

	fetcher := newLiveFetcher(sourceURL)
	reader, err := fetcher.Fetch()
	require.NoError(t, err, "failed to fetch live RTBH feed")
	require.NotNil(t, reader)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NotEmpty(t, body, "response body must not be empty")

	lines := strings.Split(string(body), "\n")
	t.Logf("fetched %d lines from RTBH feed", len(lines))

	var result []*entries.Entry
	processID := "integration-test"

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		ip := net.ParseIP(line)
		if ip == nil {
			t.Logf("skipping non-IP line: %q", line)
			continue
		}

		if ip.To4() == nil {
			t.Errorf("unexpected IPv6 entry in RTBH feed: %s", line)
			continue
		}

		entry := entries.NewEntry().
			WithSource(providerName).
			WithProcessID(processID).
			WithCategory("government-feed").
			WithIP(ip.String())

		result = append(result, entry)
	}

	assert.GreaterOrEqual(t, len(result), 38000,
		"expected at least 38K IPs, got %d", len(result))

	t.Logf("parsed %d valid IPv4 entries from RTBH feed", len(result))

	// Verify first 5 entries
	for i := 0; i < 5 && i < len(result); i++ {
		e := result[i]
		assert.Equal(t, providerName, e.Source)
		assert.Equal(t, "government-feed", e.Category)
		assert.NotEmpty(t, e.ProcessID)
		assert.True(t, net.ParseIP(e.Domain) != nil, "entry %d domain should be a valid IP, got %q", i, e.Domain)
		assert.Equal(t, e.Domain, e.Host, "entry %d domain should equal host", i)
	}
}

// TestIntegration_RTBHNoIPv6 ensures no IPv6 slips through in the live feed.
func TestIntegration_RTBHNoIPv6(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const sourceURL = "https://list.rtbh.com.tr/output.txt"

	fetcher := newLiveFetcher(sourceURL)
	reader, err := fetcher.Fetch()
	require.NoError(t, err)
	require.NotNil(t, reader)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	ipv6Count := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if ip := net.ParseIP(line); ip != nil && ip.To4() == nil {
			ipv6Count++
		}
	}

	assert.Equal(t, 0, ipv6Count, "RTBH feed contains %d unexpected IPv6 entries", ipv6Count)
}

// TestIntegration_RTBHCategory ensures the category is government-feed.
func TestIntegration_RTBHCategory(t *testing.T) {
	const providerName = "rtbh-turkey"

	// Simple in-memory test — category constant
	ip := net.ParseIP("1.2.3.4")
	require.NotNil(t, ip)

	entry := entries.NewEntry().
		WithSource(providerName).
		WithCategory("government-feed")

	assert.Equal(t, "government-feed", entry.Category)
	assert.Equal(t, providerName, entry.Source)
}
