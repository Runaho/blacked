//go:build e2e
// +build e2e
//
// Run: go test -tags=e2e ./features/e2e/... -v -timeout 600s

package e2e

import (
	"blacked/features/bloom"
	"blacked/features/cache"
	"blacked/features/entry_collector"
	"blacked/features/providers"
	provider_services "blacked/features/providers/services"
	"blacked/features/web/handlers/health"
	"blacked/features/web/handlers/provider"
	"blacked/features/web/handlers/response"
	"blacked/features/web/middlewares"
	v2 "blacked/features/web/handlers/v2"
	"blacked/internal/config"
	"blacked/internal/db"
	"blacked/internal/logger"
	"blacked/internal/query"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Data Structures
// ============================================================================

// E2ETestURL mirrors the JSON structure for e2e test data.
type E2ETestURL struct {
	URL            string `json:"url"`
	Source         string `json:"source,omitempty"`
	BloomLayer     string `json:"bloom_layer,omitempty"` // Which bloom type we expect (domain, host, host_path, file, full_url, ip)
	Description    string `json:"description"`
	ExpectedExists bool   `json:"expected_exists"`
	ExpectedStatus int    `json:"expected_status,omitempty"`
}

type E2ETestData struct {
	MaliciousURLs []E2ETestURL `json:"malicious_urls"`
	CleanURLs     []E2ETestURL `json:"clean_urls"`
	EdgeCases     []E2ETestURL `json:"edge_cases"`
}

// ============================================================================
// Test Suite
// ============================================================================

func TestBlacked_E2E(t *testing.T) {
	fmt.Println("\n========================================")
	fmt.Println("  BLACKED E2E Test Suite (Bloom-Aware)")
	fmt.Println("========================================")

	// --- Step 1: Initialize test infrastructure ---
	fmt.Println("=== Step 1: Initializing test infrastructure ===")
	server, bloomMgr := setupE2EServer(t)
	defer server.Close()
	fmt.Printf("Test server started at %s\n\n", server.URL)

	// --- Step 2: Trigger provider processing ---
	fmt.Println("=== Step 2: Triggering provider sync ===")
	processID := triggerProviderSync(t, server)
	fmt.Printf("Process started: %s\n", processID)

	// --- Step 3: Wait for provider processing to complete ---
	fmt.Println("=== Step 3: Waiting for provider sync to complete ===")
	waitForProcessComplete(t, server, processID, 180*time.Second)
	fmt.Println("Provider sync completed successfully")

	// --- Step 4: Populate BloomManager from DB ---
	fmt.Println("=== Step 4: Populating BloomManager from DB ===")
	populateBloomFromDB(t, bloomMgr)
	fmt.Println("BloomManager populated from DB entries")

	// Verify bloom is not cold start
	stats := bloomMgr.Stats()
	for k, v := range stats {
		if v > 0 {
			fmt.Printf("  BloomSet %s: %d sources\n", k, v)
		}
	}
	if bloomMgr.ColdStart() {
		t.Fatal("BloomManager is still cold after populate — no filters have entries")
	}
	fmt.Println("BloomManager verified: non-cold")

	// --- Step 5: Load test URLs ---
	testData := loadE2ETestData(t)

	// --- Step 6: Run E2E tests ---
	passed, failed := 0, 0

	// Health check
	fmt.Println("=== Step 6: Health check ===")
	if assertHealth(t, server) {
		passed++
	} else {
		failed++
	}

	// Bloom layer tests — check each malicious URL
	fmt.Println("\n=== Step 7: Bloom layer tests — known malicious URLs ===")
	for _, u := range testData.MaliciousURLs {
		if assertBloomHit(t, server, u) {
			passed++
		} else {
			failed++
		}
	}

	// Full hit tests (bloom + score)
	fmt.Println("\n=== Step 8: Full hit tests ===")
	for _, u := range testData.MaliciousURLs {
		if assertFullHit(t, server, u) {
			passed++
		} else {
			failed++
		}
	}

	// Clean URL queries (bloom misses)
	fmt.Println("\n=== Step 9: Clean URL queries ===")
	for _, u := range testData.CleanURLs {
		if assertBloomMiss(t, server, u) {
			passed++
		} else {
			failed++
		}
	}

	// Edge cases
	fmt.Println("\n=== Step 10: Edge case queries ===")
	for _, u := range testData.EdgeCases {
		if assertEdgeCase(t, server, u) {
			passed++
		} else {
			failed++
		}
	}

	// --- Step 11: Re-sync + re-populate for stability ---
	fmt.Println("\n=== Step 11: Re-sync stability test ===")
	processID2 := triggerProviderSync(t, server)
	waitForProcessComplete(t, server, processID2, 180*time.Second)

	// Re-populate bloom after re-sync
	populateBloomFromDB(t, bloomMgr)
	if bloomMgr.ColdStart() {
		t.Fatal("BloomManager cold after re-sync — expected populated")
	}

	// Re-run health + one malicious + one clean
	if assertHealth(t, server) {
		passed++
	} else {
		failed++
	}
	if len(testData.MaliciousURLs) > 0 {
		if assertBloomHit(t, server, testData.MaliciousURLs[0]) {
			passed++
		} else {
			failed++
		}
	}
	if len(testData.CleanURLs) > 0 {
		if assertBloomMiss(t, server, testData.CleanURLs[0]) {
			passed++
		} else {
			failed++
		}
	}

	// --- Report ---
	total := passed + failed
	fmt.Println("\n========================================")
	fmt.Printf("  E2E: %d/%d tests passed, %d failed\n", passed, total, failed)
	fmt.Println("========================================")

	require.Zero(t, failed, "E2E suite had %d failures", failed)
}

// ============================================================================
// Test Assertions
// ============================================================================

// assertBloomHit checks /api/v1/check returns 200 (Likely=true) for a malicious URL.
func assertBloomHit(t *testing.T, server *httptest.Server, u E2ETestURL) bool {
	url := server.URL + "/api/v1/check?url=" + u.URL
	resp, err := http.Get(url)
	if !assert.NoError(t, err, "Bloom check request failed for %s", u.Description) {
		return false
	}
	defer resp.Body.Close()

	// Expected hit → 200 OK with likely=true
	if !assert.Equal(t, http.StatusOK, resp.StatusCode,
		"Expected 200 for %s (%s) — bloom should HIT", u.Description, u.URL) {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("  Response body: %s", string(body))
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if !assert.NoError(t, err) {
		return false
	}

	var result query.LikelyResponse
	if !assert.NoError(t, json.Unmarshal(body, &result)) {
		return false
	}

	if !assert.True(t, result.Likely,
		"Likely=true expected for %s (%s)", u.Description, u.URL) {
		return false
	}

	if !assert.NotZero(t, len(result.Matches),
		"Expected at least one bloom match for %s", u.Description) {
		return false
	}

	// Log match details for visibility
	fmt.Printf("  ✓ %s → likely=true (type=%s, matches=%d)\n",
		u.Description, result.Matches[0].Type, len(result.Matches))
	return true
}

// assertFullHit checks /api/v1/hit returns 200 (Blocked=true) with score info.
func assertFullHit(t *testing.T, server *httptest.Server, u E2ETestURL) bool {
	url := server.URL + "/api/v1/hit?url=" + u.URL
	resp, err := http.Get(url)
	if !assert.NoError(t, err, "Hit request failed for %s", u.Description) {
		return false
	}
	defer resp.Body.Close()

	if !assert.Equal(t, http.StatusOK, resp.StatusCode,
		"Expected 200 for hit %s (%s)", u.Description, u.URL) {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("  Response body: %s", string(body))
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if !assert.NoError(t, err) {
		return false
	}

	var result query.QueryResponse
	if !assert.NoError(t, json.Unmarshal(body, &result)) {
		return false
	}

	if !assert.True(t, result.Blocked,
		"Blocked=true expected for %s (%s)", u.Description, u.URL) {
		return false
	}

	// Hit should have confidence > 0 and a level
	if !assert.Greater(t, result.Confidence, 0.0,
		"Confidence > 0 expected for %s", u.Description) {
		return false
	}

	fmt.Printf("  ✓ %s → blocked=true (confidence=%.2f, level=%s, took=%dms)\n",
		u.Description, result.Confidence, result.Level, result.TookMs)
	return true
}

// assertBloomMiss checks /api/v1/check returns 204 for a clean URL.
func assertBloomMiss(t *testing.T, server *httptest.Server, u E2ETestURL) bool {
	url := server.URL + "/api/v1/check?url=" + u.URL
	resp, err := http.Get(url)
	if !assert.NoError(t, err, "Clean URL request failed for %s", u.Description) {
		return false
	}
	defer resp.Body.Close()

	// Expected miss → 204 No Content
	if !assert.Equal(t, http.StatusNoContent, resp.StatusCode,
		"Expected 204 for clean URL %s (%s)", u.Description, u.URL) {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("  Response body: %s", string(body))
		return false
	}

	fmt.Printf("  ✓ %s → 204 (correct miss)\n", u.Description)
	return true
}

// assertEdgeCase handles edge case scenarios.
func assertEdgeCase(t *testing.T, server *httptest.Server, u E2ETestURL) bool {
	url := server.URL + "/api/v1/hit?url=" + u.URL
	if u.ExpectedStatus != 0 {
		resp, err := http.Get(url)
		if !assert.NoError(t, err, "Edge case request failed: %s", u.Description) {
			return false
		}
		defer resp.Body.Close()
		if !assert.Equal(t, u.ExpectedStatus, resp.StatusCode,
			"Status code mismatch for edge case: %s", u.Description) {
			return false
		}
		fmt.Printf("  ✓ %s → %d (expected)\n", u.Description, resp.StatusCode)
		return true
	}

	resp, err := http.Get(url)
	if !assert.NoError(t, err, "Edge case request failed: %s", u.Description) {
		return false
	}
	defer resp.Body.Close()

	// Edge cases are lenient — log but don't fail on unexpected hits
	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Warn().Int("status", resp.StatusCode).Str("url", u.URL).
			Str("body", string(body)).Msg("Edge case returned hit")
	}
	fmt.Printf("  ✓ %s → %d\n", u.Description, resp.StatusCode)
	return true
}

// assertHealth checks /health/status returns ok.
func assertHealth(t *testing.T, server *httptest.Server) bool {
	resp, err := http.Get(server.URL + "/health/status")
	if !assert.NoError(t, err) {
		return false
	}
	defer resp.Body.Close()

	if !assert.Equal(t, http.StatusOK, resp.StatusCode, "Health check should return 200") {
		return false
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if !assert.NoError(t, json.Unmarshal(body, &result)) {
		return false
	}
	assert.Equal(t, "ok", result["status"], "Health status should be ok")
	return true
}

// ============================================================================
// Setup
// ============================================================================

// setupE2EServer initializes a full test server with:
// - Config + DB + providers + cache
// - BloomManager (populated after provider sync)
// - V2 routes + provider routes + health
// Returns *httptest.Server and *bloom.BloomManager for test control.
func setupE2EServer(t *testing.T) (*httptest.Server, *bloom.BloomManager) {
	logger.InitializeLogger()

	// Point config to a test config
	testConfigDir := t.TempDir()
	configContent := fmt.Sprintf(`
[app]
log_level = "warn"
environment = "test"

[server]
port = 0
health_check = true
allow_origins = ["*"]

[database]
driver = "sqlite"
host = ""
port = 0
username = ""
password = ""
name = ""

[cache]
use_bloom = true
size_mb = 10
enable_compression = false

[provider]
enabled = true
run_at_startup = false
enabled_providers = ["OISD_BIG", "URLHAUS", "OPENPHISH"]

[log]
level = "warn"
`)
	configPath := filepath.Join(testConfigDir, "test.env.toml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	configFile := filepath.Join(testConfigDir, "test.env.toml")
	os.Setenv("CONFIG_FILE", configFile)
	t.Cleanup(func() { os.Unsetenv("CONFIG_FILE") })

	err = config.InitConfig()
	require.NoError(t, err)

	// Initialize DB with temp path
	dbPath := filepath.Join(testConfigDir, "blacked-e2e.db")
	os.Setenv("DB_PATH", dbPath)
	t.Cleanup(func() { os.Unsetenv("DB_PATH") })

	db.InitializeDB()
	writeDB, err := db.GetWriteDB()
	require.NoError(t, err)

	err = db.FullMigration(writeDB)
	require.NoError(t, err)

	// Initialize providers
	_, err = providers.InitProviders()
	require.NoError(t, err)

	// Initialize cache (needed for provider sync — cache.BloomFilter)
	ctx := context.Background()
	err = cache.InitializeCache(ctx)
	require.NoError(t, err)

	// Initialize PondCollector (needed for provider sync)
	entry_collector.InitPondCollector(ctx, writeDB)

	// Build Echo app manually
	e := echo.New()
	e.Server.Addr = ":0"

	// Configure validator
	middlewares.ConfigureValidator(e)

	// Configure custom error handling
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		he := err.(*echo.HTTPError)
		response.ErrorWithDetails(c, he.Code, http.StatusText(he.Code), fmt.Sprintf("%v", he.Message))
	}

	// ---- Create shared BloomManager (large capacity for real providers) ----
	bloomMgr := bloom.NewBloomManager(10000000) // 10M expected items

	// ---- Provider routes (for triggering sync) ----
	providerProcessSvc, err := provider_services.NewProviderProcessService()
	require.NoError(t, err)
	providerHandler := provider.NewProviderHandler(providerProcessSvc)
	pg := e.Group("/provider")
	pg.POST("/process", providerHandler.ProcessProviders)
	pg.GET("/process/status/:processID", providerHandler.GetProcessStatus)
	pg.GET("/processes", providerHandler.ListProcesses)

	// ---- Health ----
	health.MapHealth(e, config.GetConfig().Server)

	// ---- V2 query routes (check, hit, bulk) ----
	checker := v2.NewBloomAdapter(bloomMgr)
	database, err := db.GetDB()
	require.NoError(t, err)
	repo := db.NewEntryRepository(database)
	scorer := query.NewScorer(nil)
	v2QuerySvc := query.NewQueryService(checker, repo, scorer)
	v2Handler := v2.NewQueryHandlerWithDeps(v2QuerySvc)
	v2g := e.Group("/api/v1")
	v2g.GET("/check", v2Handler.Check)
	v2g.GET("/hit", v2Handler.Hit)
	v2g.POST("/bulk-check", v2Handler.BulkCheck)
	v2g.POST("/bulk-hit", v2Handler.BulkHit)

	return httptest.NewServer(e), bloomMgr
}

// ============================================================================
// Bloom Populate
// ============================================================================

// populateBloomFromDB reads all entries from the database and populates
// the BloomManager via PopulateEntry. This mirrors what the production
// bloom sync pipeline does after provider processing.
func populateBloomFromDB(t *testing.T, bm *bloom.BloomManager) {
	database, err := db.GetDB()
	require.NoError(t, err)

	rows, err := database.Query(`
		SELECT source_url, source, domain, host, path, raw_query
		FROM entries
		WHERE deleted_at IS NULL
	`)
	require.NoError(t, err)
	defer rows.Close()

	addCount := 0
	skipCount := 0

	for rows.Next() {
		var sourceURL, source, domain, host, pg, rawQuery string
		err := rows.Scan(&sourceURL, &source, &domain, &host, &pg, &rawQuery)
		require.NoError(t, err)

		// Use the provider name as source_id for bloom targeting
		sourceID := source

		// Parse the URL to get structured keys for bloom
		keys, err := bloom.ParseURL(sourceURL)
		if err != nil {
			skipCount++
			continue
		}

		// PopulateEntry determines bloom target internally
		bm.PopulateEntry(sourceID, keys)
		addCount++

		if addCount%50000 == 0 {
			fmt.Printf("  Bloom populate: %d entries processed...\n", addCount)
		}
	}

	require.NoError(t, rows.Err())
	fmt.Printf("  Bloom populated: %d entries added, %d skipped\n", addCount, skipCount)
}

// ============================================================================
// Helper Functions
// ============================================================================

func triggerProviderSync(t *testing.T, server *httptest.Server) string {
	payload := `{"providers_to_process":["OISD_BIG","URLHAUS","OPENPHISH"],"providers_to_remove":[]}`
	resp, err := http.Post(
		server.URL+"/provider/process",
		"application/json",
		strings.NewReader(payload),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "Failed to trigger provider sync")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	t.Logf("Provider sync response body: %s", string(body))

	var wrapper struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	err = json.Unmarshal(body, &wrapper)
	require.NoError(t, err)
	require.True(t, wrapper.Success, "Provider sync should succeed")

	processID, ok := wrapper.Data["process_id"].(string)
	require.True(t, ok, "process_id not found in response")
	require.NotEmpty(t, processID)

	return processID
}

func waitForProcessComplete(t *testing.T, server *httptest.Server, processID string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(server.URL + "/provider/process/status/" + processID)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)

		var statusWrapper struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		err = json.Unmarshal(body, &statusWrapper)
		require.NoError(t, err)

		s, _ := statusWrapper.Data["status"].(string)
		if s == "completed" {
			return
		}
		if s == "failed" {
			errMsg, _ := statusWrapper.Data["error"].(string)
			t.Fatalf("Provider process failed: %s", errMsg)
		}

		time.Sleep(1 * time.Second)
	}
	t.Fatalf("Provider process timed out after %v", timeout)
}

func loadE2ETestData(t *testing.T) E2ETestData {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	dir := filepath.Dir(file)
	dataPath := filepath.Join(dir, "testdata", "e2e_urls.json")

	data, err := os.ReadFile(dataPath)
	require.NoError(t, err)

	var testData E2ETestData
	err = json.Unmarshal(data, &testData)
	require.NoError(t, err)

	return testData
}
