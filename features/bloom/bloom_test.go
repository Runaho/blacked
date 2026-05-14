package bloom

import (
	"testing"
)

func TestBloomSetBasicOperations(t *testing.T) {
	bs := NewBloomSet(BloomDomain, 10000)

	if bs.Type != BloomDomain {
		t.Fatalf("expected type to be %q, got %q", BloomDomain, bs.Type)
	}

	// Initially empty
	if bs.Test("malicious.com") {
		t.Fatal("expected no match for unknown key")
	}
	if bs.TestSource("source1", "malicious.com") {
		t.Fatal("expected no match for unknown source")
	}
	if bs.SourceCount() != 0 {
		t.Fatalf("expected source count 0, got %d", bs.SourceCount())
	}

	// Add a key
	bs.Add("source1", "malicious.com")

	// Global filter should now match
	if !bs.Test("malicious.com") {
		t.Fatal("expected global filter to match after add")
	}

	// Source filter should match
	if !bs.TestSource("source1", "malicious.com") {
		t.Fatal("expected source filter to match after add")
	}

	// Different source should not match
	if bs.TestSource("source2", "malicious.com") {
		t.Fatal("expected different source not to match")
	}

	// Should have 1 source
	if bs.SourceCount() != 1 {
		t.Fatalf("expected source count 1, got %d", bs.SourceCount())
	}

	// GetSourceIDs
	ids := bs.GetSourceIDs()
	if len(ids) != 1 || ids[0] != "source1" {
		t.Fatalf("expected source IDs [source1], got %v", ids)
	}

	// Add another key for same source
	bs.Add("source1", "evil.com")
	if !bs.Test("evil.com") {
		t.Fatal("expected global filter to match second key")
	}

	// Add key for another source
	bs.Add("source2", "phishing.com")
	if bs.TestSource("source1", "phishing.com") {
		t.Fatal("expected source1 not to match source2's key")
	}
	if !bs.TestSource("source2", "phishing.com") {
		t.Fatal("expected source2 to match its own key")
	}

	// TotalKeys should reflect capacity (non-zero)
	if bs.TotalKeys() == 0 {
		t.Fatal("expected TotalKeys > 0 after adds")
	}

	// Reset source1
	bs.ResetSource("source1")
	if bs.TestSource("source1", "malicious.com") {
		t.Fatal("expected source1 not to match after reset")
	}

	// Global filter may still match due to source2's entries
	// But source1-only keys should not match per-source
	if bs.TestSource("source1", "evil.com") {
		t.Fatal("expected reset source1 not to match its old key")
	}

	// source2 should still work
	if !bs.TestSource("source2", "phishing.com") {
		t.Fatal("expected source2 to still work after source1 reset")
	}

	// Should now have 1 source
	if bs.SourceCount() != 1 {
		t.Fatalf("expected source count 1 after reset, got %d", bs.SourceCount())
	}
}

func TestBloomManager_Likely(t *testing.T) {
	bm := NewBloomManager(10000)

	// Cold start: no entries
	if !bm.ColdStart() {
		t.Fatal("expected cold start to be true initially")
	}

	// Add entries
	keys, _ := ParseURL("https://malware.example.com/path/file.exe?ref=bad")
	bm.Add("src1", keys, []BloomType{BloomDomain, BloomHost, BloomPath, BloomFile, BloomQuery})

	// Stats should show sources
	stats := bm.Stats()
	if stats["domain"] == 0 && stats["host"] == 0 {
		t.Fatal("expected stats to show sources after add")
	}

	// Cold start should now be false
	if bm.ColdStart() {
		t.Fatal("expected cold start to be false after adding entries")
	}

	// Likely check for matching URL
	result, err := bm.Likely("https://malware.example.com/path/file.exe?ref=bad")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	if !result.Likely {
		t.Fatal("expected likely=true for known URL")
	}
	if len(result.Matches) == 0 {
		t.Fatal("expected at least one match")
	}

	// Verify MaxDepth is set
	if result.MaxDepth == 0 {
		t.Fatal("expected MaxDepth > 0")
	}

	// Check one of the matches has correct source
	foundDomain := false
	for _, m := range result.Matches {
		if m.Type == BloomDomain && m.SourceID == "src1" {
			foundDomain = true
		}
	}
	if !foundDomain {
		t.Fatalf("expected match for BloomDomain from src1, got %v", result.Matches)
	}

	// Check non-matching URL — use unlikely domain to avoid false positives
	result2, err := bm.Likely("https://safeunlikely2026x.com/other")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	// Note: Bloom filters may have ~1% false positives, so this is probabilistic
	// If it fails, re-run or adjust expectedItemsPerSet
	if result2.Likely {
		t.Log("Warning: unexpected false positive on unlikely domain")
	}
	if len(result2.Matches) > 0 {
		t.Logf("Warning: unexpected matches on unlikely domain: %v", result2.Matches)
	}
}

