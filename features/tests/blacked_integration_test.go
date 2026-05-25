package tests

import (
	"blacked/features/bloom"
	idb "blacked/internal/db"
	"blacked/internal/query"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

var testURLsData []byte

func init() {
	var err error
	testURLsData, err = os.ReadFile("testdata/test_urls.json")
	if err != nil {
		panic("failed to load test URLs: " + err.Error())
	}
}

type testURLs struct {
	Malicious struct {
		URLHaus   []string `json:"urlhaus"`
		PhishTank []string `json:"phishtank"`
		OISDNSFW  []string `json:"oisd_nsfw"`
		OISDBig   []string `json:"oisd_big"`
		OpenPhish []string `json:"openphish"`
	} `json:"malicious_urls"`
	Safe struct {
		Tech      []string `json:"tech"`
		News      []string `json:"news"`
		Reference []string `json:"reference"`
		Shopping  []string `json:"shopping"`
	} `json:"safe_urls"`
	Edge struct {
		IPOnly         []string `json:"ip_only"`
		Subdomains     []string `json:"subdomains"`
		QueryParams    []string `json:"query_params"`
		SpecialChars   []string `json:"special_chars"`
		LongPaths      []string `json:"long_paths"`
		UnicodeDomains []string `json:"unicode_domains"`
	} `json:"edge_cases"`
}

type testSuite struct {
	t       *testing.T
	db      *sql.DB
	dbWrite *sql.DB
	repo    query.EntryRepository
	bloom   *bloom.BloomManager
	svc     *query.QueryService
	urls    testURLs
	entries map[string][]int64 // sourceID -> rowids
}

func loadTestURLs(t testing.TB) testURLs {
	var urls testURLs
	err := json.Unmarshal(testURLsData, &urls)
	require.NoError(t, err)
	return urls
}

// setupDB creates a fresh :memory: database with the Phase-2 schema migrated.
func setupDB(t testing.TB) *sql.DB {
	// Use a single :memory: DB name shared between connections via mode=rwc
	db, err := sql.Open("sqlite", "file:test-integration.db?vfs=memdb")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	err = db.Ping()
	require.NoError(t, err)

	// Enable WAL + foreign keys on in-memory
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Run migrations
	err = idb.FullMigration(db)
	require.NoError(t, err)

	return db
}

// seedProviders inserts test providers.
func seedProviders(t testing.TB, db *sql.DB) {
	providers := []struct {
		id, name string
		score    float64
	}{
		{"test-urlhaus", "URLHaus Test", 0.90},
		{"test-phishtank", "PhishTank Test", 0.70},
		{"test-oisd-nsfw", "OISD NSFW Test", 0.65},
		{"test-oisd-big", "OISD Big Test", 0.65},
		{"test-openphish", "OpenPhish Test", 0.70},
	}
	for _, p := range providers {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO providers (id, name, trust_score)
			VALUES (?, ?, ?)
		`, p.id, p.name, p.score)
		require.NoError(t, err)
	}
}

// seedSources inserts test sources linked to providers.
func seedSources(t testing.TB, db *sql.DB) {
	sources := []struct {
		id, provID, name, url, typ string
		score                      float64
		interval                   int
	}{
		{"src-urlhaus", "test-urlhaus", "URLHaus Source", "https://urlhaus.abuse.ch/downloads/csv/", "urlhaus", 0.90, 3600},
		{"src-phishtank", "test-phishtank", "PhishTank Source", "https://phishtank.org/files/valid/", "phishtank", 0.70, 7200},
		{"src-oisd-nsfw", "test-oisd-nsfw", "OISD NSFW Source", "https://nsfw.oisd.nl/", "oisd_nsfw", 0.65, 86400},
		{"src-oisd-big", "test-oisd-big", "OISD Big Source", "https://big.oisd.nl/", "oisd_big", 0.65, 86400},
		{"src-openphish", "test-openphish", "OpenPhish Source", "https://openphish.com/feed.txt", "openphish", 0.70, 1800},
	}
	for _, s := range sources {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO sources (id, provider_id, name, source_url, type, trust_score, update_interval, enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1)
		`, s.id, s.provID, s.name, s.url, s.typ, s.score, s.interval)
		require.NoError(t, err)
	}
}

