//go:build e2e
// +build e2e
//
// Bloom-Aware E2E Test Suite
// ==========================
// No provider dependency. BloomManager is populated directly with known URLs,
// then checked via the V2 API (check/hit/bulk). Covers every bloom type,
// parent path traversal, first-hit-wins, and edge cases.
//
// Run: go test -tags=e2e ./features/e2e/... -v -timeout 60s

package e2e

import (
	"blacked/features/bloom"
	"blacked/features/web/middlewares"
	v2 "blacked/features/web/handlers/v2"
	"blacked/internal/query"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Helpers
// ============================================================================

func mustParse(t *testing.T, raw string) *bloom.URLKeys {
	t.Helper()
	keys, err := bloom.ParseURL(raw)
	require.NoError(t, err, "ParseURL(%q)", raw)
	return keys
}

func mustPopulate(t *testing.T, bm *bloom.BloomManager, src, raw string) {
	t.Helper()
	bm.PopulateEntry(src, mustParse(t, raw))
}

// setupMinimalServer creates an httptest.Server with V2 API routes wired to a
// BloomManager. No DB, no provider sync, no cache — pure bloom pipeline.
func setupMinimalServer(t *testing.T, bm *bloom.BloomManager) *httptest.Server {
	t.Helper()
	e := echo.New()
	middlewares.ConfigureValidator(e)
	checker := v2.NewBloomAdapter(bm)
	scorer := query.NewScorer(nil)
	svc := query.NewQueryService(checker, nil, scorer)
	handler := v2.NewQueryHandlerWithDeps(svc)

	g := e.Group("/api/v1")
	g.GET("/check", handler.Check)
	g.GET("/hit", handler.Hit)
	g.POST("/bulk-check", handler.BulkCheck)
	g.POST("/bulk-hit", handler.BulkHit)

	return httptest.NewServer(e)
}

// e2eRequest performs a GET check/hit and parses the response.
func e2eRequest(t *testing.T, srv *httptest.Server, endpoint, targetURL string) (int, *query.LikelyResponse, *query.QueryResponse) {
	t.Helper()
	fullURL := fmt.Sprintf("%s/api/v1/%s?url=%s", srv.URL, endpoint, url.QueryEscape(targetURL))
	resp, err := http.Get(fullURL)
	require.NoError(t, err, "GET %s", fullURL)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var lr query.LikelyResponse
	var qr query.QueryResponse

	if endpoint == "check" && len(body) > 0 {
		json.Unmarshal(body, &lr)
	} else if endpoint == "hit" && len(body) > 0 {
		json.Unmarshal(body, &qr)
	}

	return resp.StatusCode, &lr, &qr
}

// e2eBulkRequest performs a POST to bulk-check or bulk-hit.
func e2eBulkRequest(t *testing.T, srv *httptest.Server, endpoint string, urls []string) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(map[string][]string{"urls": urls})
	fullURL := fmt.Sprintf("%s/api/v1/%s", srv.URL, endpoint)
	resp, err := http.Post(fullURL, "application/json", bytes.NewReader(payload))
	require.NoError(t, err, "POST %s", fullURL)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

// ============================================================================
// Bloom Layer Tests — each populates its own BloomManager for isolation
// ============================================================================

func TestBlacked_BloomE2E(t *testing.T) {
	// ------------------------------------------------------------------
	// 1. Domain bloom: bare domain → subdomain check
	// ------------------------------------------------------------------
	t.Run("DomainBloom", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://malicious.com") // determineBloomTarget → BloomDomain "malicious.com"
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		// subdomain should hit Domain bloom (shallowest in check chain)
		status, lr, _ := e2eRequest(t, srv, "check", "https://sub.malicious.com/path")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)
		require.Len(t, lr.Matches, 1)
		require.Equal(t, "domain", lr.Matches[0].Type)
		require.Equal(t, "oisd", lr.Matches[0].SourceID)
		require.Equal(t, "malicious.com", lr.Matches[0].Key)
		fmt.Println("  ✓ DomainBloom: bare domain → subdomain hits Domain bloom")
	})

	// ------------------------------------------------------------------
	// 2. Host bloom: subdomain → exact host check
	// ------------------------------------------------------------------
	t.Run("HostBloom", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://sub.malicious.com") // determineBloomTarget → BloomHost "sub.malicious.com"
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, lr, _ := e2eRequest(t, srv, "check", "https://sub.malicious.com/anything")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)
		require.Len(t, lr.Matches, 1)
		require.Equal(t, "host", lr.Matches[0].Type)
		require.Equal(t, "sub.malicious.com", lr.Matches[0].Key)
		fmt.Println("  ✓ HostBloom: subdomain → exact host hits Host bloom")
	})

	// ------------------------------------------------------------------
	// 3. HostPath bloom: path URL → exact path check
	// ------------------------------------------------------------------
	t.Run("HostPathBloom", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "urlhaus", "https://evil.com/malware") // → HostPath "evil.com/malware"
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, lr, _ := e2eRequest(t, srv, "check", "https://evil.com/malware")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)
		require.Len(t, lr.Matches, 1)
		require.Equal(t, "host_path", lr.Matches[0].Type)
		require.Equal(t, "evil.com/malware", lr.Matches[0].Key)
		fmt.Println("  ✓ HostPathBloom: path URL → exact path hits HostPath bloom")
	})

	// ------------------------------------------------------------------
	// 4. Parent path bloom: /a populated, /a/b/c checked → HostPath hit
	// ------------------------------------------------------------------
	t.Run("ParentPathBloom", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "urlhaus", "https://evil.com/a") // → HostPath "evil.com/a"
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		// "evil.com/a/b/c" → check keys: Domain→Host→HostPath(/a)→HostPath(/a/b)→HostPath(/a/b/c)
		// "evil.com/a" hits on the first parent path check
		status, lr, _ := e2eRequest(t, srv, "check", "https://evil.com/a/b/c")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)
		require.Len(t, lr.Matches, 1)
		require.Equal(t, "host_path", lr.Matches[0].Type)
		require.Equal(t, "evil.com/a", lr.Matches[0].Key)
		fmt.Println("  ✓ ParentPathBloom: parent populated → child check hits via parentPaths traversal")
	})

	// ------------------------------------------------------------------
	// 5. File bloom: .exe URL → File match
	// ------------------------------------------------------------------
	t.Run("FileBloom", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "urlhaus", "https://cdn.evil.com/exploit.exe") // → BloomFile "exploit.exe"
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, lr, _ := e2eRequest(t, srv, "check", "https://cdn.evil.com/exploit.exe")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)
		// Should hit File bloom (most specific, but after HostPath parents in chain)
		hasFile := false
		for _, m := range lr.Matches {
			if m.Type == "file" {
				hasFile = true
				require.Equal(t, "exploit.exe", m.Key)
			}
		}
		require.True(t, hasFile, "expected File bloom match")
		fmt.Println("  ✓ FileBloom: .exe URL → File bloom match")
	})

	// ------------------------------------------------------------------
	// 6. FullURL bloom: .php?ref=... → FullURL match
	// ------------------------------------------------------------------
	t.Run("FullURLBloom", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "openphish", "https://phish.example.com/login.php?ref=evil") // → FullURL
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, lr, _ := e2eRequest(t, srv, "check", "https://phish.example.com/login.php?ref=evil")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)

		// FullURL match should be present
		hasFullURL := false
		for _, m := range lr.Matches {
			if m.Type == "full_url" {
				hasFullURL = true
				require.Equal(t, "phish.example.com/login.php?ref=evil", m.Key)
			}
		}
		require.True(t, hasFullURL, "expected FullURL bloom match")

		// Different query → MISS (provider responsibility)
		status2, lr2, _ := e2eRequest(t, srv, "check", "https://phish.example.com/login.php?ref=safe")
		require.Equal(t, 204, status2)
		require.False(t, lr2.Likely)
		fmt.Println("  ✓ FullURLBloom: .php?q= → FullURL match, different query → miss")
	})

	// ------------------------------------------------------------------
	// 7. IP bloom: IP URL → IP match
	// ------------------------------------------------------------------
	t.Run("IPBloom", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		// "http://192.168.1.1:8080/malware" → IP="192.168.1.1", determineBloomTarget
		// now checks IP first (rule 0), so it goes to BloomIP "192.168.1.1"
		mustPopulate(t, bm, "urlhaus", "http://192.168.1.1:8080/malware")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		// Check same IP — should hit IP bloom
		status, lr, _ := e2eRequest(t, srv, "check", "http://192.168.1.1:8080/other-path")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)

		hasIP := false
		for _, m := range lr.Matches {
			if m.Type == "ip" {
				hasIP = true
				require.Equal(t, "192.168.1.1", m.Key)
				require.Equal(t, "urlhaus", m.SourceID)
			}
		}
		require.True(t, hasIP, "expected IP bloom match")

		// Different IP → miss
		status2, lr2, _ := e2eRequest(t, srv, "check", "http://10.0.0.1/test")
		require.Equal(t, 204, status2)
		require.False(t, lr2.Likely)

		fmt.Println("  ✓ IPBloom: IP URL → IP bloom match, different IP → miss")
	})

	// ------------------------------------------------------------------
	// 8. First-hit-wins: Domain + Host populated, Domain hits first
	// ------------------------------------------------------------------
	t.Run("FirstHitWinsDomain", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		// Populate Domain bloom (shallow)
		mustPopulate(t, bm, "oisd", "https://evil.com")
		// Populate HostPath bloom (deeper — should not win)
		mustPopulate(t, bm, "urlhaus", "https://evil.com/deep-payload")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		// Check "evil.com/deep-payload" → check chain: Domain→Host→HostPath(/deep)→HostPath(/deep-payload)→...
		// Domain("evil.com") matches from oisd FIRST → first-hit-wins
		status, lr, _ := e2eRequest(t, srv, "check", "https://evil.com/deep-payload")
		require.Equal(t, 200, status)
		require.True(t, lr.Likely)
		require.Len(t, lr.Matches, 1, "first-hit-wins: only one match expected")
		require.Equal(t, "domain", lr.Matches[0].Type, "shallowest bloom should win")
		require.Equal(t, "oisd", lr.Matches[0].SourceID)
		fmt.Println("  ✓ FirstHitWinsDomain: Domain beats HostPath in parallel check")
	})

	// ------------------------------------------------------------------
	// 9. Clean miss: google.com → 204
	// ------------------------------------------------------------------
	t.Run("CleanMiss", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://evil.com")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, lr, _ := e2eRequest(t, srv, "check", "https://google.com")
		require.Equal(t, 204, status)
		require.False(t, lr.Likely)
		fmt.Println("  ✓ CleanMiss: google.com → 204")
	})

	// ------------------------------------------------------------------
	// 10. Hit endpoint: returns blocked=true with confidence
	// ------------------------------------------------------------------
	t.Run("HitEndpoint", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://evil.com")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, _, qr := e2eRequest(t, srv, "hit", "https://sub.evil.com/path")
		require.Equal(t, 200, status)
		require.True(t, qr.Blocked)
		require.Len(t, qr.Matches, 1)
		require.Equal(t, "domain", qr.Matches[0].Type)
		require.Equal(t, "oisd", qr.Matches[0].SourceID)
		// Confidence should be > 0 (scorer with single source)
		require.Greater(t, qr.Confidence, 0.0, "expected confidence > 0")
		require.NotEmpty(t, qr.Level)
		fmt.Println("  ✓ HitEndpoint: returns blocked=true, confidence, level")
	})

	// ------------------------------------------------------------------
	// 11. Hit on clean URL → 204
	// ------------------------------------------------------------------
	t.Run("HitClean", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://evil.com")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, _, qr := e2eRequest(t, srv, "hit", "https://google.com")
		require.Equal(t, 204, status)
		require.False(t, qr.Blocked)
		fmt.Println("  ✓ HitClean: clean URL → 204 on hit endpoint")
	})

	// ------------------------------------------------------------------
	// 12. Empty URL → 204
	// ------------------------------------------------------------------
	t.Run("EmptyURL", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://evil.com")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, _, _ := e2eRequest(t, srv, "check", "")
		require.Equal(t, 204, status)

		status2, _, _ := e2eRequest(t, srv, "hit", "")
		require.Equal(t, 204, status2)
		fmt.Println("  ✓ EmptyURL: empty URL → 204 on both endpoints")
	})

	// ------------------------------------------------------------------
	// 13. Bulk check: mixed results
	// ------------------------------------------------------------------
	t.Run("BulkCheck", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://evil.com")
		mustPopulate(t, bm, "urlhaus", "https://phish.com/campaign")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, body := e2eBulkRequest(t, srv, "bulk-check", []string{
			"https://sub.evil.com/path",
			"https://phish.com/campaign",
			"https://google.com",
			"https://github.com",
		})
		require.Equal(t, 200, status)

		var results []query.LikelyResponse
		require.NoError(t, json.Unmarshal(body, &results))
		require.Len(t, results, 4)

		// evil.com — Domain hit
		require.True(t, results[0].Likely, "evil.com should be likely=true")
		require.Len(t, results[0].Matches, 1)
		require.Equal(t, "domain", results[0].Matches[0].Type)

		// phish.com/campaign — HostPath hit
		require.True(t, results[1].Likely, "phish.com should be likely=true")

		// google.com — miss
		require.False(t, results[2].Likely, "google.com should be miss")

		// github.com — miss
		require.False(t, results[3].Likely, "github.com should be miss")

		fmt.Println("  ✓ BulkCheck: 4 URLs, 2 hits + 2 misses")
	})

	// ------------------------------------------------------------------
	// 14. Bulk hit: mixed results with confidence
	// ------------------------------------------------------------------
	t.Run("BulkHit", func(t *testing.T) {
		bm := bloom.NewBloomManager(1000)
		mustPopulate(t, bm, "oisd", "https://evil.com")
		srv := setupMinimalServer(t, bm)
		defer srv.Close()

		status, body := e2eBulkRequest(t, srv, "bulk-hit", []string{
			"https://sub.evil.com/path",
			"https://google.com",
		})
		require.Equal(t, 200, status)

		var results []query.QueryResponse
		require.NoError(t, json.Unmarshal(body, &results))
		require.Len(t, results, 2)

		require.True(t, results[0].Blocked, "evil.com should be blocked=true")
		require.Greater(t, results[0].Confidence, 0.0)
		require.NotEmpty(t, results[0].Level)

		require.False(t, results[1].Blocked, "google.com should be blocked=false")
		require.Equal(t, 0.0, results[1].Confidence)
		require.Equal(t, "informational", results[1].Level)

		fmt.Println("  ✓ BulkHit: 2 URLs, 1 blocked + 1 clean with confidence/level")
	})
}