func TestBloomManager_AddAndRebuild(t *testing.T) {
	bm := NewBloomManager(10000)

	keys1, _ := ParseURL("https://evil.com/path1")
	bm.Add("src1", keys1, []BloomType{BloomDomain, BloomHostPath})

	keys2, _ := ParseURL("https://phish.com/path2")
	bm.Add("src2", keys2, []BloomType{BloomDomain, BloomHostPath})

	// Both should match
	r1, _ := bm.Likely("https://evil.com/path1")
	if !r1.Likely {
		t.Fatal("expected evil.com to match")
	}

	r2, _ := bm.Likely("https://phish.com/path2")
	if !r2.Likely {
		t.Fatal("expected phish.com to match")
	}

	// Verify source-specific matching
	hasSrc1 := false
	hasSrc2 := false
	for _, m := range r1.Matches {
		if m.SourceID == "src1" {
			hasSrc1 = true
		}
		if m.SourceID == "src2" {
			hasSrc2 = true
		}
	}
	if !hasSrc1 {
		t.Fatal("expected match from src1")
	}
	if hasSrc2 {
		t.Fatal("did not expect match from src2 for evil.com")
	}
}

func TestBloomManager_EmptyURL(t *testing.T) {
	bm := NewBloomManager(1000)

	// Empty URL should error
	_, err := bm.Likely("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestBloomManager_MultipleTypes(t *testing.T) {
	bm := NewBloomManager(10000)

	// Add entries with ALL bloom types
	keys, _ := ParseURL("https://user:pass@evil.com/path/file.exe?q=1")
	bm.Add("src1", keys, []BloomType{
		BloomDomain, BloomHost, BloomHostPath,
		BloomPath, BloomFile, BloomQuery, BloomLogin,
	})

	result, err := bm.Likely("https://user:pass@evil.com/path/file.exe?q=1")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}

	if !result.Likely {
		t.Fatal("expected likely=true")
	}

	// Should have matches for multiple types
	typeSet := make(map[BloomType]bool)
	for _, m := range result.Matches {
		typeSet[m.Type] = true
	}

	// At minimum domain and host should match
	if !typeSet[BloomDomain] {
		t.Fatalf("expected Domain match, got types: %v", typeSet)
	}
	if !typeSet[BloomHost] {
		t.Fatalf("expected Host match, got types: %v", typeSet)
	}
}

func TestConfidenceLevel(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0.95, "critical"},
		{0.90, "critical"},
		{0.85, "high"},
		{0.70, "high"},
		{0.60, "medium"},
		{0.50, "medium"},
		{0.40, "low"},
		{0.25, "low"},
		{0.20, "informational"},
		{0.0, "informational"},
	}
	for _, tc := range tests {
		got := ConfidenceLevel(tc.score)
		if got != tc.want {
			t.Errorf("ConfidenceLevel(%.2f) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestDepthWeights(t *testing.T) {
	// Verify all depth weights are present and positive
	for _, bt := range []BloomType{
		BloomDomain, BloomHost, BloomHostPath, BloomPath,
		BloomQuery, BloomFile, BloomLogin, BloomIP,
	} {
		w, ok := DepthWeight[bt]
		if !ok {
			t.Errorf("missing depth weight for %q", bt)
		}
		if w <= 0 || w > 2 {
			t.Errorf("depth weight for %q out of expected range: %.2f", bt, w)
		}
	}
}