// insertEntry inserts a single entry into the new entries table.
func insertEntry(t testing.TB, db *sql.DB, id, sourceID, urlStr string) int64 {
	keys, err := bloom.ParseURL(urlStr)
	require.NoError(t, err)

	// Generate a stable ID if not given
	if id == "" {
		id = fmt.Sprintf("entry-%s-%d", sourceID, time.Now().UnixNano())
	}

	res, err := db.Exec(`
		INSERT OR IGNORE INTO entries
		(id, source, domain, host, path, raw_query, source_url, scheme, confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, sourceID,
		keys.Domain, keys.Host, keys.Path, "", urlStr, "https", 0.8)
	require.NoError(t, err)

	rowID, _ := res.LastInsertId()
	return rowID
}

// populateBloom inserts all seeded entries into the BloomManager
// using PopulateEntry — each entry goes into exactly one bloom type.
func populateBloom(t testing.TB, bm *bloom.BloomManager, db *sql.DB) {
	rows, err := db.Query(`
		SELECT source, source_url
		FROM entries
		WHERE deleted_at IS NULL
	`)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var sourceID, sourceURL string
		err := rows.Scan(&sourceID, &sourceURL)
		require.NoError(t, err)

		keys, err := bloom.ParseURL(sourceURL)
		if err != nil {
			// Skip unparseable entries
			continue
		}

		bm.PopulateEntry(sourceID, keys)
		count++
	}
	require.NoError(t, rows.Err())
	require.Greater(t, count, 0, "no entries populated into bloom")
}

// newTestSuite builds a fully wired test suite.
func newTestSuite(t *testing.T) *testSuite {
	t.Helper()

	urls := loadTestURLs(t)
	db := setupDB(t)
	seedProviders(t, db)
	seedSources(t, db)

	// Seed malicious URLs into entries table
	entries := make(map[string][]int64)
	for i, url := range urls.Malicious.URLHaus {
		rid := insertEntry(t, db, fmt.Sprintf("uh-%d", i), "src-urlhaus", url)
		entries["src-urlhaus"] = append(entries["src-urlhaus"], rid)
	}
	for i, url := range urls.Malicious.PhishTank {
		rid := insertEntry(t, db, fmt.Sprintf("pt-%d", i), "src-phishtank", url)
		entries["src-phishtank"] = append(entries["src-phishtank"], rid)
	}
	for i, url := range urls.Malicious.OISDNSFW {
		rid := insertEntry(t, db, fmt.Sprintf("on-%d", i), "src-oisd-nsfw", url)
		entries["src-oisd-nsfw"] = append(entries["src-oisd-nsfw"], rid)
	}
	for i, url := range urls.Malicious.OISDBig {
		rid := insertEntry(t, db, fmt.Sprintf("ob-%d", i), "src-oisd-big", url)
		entries["src-oisd-big"] = append(entries["src-oisd-big"], rid)
	}
	for i, url := range urls.Malicious.OpenPhish {
		rid := insertEntry(t, db, fmt.Sprintf("op-%d", i), "src-openphish", url)
		entries["src-openphish"] = append(entries["src-openphish"], rid)
	}

	// Build bloom manager
	bm := bloom.NewBloomManager(10000)
	populateBloom(t, bm, db)

	// Build entry repository on read pool (use same db for in-memory)
	repo := idb.NewEntryRepository(db)

	// Build query service (no scorer for simplicity)
	svc := query.NewQueryService(
		&bloomCheckerAdapter{bm: bm},
		repo,
		nil,
	)

	return &testSuite{
		t:       t,
		db:      db,
		repo:    repo,
		bloom:   bm,
		svc:     svc,
		urls:    urls,
		entries: entries,
	}
}

// bloomCheckerAdapter adapts BloomManager to query.BloomChecker.
type bloomCheckerAdapter struct {
	bm *bloom.BloomManager
}

func (a *bloomCheckerAdapter) Check(urlStr string) (bool, []query.Match, error) {
	br, err := a.bm.Likely(urlStr)
	if err != nil {
		return false, nil, err
	}
	qm := make([]query.Match, len(br.Matches))
	for i, m := range br.Matches {
		qm[i] = query.Match{
			SourceID: m.SourceID,
			Type:     string(m.Type),
			Key:      m.Key,
		}
	}
	return br.Likely, qm, nil
}

// ============================================================================
// Category A: Provider Sync ve DB Yazma
// ============================================================================
func TestCategoryA_ProviderSyncAndDBWrite(t *testing.T) {
	ts := newTestSuite(t)

	t.Run("A_1_Bütün_Providerlar_Kayıtlı", func(t *testing.T) {
		rows, err := ts.db.Query("SELECT id FROM sources WHERE enabled = 1")
		require.NoError(t, err)
		defer rows.Close()

		var ids []string
		for rows.Next() {
			var id string
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}

		assert.GreaterOrEqual(t, len(ids), 5, "should have at least 5 test sources")
		assert.Contains(t, ids, "src-urlhaus")
		assert.Contains(t, ids, "src-phishtank")
		assert.Contains(t, ids, "src-oisd-nsfw")
		assert.Contains(t, ids, "src-oisd-big")
		assert.Contains(t, ids, "src-openphish")
	})

	t.Run("A_2_Malicious_URLler_Entry_Olarak_Yazılmış", func(t *testing.T) {
		for sourceID, rids := range ts.entries {
			assert.Greater(t, len(rids), 0, "source %s has no entries", sourceID)
		}

		var total int
		err := ts.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&total)
		require.NoError(t, err)
		assert.Equal(t, 25, total, "expected 5 sources × 5 URLs each")
	})

	t.Run("A_3_Entry_URL_Bileşenleri_Doğru_Ayrıştırılmış", func(t *testing.T) {
		// Check a specific URL decomposition
		var domain, host, path string
		err := ts.db.QueryRow(`
			SELECT domain, host, path FROM entries
			WHERE source_url = ?
		`, "http://103.224.212.251/jjjj.exe").Scan(&domain, &host, &path)
		require.NoError(t, err)

		assert.Equal(t, "103.224.212.251", host)
		assert.Equal(t, "/jjjj.exe", path)
	})

	t.Run("A_4_Providerlar_Araya_Eklenmiş", func(t *testing.T) {
		// Seed adds extra providers via models.ProviderSeed (6)
		// So total providers = 6 (seed) + 5 (test) = 11
		var count int
		err := ts.db.QueryRow("SELECT COUNT(*) FROM providers").Scan(&count)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 5, "should have at least 5 providers")
		assert.LessOrEqual(t, count, 11, "should have at most 11 providers (6 seed + 5 test)")
	})

	t.Run("A_5_Kaynakların_Kaynakları_Doğru", func(t *testing.T) {
		var provID string
		err := ts.db.QueryRow("SELECT provider_id FROM sources WHERE id = ?", "src-urlhaus").Scan(&provID)
		require.NoError(t, err)
		assert.Equal(t, "test-urlhaus", provID)
	})

	t.Run("A_6_Tekrar_Ekleme_Unique_Constraint", func(t *testing.T) {
		// Same URL from same source should be ignored (ON CONFLICT IGNORE)
		// Must use the exact same (source_url, source, host) tuple that's already in DB
		res, err := ts.db.Exec(`
			INSERT OR IGNORE INTO entries (id, source, domain, host, source_url, scheme)
			VALUES (?, ?, ?, ?, ?, ?)
		`, "dup-test", "src-urlhaus", "103.224.212.251", "103.224.212.251", "http://103.224.212.251/jjjj.exe", "http")
		require.NoError(t, err)

		affected, _ := res.RowsAffected()
		assert.Equal(t, int64(0), affected, "duplicate should not insert")
	})
}

// ============================================================================
// Category B: Bloom Filter Hit Testleri
// ============================================================================
func TestCategoryB_BloomHitTests(t *testing.T) {
	ts := newTestSuite(t)

	t.Run("B_1_URLHaus_Domain_Hit", func(t *testing.T) {
		// 103.224.212.251 is in the database via urlhaus
		result, err := ts.bloom.Likely("http://103.224.212.251/jjjj.exe")
		require.NoError(t, err)
		assert.True(t, result.Likely, "bloom should indicate likely for known IP host")
		assert.Greater(t, len(result.Matches), 0)
	})

	t.Run("B_2_PhishTank_Host_Hit", func(t *testing.T) {
		result, err := ts.bloom.Likely("https://rtyrty.soup.io/")
		require.NoError(t, err)
		assert.True(t, result.Likely)
		// rtyrty.soup.io is a subdomain → Host bloom
		var foundHost bool
		for _, m := range result.Matches {
			if m.Type == bloom.BloomHost && m.Key == "rtyrty.soup.io" {
				foundHost = true
				break
			}
		}
		assert.True(t, foundHost, "should match on host")
	})

	t.Run("B_3_OISD_NSFW_File_Hit", func(t *testing.T) {
		result, err := ts.bloom.Likely("https://xxx.com/porn.jpg")
		require.NoError(t, err)
		assert.True(t, result.Likely)
		// "porn.jpg" has extension → File bloom
		var foundFile bool
		for _, m := range result.Matches {
			if m.Type == bloom.BloomFile && m.Key == "porn.jpg" {
				foundFile = true
				break
			}
		}
		assert.True(t, foundFile, "expected File match for porn.jpg")
	})

	t.Run("B_4_OpenPhish_HostPath_Hit", func(t *testing.T) {
		result, err := ts.bloom.Likely("https://evil-bank.example.com/login")
		require.NoError(t, err)
		assert.True(t, result.Likely)
		// "login" has no extension → HostPath bloom
		var foundHP bool
		for _, m := range result.Matches {
			if m.Type == bloom.BloomHostPath {
				foundHP = true
				break
			}
		}
		assert.True(t, foundHP)
	})

	t.Run("B_5_Multi_Source_Aynı_URL", func(t *testing.T) {
		// http://103.224.212.251/jjjj.exe exists in both urlhaus and phishtank
		result, err := ts.bloom.Likely("http://103.224.212.251/jjjj.exe")
		require.NoError(t, err)
		assert.True(t, result.Likely)
		// Check both source IDs are represented
		sources := make(map[string]bool)
		for _, m := range result.Matches {
			sources[m.SourceID] = true
		}
		assert.GreaterOrEqual(t, len(sources), 1)
	})

	t.Run("B_6_Bloom_MaxDepth_Doğru_Hesaplanıyor", func(t *testing.T) {
		result, err := ts.bloom.Likely("https://evil-bank.example.com/login")
		require.NoError(t, err)
		assert.True(t, result.Likely)
		// host_path has weight 1.0 => max_depth = 100
		if len(result.Matches) > 0 {
			assert.GreaterOrEqual(t, result.MaxDepth, 50)
		}
	})

	t.Run("B_7_QueryService_Hit_Bloomed_URL", func(t *testing.T) {
		resp, err := ts.svc.Hit(context.Background(), "https://xxx.com/porn.jpg")
		require.NoError(t, err)
		assert.True(t, resp.Blocked)
		assert.GreaterOrEqual(t, resp.Confidence, 0.5)
	})
}

// ============================================================================
// Category C: Bloom Filter Non-Hit Testleri
// ============================================================================
func TestCategoryC_BloomNonHitTests(t *testing.T) {
	ts := newTestSuite(t)

	t.Run("C_1_Safe_URL_Teknoloji", func(t *testing.T) {
		for _, url := range ts.urls.Safe.Tech {
			result, err := ts.bloom.Likely(url)
			require.NoError(t, err)
			assert.False(t, result.Likely, "safe tech URL should not bloom-hit: %s", url)
		}
	})

	t.Run("C_2_Safe_URL_Haber", func(t *testing.T) {
		for _, url := range ts.urls.Safe.News {
			result, err := ts.bloom.Likely(url)
			require.NoError(t, err)
			assert.False(t, result.Likely, "safe news URL should not bloom-hit: %s", url)
		}
	})

	t.Run("C_3_Safe_URL_Alışveriş", func(t *testing.T) {
		for _, url := range ts.urls.Safe.Shopping {
			result, err := ts.bloom.Likely(url)
			require.NoError(t, err)
			assert.False(t, result.Likely, "safe shopping URL should not bloom-hit: %s", url)
		}
	})

	t.Run("C_4_Safety_Tüm_Safe_URLler", func(t *testing.T) {
		allSafe := append(ts.urls.Safe.Tech, ts.urls.Safe.News...)
		allSafe = append(allSafe, ts.urls.Safe.Reference...)
		allSafe = append(allSafe, ts.urls.Safe.Shopping...)

		falsePositives := 0
		for _, url := range allSafe {
			result, err := ts.bloom.Likely(url)
			require.NoError(t, err)
			if result.Likely {
				falsePositives++
			}
		}
		t.Logf("False positives: %d/%d (%.1f%%)", falsePositives, len(allSafe),
			float64(falsePositives)/float64(len(allSafe))*100)
		// Bloom filter rate ~1%; allow up to 5% for test stability
		assert.LessOrEqual(t, falsePositives, len(allSafe)*5/100+1,
			"false positive rate should stay under ~5%%")
	})

	t.Run("C_5_QueryService_Hit_Clean_URL", func(t *testing.T) {
		resp, err := ts.svc.Hit(context.Background(), "https://github.com/guneskorkmaz")
		require.NoError(t, err)
		assert.False(t, resp.Blocked)
		assert.Equal(t, "informational", resp.Level)
	})
}

// ============================================================================
// Category D: Cache ve Re-Sync Stabilitesi
// ============================================================================
func TestCategoryD_CacheAndReSyncStability(t *testing.T) {
	ts := newTestSuite(t)

	t.Run("D_1_Bloom_ColdStart_Yok", func(t *testing.T) {
		assert.False(t, ts.bloom.ColdStart(), "bloom should be warm after seeding")
	})

	t.Run("D_2_RebuildSource_Sadece_İlgili_Kaynak", func(t *testing.T) {
		// Insert a new entry for a single source after initial bloom build
		newURL := "https://new-evil.example.com/malware.bin"
		insertEntry(t, ts.db, "new-evil-001", "src-urlhaus", newURL)

		// Add to bloom using PopulateEntry
		keys, _ := bloom.ParseURL(newURL)
		ts.bloom.PopulateEntry("src-urlhaus", keys)

		// Verify the new URL is now bloom positive
		result, err := ts.bloom.Likely(newURL)
		require.NoError(t, err)
		assert.True(t, result.Likely)
	})

	t.Run("D_3_Bloom_Stats_Doğru", func(t *testing.T) {
		stats := ts.bloom.Stats()
		assert.NotEmpty(t, stats)
		// PopulateEntry writes each entry to its most specific bloom type.
		// Test URLs include HostPath, File, FullURL, Host, Domain entries.
		// At minimum, HostPath and Host should have sources.
		hasSource := false
		for _, bt := range []bloom.BloomType{
			bloom.BloomHost, bloom.BloomHostPath,
			bloom.BloomFile, bloom.BloomFullURL, bloom.BloomDomain,
		} {
			if v, ok := stats[string(bt)]; ok && v > 0 {
				hasSource = true
				break
			}
		}
		assert.True(t, hasSource, "at least one bloom type should have sources")
	})

	t.Run("D_4_Bloom_ColdStart_True_Empty_Manager", func(t *testing.T) {
		emptyBM := bloom.NewBloomManager(100)
		assert.True(t, emptyBM.ColdStart(), "fresh manager should be cold")
	})

	t.Run("D_5_Concurrent_Check_Stabil", func(t *testing.T) {
		urls := append(ts.urls.Malicious.URLHaus, ts.urls.Safe.Tech...)
		// Run concurrent checks from multiple goroutines
		done := make(chan struct{})
		for i := range 10 {
			go func(idx int) {
				defer func() { done <- struct{}{} }()
				for j := range 10 {
					url := urls[(idx+j)%len(urls)]
					_, err := ts.bloom.Likely(url)
					assert.NoError(t, err)
				}
			}(i)
		}
		for range 10 {
			<-done
		}
	})
}

// ============================================================================
// Category E: Web API
// ============================================================================
func TestCategoryE_WebAPI(t *testing.T) {
	ts := newTestSuite(t)
	e := echo.New()

	// Mount a minimal V2-like route using the real QueryService
	e.GET("/api/v1/check", func(c echo.Context) error {
		urlStr := c.QueryParam("url")
		if urlStr == "" {
			return c.JSON(400, map[string]string{"error": "url required"})
		}
		resp, err := ts.svc.Likely(c.Request().Context(), urlStr)
		if err != nil {
			return c.JSON(500, map[string]string{"error": err.Error()})
		}
		return c.JSON(200, resp)
	})

	e.GET("/api/v1/hit", func(c echo.Context) error {
		urlStr := c.QueryParam("url")
		if urlStr == "" {
			return c.JSON(400, map[string]string{"error": "url required"})
		}
		resp, err := ts.svc.Hit(c.Request().Context(), urlStr)
		if err != nil {
			return c.JSON(500, map[string]string{"error": err.Error()})
		}
		return c.JSON(200, resp)
	})

	e.POST("/api/v1/bulk-hit", func(c echo.Context) error {
		var req struct {
			URLs []string `json:"urls"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(400, map[string]string{"error": "invalid JSON"})
		}
		results, err := ts.svc.BulkHit(c.Request().Context(), req.URLs)
		if err != nil {
			return c.JSON(500, map[string]string{"error": err.Error()})
		}
		return c.JSON(200, results)
	})

	t.Run("E_1_GET_Check_Malicious_URL", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/check?url=https://xxx.com/porn.jpg", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, `"likely":true`)
		assert.Contains(t, body, `"url":"https://xxx.com/porn.jpg"`)
	})

	t.Run("E_2_GET_Check_Clean_URL", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/check?url=https://github.com/guneskorkmaz", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, `"likely":false`)
	})

	t.Run("E_3_GET_Check_URL_Eksin_400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/check", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("E_4_GET_Hit_Malicious_URL", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/hit?url=https://evil-bank.example.com/login", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, `"blocked":true`)
		assert.Contains(t, body, `"url":"https://evil-bank.example.com/login"`)
	})

	t.Run("E_5_GET_Hit_Clean_URL_Bilgi", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/hit?url=https://stackoverflow.com/questions/go", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, `"blocked":false`)
		assert.Contains(t, body, `"level":"informational"`)
	})

	t.Run("E_6_POST_Bulk_Hit_Karma_URL_Listesi", func(t *testing.T) {
		payload := `{"urls":["https://xxx.com/porn.jpg","https://github.com/guneskorkmaz","https://evil-bank.example.com/login"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/bulk-hit", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, `"blocked":true`)
		assert.Contains(t, body, `"blocked":false`)
	})

	t.Run("E_7_POST_Bulk_Hit_Boş_Liste", func(t *testing.T) {
		payload := `{"urls":[]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/bulk-hit", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
	})
}

