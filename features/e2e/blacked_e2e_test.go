//go:build e2e
// +build e2e

package e2e

import (
	"blacked/features/cache"
	"blacked/features/providers"
	provider_services "blacked/features/providers/services"
	"blacked/features/web/handlers/health"
	"blacked/features/web/handlers/provider"
	"blacked/features/web/handlers/response"
	"blacked/features/web/middlewares"
	v2 "blacked/features/web/handlers/v2"
	"blacked/features/bloom"
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

type TestURL struct {
	URL            string `json:"url"`
	Source         string `json:"source,omitempty"`
	Description    string `json:"description"`
	ExpectedExists bool   `json:"expected_exists"`
	ExpectedStatus int    `json:"expected_status,omitempty"`
}

type TestData struct {
	MaliciousURLs []TestURL `json:"malicious_urls"`
	CleanURLs     []TestURL `json:"clean_urls"`
	EdgeCases     []TestURL `json:"edge_cases"`
}

// ============================================================================
// Test Suite
// ============================================================================

func TestBlacked_E2E(t *testing.T) {
	fmt.Println("\n========================================")
	fmt.Println("  BLACKED E2E Test Suite")
	fmt.Println("========================================")

	// --- Step 1: Initialize test infrastructure ---
	fmt.Println("=== Step 1: Initializing test server ===")
	server := setupTestServer(t)
	defer server.Close()
	fmt.Printf("Test server started at %s\n\n", server.URL)

	// --- Step 2: Trigger provider processing via HTTP API ---
	fmt.Println("=== Step 2: Triggering provider sync ===")
	processID := triggerProviderSync(t, server)
	fmt.Printf("Process started: %s\n", processID)

	// --- Step 3: Wait for provider processing to complete ---
	fmt.Println("=== Step 3: Waiting for provider sync to complete ===")
	waitForProcessComplete(t, server, processID, 120*time.Second)
	fmt.Println("Provider sync completed successfully")

	// --- Step 4: Load test URLs ---
	testData := loadTestData(t)

	// --- Step 5: Run E2E tests ---
	passed, failed := 0, 0

	// Health check
	fmt.Println("=== Step 5: Health check ===")
	if testHealthCheck(t, server) {
		passed++
	} else {
		failed++
	}

	// Query known malicious URLs
	fmt.Println("\n=== Step 6: Known malicious URL queries ===")
	for _, u := range testData.MaliciousURLs {
		if testQueryExists(t, server, u.URL, true, u.Description) {
			passed++
		} else {
			failed++
		}
	}

	// Query clean URLs
	fmt.Println("\n=== Step 7: Clean URL queries ===")
	for _, u := range testData.CleanURLs {
		if testQueryExists(t, server, u.URL, false, u.Description) {
			passed++
		} else {
			failed++
		}
	}

	// Edge cases
	fmt.Println("\n=== Step 8: Edge case queries ===")
	for _, u := range testData.EdgeCases {
		if testEdgeCase(t, server, u) {
			passed++
		} else {
			failed++
		}
	}

	// --- Step 9: Trigger second sync for stability test ---
	fmt.Println("\n=== Step 9: Re-sync stability test ===")
	processID2 := triggerProviderSync(t, server)
	waitForProcessComplete(t, server, processID2, 120*time.Second)

	// Re-run quick health + one malicious + one clean
	if testHealthCheck(t, server) {
		passed++
	} else {
		failed++
	}
	if len(testData.MaliciousURLs) > 0 {
		if testQueryExists(t, server, testData.MaliciousURLs[0].URL, true, "Re-sync stability: known malicious") {
			passed++
		} else {
			failed++
		}
	}
	if len(testData.CleanURLs) > 0 {
		if testQueryExists(t, server, testData.CleanURLs[0].URL, false, "Re-sync stability: clean URL") {
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
// Helpers
// ============================================================================

func setupTestServer(t *testing.T) *httptest.Server {
	logger.InitializeLogger()

	// Point config to a test config
	testConfigDir := t.TempDir()
	configContent := `
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
enabled_providers = ["oisd", "urlhaus", "openphish"]

[log]
level = "warn"
`
	configPath := filepath.Join(testConfigDir, "test.env.toml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	configFile := filepath.Join(testConfigDir, "test.env.toml")
	os.Setenv("CONFIG_FILE", configFile)
	defer os.Unsetenv("CONFIG_FILE")

	// Initialize config
	err = config.InitConfig()
	require.NoError(t, err)

	// Initialize DB with temp path
	os.Setenv("DB_PATH", filepath.Join(testConfigDir, "blacked-e2e.db"))
	defer os.Unsetenv("DB_PATH")

	db.InitializeDB()
	writeDB, err := db.GetWriteDB()
	require.NoError(t, err)

	err = db.FullMigration(writeDB)
	require.NoError(t, err)

	// Initialize providers
	provList, err := providers.InitProviders()
	require.NoError(t, err)

	// Build Echo app manually
	e := echo.New()
	e.Server.Addr = ":0"

	// Configure validator (used by handlers)
	middlewares.ConfigureValidator(e)

	// Configure custom error handling for 404/400 — standardize to response handler format
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		he := err.(*echo.HTTPError)
		response.ErrorWithDetails(c, he.Code, http.StatusText(he.Code), fmt.Sprintf("%v", he.Message))
	}

	// ---- Register routes manually ----

	// Provider routes (for triggering sync)
	providerProcessSvc, err := provider_services.NewProviderProcessService()
	require.NoError(t, err)
	providerHandler := provider.NewProviderHandler(providerProcessSvc)
	pg := e.Group("/provider")
	pg.POST("/process", providerHandler.ProcessProviders)
	pg.GET("/process/status/:processID", providerHandler.GetProcessStatus)
	pg.GET("/processes", providerHandler.ListProcesses)

	// Health
	health.MapHealth(e, config.GetConfig().Server)

	// V2 query routes (new API: check, hit, bulk)
	mgr := bloom.NewBloomManager(1000)
	checker := v2.NewBloomAdapter(mgr)
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

	// Initialize cache AFTER providers + DB are ready
	ctx := context.Background()
	// Run provider processing to populate cache first
	providerNames := provList.GetNames()
	processErr := provList.Processor(providerNames, nil)
	if processErr != nil {
		log.Warn().Err(processErr).Msg("Provider processing returned error (may be partial)")
	}

	// Now init cache and bloom
	err = cache.InitializeCache(ctx)
	require.NoError(t, err)

	return httptest.NewServer(e)
}

func triggerProviderSync(t *testing.T, server *httptest.Server) string {
	payload := `{"providers_to_process":["oisd","urlhaus","openphish"],"providers_to_remove":[]}`
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

	var result map[string]any
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)

	processID, ok := result["process_id"].(string)
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

		var status map[string]any
		err = json.Unmarshal(body, &status)
		require.NoError(t, err)

		s, _ := status["status"].(string)
		if s == "completed" {
			return
		}
		if s == "failed" {
			errMsg, _ := status["error"].(string)
			t.Fatalf("Provider process failed: %s", errMsg)
		}

		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Provider process timed out after %v", timeout)
}

func loadTestData(t *testing.T) TestData {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	dir := filepath.Dir(file)
	dataPath := filepath.Join(dir, "testdata", "e2e_urls.json")

	data, err := os.ReadFile(dataPath)
	require.NoError(t, err)

	var testData TestData
	err = json.Unmarshal(data, &testData)
	require.NoError(t, err)

	return testData
}

func testHealthCheck(t *testing.T, server *httptest.Server) bool {
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
	err = json.Unmarshal(body, &result)
	if !assert.NoError(t, err) {
		return false
	}
	assert.Equal(t, "ok", result["status"], "Health status should be ok")
	return true
}

func testQueryExists(t *testing.T, server *httptest.Server, url string, expectedExists bool, desc string) bool {
	apiURL := server.URL + "/api/v1/hit?url=" + url
	resp, err := http.Get(apiURL)
	if !assert.NoError(t, err, "Request failed for %s", desc) {
		return false
	}
	defer resp.Body.Close()

	if !assert.Equal(t, http.StatusOK, resp.StatusCode,
		"Expected 200 for %s (%s)", desc, url) {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if !assert.NoError(t, err) {
		return false
	}

	var result query.QueryResponse
	err = json.Unmarshal(body, &result)
	if !assert.NoError(t, err) {
		return false
	}

	assert.Equal(t, expectedExists, result.Blocked,
		"Blocked mismatch for %s (url=%s): got %v, want %v",
		desc, url, result.Blocked, expectedExists)
	return result.Blocked == expectedExists
}

func testEdgeCase(t *testing.T, server *httptest.Server, u TestURL) bool {
	apiURL := server.URL + "/api/v1/hit?url=" + u.URL
	if u.ExpectedStatus != 0 {
		resp, err := http.Get(apiURL)
		if !assert.NoError(t, err, "Edge case request failed: %s", u.Description) {
			return false
		}
		defer resp.Body.Close()
		return assert.Equal(t, u.ExpectedStatus, resp.StatusCode,
			"Status code mismatch for edge case: %s", u.Description)
	}

	resp, err := http.Get(apiURL)
	if !assert.NoError(t, err, "Edge case request failed: %s", u.Description) {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Str("url", u.URL).Msg("Edge case returned non-200")
		return true // edge cases are lenient
	}

	body, err := io.ReadAll(resp.Body)
	if !assert.NoError(t, err) {
		return false
	}

	var result query.QueryResponse
	err = json.Unmarshal(body, &result)
	if !assert.NoError(t, err, "Edge case response is not valid JSON: %s", u.Description) {
		return false
	}
	return true
}
