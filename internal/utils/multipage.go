package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

// PageData represents a single page of provider data that has been persisted to disk.
type PageData struct {
	PageNumber    int
	FilePath      string
	Indicators    int // parsed indicator count (may be 0 if not yet parsed)
	FetchedAt     time.Time
	NextPageURL   string // empty string if no more pages
}

// PageMetadata tracks all pages for a provider's multi-page fetch session.
type PageMetadata struct {
	Provider      string    `json:"provider"`
	FetchStarted  time.Time `json:"fetch_started"`
	FetchCompleted time.Time `json:"fetch_completed,omitempty"`
	TotalPages    int       `json:"total_pages"`
	TotalIndicators int    `json:"total_indicators"`
	Pages         []PageInfo `json:"pages"`
	Status        string    `json:"status"` // "in_progress", "completed", "failed"
	Error         string    `json:"error,omitempty"`
}

// PageInfo describes a single page file on disk.
type PageInfo struct {
	File       string    `json:"file"`
	FetchedAt  time.Time `json:"fetched_at"`
	Indicators int       `json:"indicators"`
}

// --- Per-page persistence functions ---

// GetProviderDataDir returns the per-provider data directory path.
// Creates it if it doesn't exist.
func GetProviderDataDir(storePath, providerName string) (string, error) {
	dir := filepath.Join(storePath, providerName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create provider data dir %s: %w", dir, err)
	}
	return dir, nil
}

// SavePageData saves a single page's raw response to disk and updates the meta file.
// pageNum is 1-indexed. indicatorCount may be 0 if not yet parsed.
func SavePageData(storePath, providerName string, pageNum int, data []byte, indicatorCount int, fetchedAt time.Time) (string, error) {
	dir, err := GetProviderDataDir(storePath, providerName)
	if err != nil {
		return "", err
	}

	// Save page file as page_NNN.dat
	pageFilename := fmt.Sprintf("page_%03d.dat", pageNum)
	pagePath := filepath.Join(dir, pageFilename)

	if err := os.WriteFile(pagePath, data, 0644); err != nil {
		return "", fmt.Errorf("write page file %s: %w", pagePath, err)
	}

	// Load or create meta file
	metaPath := filepath.Join(dir, providerName+".meta.json")
	meta, err := loadPageMetadata(metaPath)
	if err != nil || meta == nil {
		// Create fresh meta if doesn't exist or failed to load
		meta = &PageMetadata{
			Provider:     providerName,
			FetchStarted: fetchedAt,
			Status:       "in_progress",
		}
	}

	// Update meta — ensure pages array is large enough
	for len(meta.Pages) < pageNum {
		meta.Pages = append(meta.Pages, PageInfo{})
	}
	meta.Pages[pageNum-1] = PageInfo{
		File:       pageFilename,
		FetchedAt:  fetchedAt,
		Indicators: indicatorCount,
	}
	if pageNum > meta.TotalPages {
		meta.TotalPages = pageNum
	}

	if err := savePageMetadata(metaPath, meta); err != nil {
		return pagePath, err
	}

	log.Debug().
		Str("provider", providerName).
		Int("page", pageNum).
		Str("file", pageFilename).
		Int("bytes", len(data)).
		Msg("Page saved to disk")

	return pagePath, nil
}

// ResumePageNumber scans the provider's data directory and returns the highest
// completed page number + 1 (the page to resume from). Returns 1 if no pages exist.
func ResumePageNumber(storePath, providerName string) (int, error) {
	dir := filepath.Join(storePath, providerName)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 1, fmt.Errorf("read provider dir %s: %w", dir, err)
	}

	maxPage := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) == 11 && name[:5] == "page_" && name[len(name)-4:] == ".dat" {
			n, err := strconv.Atoi(name[5 : len(name)-4])
			if err == nil && n > maxPage {
				maxPage = n
			}
		}
	}

	return maxPage + 1, nil
}

// LoadAllPages returns all saved page files for a provider, sorted by page number.
func LoadAllPages(storePath, providerName string) ([]string, error) {
	dir := filepath.Join(storePath, providerName)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read provider dir %s: %w", dir, err)
	}

	var pageFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) == 11 && name[:5] == "page_" && name[len(name)-4:] == ".dat" {
			pageFiles = append(pageFiles, filepath.Join(dir, name))
		}
	}

	// Sort by page number
	sort.Slice(pageFiles, func(i, j int) bool {
		pi, _ := strconv.Atoi(pageFiles[i][len(filepath.Join(dir, "page_")):len(pageFiles[i])-4])
		pj, _ := strconv.Atoi(pageFiles[j][len(filepath.Join(dir, "page_")):len(pageFiles[j])-4])
		return pi < pj
	})

	return pageFiles, nil
}

// GetPageMetadata returns the current metadata for a provider.
func GetPageMetadata(storePath, providerName string) (*PageMetadata, error) {
	metaPath := filepath.Join(storePath, providerName, providerName+".meta.json")
	return loadPageMetadata(metaPath)
}

// loadPageMetadata loads PageMetadata from a JSON file.
func loadPageMetadata(metaPath string) (*PageMetadata, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read meta file %s: %w", metaPath, err)
	}
	var meta PageMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("decode meta %s: %w", metaPath, err)
	}
	return &meta, nil
}

// savePageMetadata writes PageMetadata to a JSON file.
func savePageMetadata(metaPath string, meta *PageMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write meta file %s: %w", metaPath, err)
	}
	return nil
}

// MarkProviderFetchComplete updates the meta file to mark the fetch as complete.
func MarkProviderFetchComplete(storePath, providerName string, totalIndicators int) error {
	metaPath := filepath.Join(storePath, providerName, providerName+".meta.json")
	meta, err := loadPageMetadata(metaPath)
	if err != nil {
		return err
	}
	meta.FetchCompleted = time.Now()
	meta.Status = "completed"
	meta.TotalIndicators = totalIndicators
	return savePageMetadata(metaPath, meta)
}