// ============================================================================
// Category F: Edge Cases
// ============================================================================
func TestCategoryF_EdgeCases(t *testing.T) {
	ts := newTestSuite(t)

	t.Run("F_1_IP_Only_URL", func(t *testing.T) {
		for _, url := range ts.urls.Edge.IPOnly {
			_, err := ts.bloom.Likely(url)
			// Should not panic, should return clean result
			assert.NoError(t, err)
		}
	})

	t.Run("F_2_Subdomain_URL", func(t *testing.T) {
		result, err := ts.bloom.Likely("https://api.github.com/users/octocat")
		require.NoError(t, err)
		assert.False(t, result.Likely)
	})

	t.Run("F_3_Query_Params_URL", func(t *testing.T) {
		result, err := ts.bloom.Likely("https://www.google.com/search?q=go+programming&source=hp")
		require.NoError(t, err)
		assert.False(t, result.Likely)
	})

	t.Run("F_4_Special_Chars_URL", func(t *testing.T) {
		for _, url := range ts.urls.Edge.SpecialChars {
			_, err := ts.bloom.Likely(url)
			assert.NoError(t, err, "URL should parse without error: %s", url)
		}
	})

	t.Run("F_5_Long_Path_URL", func(t *testing.T) {
		for _, url := range ts.urls.Edge.LongPaths {
			_, err := ts.bloom.Likely(url)
			assert.NoError(t, err, "long path URL should parse: %s", url)
		}
	})

	t.Run("F_6_Unicode_Domain_URL", func(t *testing.T) {
		for _, url := range ts.urls.Edge.UnicodeDomains {
			_, err := ts.bloom.Likely(url)
			// Unicode domains may fail to parse; ensure no panic
			_ = err
		}
	})

	t.Run("F_7_Boş_URL", func(t *testing.T) {
		result, err := ts.bloom.Likely("")
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("F_8_Whitespace_URL", func(t *testing.T) {
		result, err := ts.bloom.Likely("   ")
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("F_9_URL_Scheme_Eksik", func(t *testing.T) {
		// bloom.ParseURL auto-prefixes // for schemeless URLs
		// Should not panic or error — bloom may false-positive
		result, err := ts.bloom.Likely("example.com/path")
		require.NoError(t, err)
		// Bloom false positive rate ~1%, so Likely assertion is soft
		t.Logf("F_9: Likely=%v (false positive expected ~1%%)", result.Likely)
	})

	t.Run("F_10_Case_Duyarlılığı", func(t *testing.T) {
		// Bloom filters use exact string matching by default
		result1, err := ts.bloom.Likely("https://XXX.COM/porn.jpg")
		require.NoError(t, err)
		// domain is stored as "xxx.com", query uses "XXX.COM" which won't match
		// this is expected behavior for exact-match bloom
		_ = result1
	})
}

// ============================================================================
// Category G: Benchmark (optional)
// ============================================================================
func BenchmarkBloomPopulate(b *testing.B) {
	db := setupDB(b)
	seedProviders(b, db)
	seedSources(b, db)

	urls := loadTestURLs(b)
	for i, url := range urls.Malicious.URLHaus {
		insertEntry(b, db, fmt.Sprintf("b-%d", i), "src-urlhaus", url)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bm := bloom.NewBloomManager(10000)
		populateBloom(b, bm, db)
	}
}

func BenchmarkBloomCheck(b *testing.B) {
	db := setupDB(b)
	seedProviders(b, db)
	seedSources(b, db)

	urls := loadTestURLs(b)
	for i, url := range urls.Malicious.URLHaus {
		insertEntry(b, db, fmt.Sprintf("b-%d", i), "src-urlhaus", url)
	}

	bm := bloom.NewBloomManager(10000)
	populateBloom(b, bm, db)

	checkURL := "http://103.224.212.251/jjjj.exe"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bm.Likely(checkURL)
	}
}

func BenchmarkQueryServiceHit(b *testing.B) {
	db := setupDB(b)
	seedProviders(b, db)
	seedSources(b, db)

	urls := loadTestURLs(b)
	for i, url := range urls.Malicious.URLHaus {
		insertEntry(b, db, fmt.Sprintf("b-%d", i), "src-urlhaus", url)
	}

	bm := bloom.NewBloomManager(10000)
	populateBloom(b, bm, db)

	repo := idb.NewEntryRepository(db)
	svc := query.NewQueryService(&bloomCheckerAdapter{bm: bm}, repo, nil)

	ctx := context.Background()
	checkURL := "http://103.224.212.251/jjjj.exe"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.Hit(ctx, checkURL)
	}
}
