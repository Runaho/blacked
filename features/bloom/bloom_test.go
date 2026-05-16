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

	// source2 should still work
	if !bs.TestSource("source2", "phishing.com") {
		t.Fatal("expected source2 to still work after source2 reset")
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

	// Populate entries using PopulateEntry (single-bloom-per-entry)
	keys1, _ := ParseURL("https://malware.example.com/path/file.exe?ref=bad")
	bm.PopulateEntry("src1", keys1)
	keys2, _ := ParseURL("https://malware.example.com/path")
	bm.PopulateEntry("src1", keys2)

	// Stats should show sources
	stats := bm.Stats()
	if stats["domain"] == 0 && stats["host"] == 0 && stats["host_path"] == 0 {
		t.Fatal("expected stats to show sources after populate")
	}

	// Cold start should now be false
	if bm.ColdStart() {
		t.Fatal("expected cold start to be false after adding entries")
	}

	// Likely check for matching URL (file.exe with query → FullURL bloom)
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

	// Check match is from src1
	foundSrc1 := false
	for _, m := range result.Matches {
		if m.SourceID == "src1" {
			foundSrc1 = true
		}
	}
	if !foundSrc1 {
		t.Fatalf("expected match from src1, got %v", result.Matches)
	}

	// Check non-matching URL
	result2, err := bm.Likely("https://safeunlikely2026x.com/other")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	if result2.Likely {
		t.Log("Warning: unexpected false positive on unlikely domain")
	}
	if len(result2.Matches) > 0 {
		t.Logf("Warning: unexpected matches on unlikely domain: %v", result2.Matches)
	}
}

func TestBloomManager_PopulateAndCheck(t *testing.T) {
	bm := NewBloomManager(10000)

	// Populate HostPath entry
	keys1, _ := ParseURL("https://evil.com/path1")
	bm.PopulateEntry("src1", keys1)

	// Populate another HostPath entry
	keys2, _ := ParseURL("https://phish.com/path2")
	bm.PopulateEntry("src2", keys2)

	// Both should match via parallel Likely
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

	_, err := bm.Likely("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestBloomManager_PopulateEntry_Targets(t *testing.T) {
	bm := NewBloomManager(10000)

	// FullURL: file + query → BloomFullURL
	keys, _ := ParseURL("https://cdn.example.com/malware/shell.php?ref=evil")
	bm.PopulateEntry("src1", keys)

	result, err := bm.Likely("https://cdn.example.com/malware/shell.php?ref=evil")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	if !result.Likely {
		t.Fatal("expected FullURL bloom hit with exact query match")
	}
	foundFull := false
	for _, m := range result.Matches {
		if m.Type == BloomFullURL {
			foundFull = true
		}
		if m.Type == BloomFile {
			t.Fatal("unexpected File bloom match — entry should be FullURL only")
		}
	}
	if !foundFull {
		t.Fatal("expected FullURL match")
	}

	// Query mismatch — provider responsibility
	result2, err := bm.Likely("https://cdn.example.com/malware/shell.php?ref=other")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	if result2.Likely {
		t.Fatal("expected MISS on different query — provider responsibility")
	}

	// File: file without query → BloomFile
	keys2, _ := ParseURL("https://files.example.com/virus.exe")
	bm.PopulateEntry("src2", keys2)

	result3, err := bm.Likely("https://files.example.com/virus.exe")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	if !result3.Likely {
		t.Fatal("expected File bloom hit")
	}
	foundFile := false
	for _, m := range result3.Matches {
		if m.Type == BloomFile {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatal("expected File match")
	}

	// Domain only
	keys3, _ := ParseURL("https://domain-bloom-test.com")
	bm.PopulateEntry("src3", keys3)

	result4, err := bm.Likely("https://sub.domain-bloom-test.com/anything")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	if !result4.Likely {
		t.Fatal("expected Domain bloom hit for subdomain")
	}
}

func TestParentPath_Match(t *testing.T) {
	bm := NewBloomManager(10000)

	keys, _ := ParseURL("https://www.example.com/a/b/c/exploit")
	bm.PopulateEntry("src1", keys)

	// Exact path match
	r1, _ := bm.Likely("https://www.example.com/a/b/c/exploit")
	if !r1.Likely {
		t.Fatal("expected exact HostPath match")
	}

	// Parent path match — /a/b/c/exploit is under /a/b
	// /a/b/c/exploit → HostPath key is "www.example.com/a/b/c/exploit" (no file ext)
	// Check generates parent paths: /a, /a/b, /a/b/c
	// Populate entry HostPath key: "www.example.com/a/b/c/exploit" — this is NOT one of the parent paths
	// The check will generate: [/a, /a/b, /a/b/c] which won't hit "www.example.com/a/b/c/exploit"
	// Wait — GenerateCheckKeys generates HostPath variants from the CHECK URL's path, not from the populate key.
	// For check URL "/a/b/c/exploit" → parentPaths gives ["/a", "/a/b", "/a/b/c"]
	// Populate entry stores "www.example.com/a/b/c/exploit" — no match to any parent path.
	// The parent path match only works when the populate key IS one of the parent paths
	// and the check URL is a child. The populate key is the exact HostPath.
	// So parent path matching goes: populate stores /a, check URL is /a/b/c → hits /a on parent scan.
	// Not the other way around.

	// Let's test the correct direction: populate a parent, check a child.
	keys2, _ := ParseURL("https://www.example.com/a")
	bm.PopulateEntry("src2", keys2)

	r2, _ := bm.Likely("https://www.example.com/a/b/c/exploit")
	if !r2.Likely {
		t.Fatal("expected parent HostPath match: /a populated, /a/b/c checked")
	}

	// Verify at least one HostPath match (first-hit-wins in parallel check
	// makes the winning source non-deterministic — both src1 and src2 are valid)
	foundHP := false
	for _, m := range r2.Matches {
		if m.Type == BloomHostPath {
			foundHP = true
		}
	}
	if !foundHP {
		t.Fatalf("expected HostPath match, got %v", r2.Matches)
	}
}

func TestDifferentSubdomain_NoMatch(t *testing.T) {
	bm := NewBloomManager(10000)

	keys, _ := ParseURL("https://sub1.example.com/path")
	bm.PopulateEntry("src1", keys)

	// Different subdomain
	result, err := bm.Likely("https://sub2.example.com/path")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}

	// HostPath key is "sub1.example.com/path" — check generates "sub2.example.com/path"
	// These don't match. But Host key "sub2.example.com" vs populated "sub1.example.com" — different.
	// Domain check: "example.com" vs "example.com" — MATCH! But domain was not populated.
	if result.Likely {
		t.Fatal("expected no match for different subdomain")
	}
}

func TestQuery_ProviderResponsibility(t *testing.T) {
	bm := NewBloomManager(10000)

	// Populate with query → FullURL
	keys, _ := ParseURL("https://cdn.example.com/shell.php?ref=evil")
	bm.PopulateEntry("src1", keys)

	// Queryless check should MISS
	result, err := bm.Likely("https://cdn.example.com/shell.php")
	if err != nil {
		t.Fatalf("Likely failed: %v", err)
	}
	if result.Likely {
		t.Fatal("expected MISS on queryless check when populate had query — provider responsibility")
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
		BloomDomain, BloomHost, BloomHostPath,
		BloomFile, BloomFullURL, BloomLogin, BloomIP,
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

func TestParseURL_FileDetection(t *testing.T) {
	tests := []struct {
		url  string
		file string
	}{
		{"https://example.com/virus.exe", "virus.exe"},
		{"https://example.com/path/shell.php?ref=evil", "shell.php"},
		{"https://example.com/login", ""},            // no extension
		{"https://example.com/exploit", ""},           // no extension
		{"https://github.com/guneskorkmaz", ""},       // no extension
		{"https://example.com/file.", ""},             // dot but no extension
		{"https://example.com/path.tar.gz", "path.tar.gz"}, // double extension
	}

	for _, tc := range tests {
		keys, err := ParseURL(tc.url)
		if err != nil {
			t.Fatalf("ParseURL(%q) failed: %v", tc.url, err)
		}
		if keys.File != tc.file {
			t.Errorf("ParseURL(%q): file = %q, want %q", tc.url, keys.File, tc.file)
		}
	}
}

func TestGenerateCheckKeys(t *testing.T) {
	keys, err := ParseURL("https://sub.example.com/a/b/c/file.exe?x=1")
	if err != nil {
		t.Fatalf("ParseURL failed: %v", err)
	}

	checkKeys := keys.GenerateCheckKeys()

	// Expected order: Domain → Host → HostPath parents + full → File → FullURL
	// Domain: example.com
	// Host: sub.example.com
	// HostPath (parentPaths): sub.example.com/a, sub.example.com/a/b, sub.example.com/a/b/c, sub.example.com/a/b/c/file.exe
	// File: file.exe
	// FullURL: sub.example.com/a/b/c/file.exe?x=1

	if len(checkKeys) != 8 {
		t.Fatalf("expected 8 check keys, got %d: %v", len(checkKeys), checkKeys)
	}

	if checkKeys[0].Type != BloomDomain || checkKeys[0].Key != "example.com" {
		t.Errorf("key[0] = %v, want Domain:example.com", checkKeys[0])
	}
	if checkKeys[1].Type != BloomHost || checkKeys[1].Key != "sub.example.com" {
		t.Errorf("key[1] = %v, want Host:sub.example.com", checkKeys[1])
	}
	if checkKeys[2].Type != BloomHostPath || checkKeys[2].Key != "sub.example.com/a" {
		t.Errorf("key[2] = %v, want HostPath:sub.example.com/a", checkKeys[2])
	}
	if checkKeys[3].Type != BloomHostPath || checkKeys[3].Key != "sub.example.com/a/b" {
		t.Errorf("key[3] = %v, want HostPath:sub.example.com/a/b", checkKeys[3])
	}
	if checkKeys[4].Type != BloomHostPath || checkKeys[4].Key != "sub.example.com/a/b/c" {
		t.Errorf("key[4] = %v, want HostPath:sub.example.com/a/b/c", checkKeys[4])
	}
	if checkKeys[5].Type != BloomHostPath || checkKeys[5].Key != "sub.example.com/a/b/c/file.exe" {
		t.Errorf("key[5] = %v, want HostPath:sub.example.com/a/b/c/file.exe", checkKeys[5])
	}
	if checkKeys[6].Type != BloomFile || checkKeys[6].Key != "file.exe" {
		t.Errorf("key[6] = %v, want File:file.exe", checkKeys[6])
	}
	if checkKeys[7].Type != BloomFullURL || checkKeys[7].Key != "sub.example.com/a/b/c/file.exe?x=1" {
		t.Errorf("key[7] = %v, want FullURL:sub.example.com/a/b/c/file.exe?x=1", checkKeys[7])
	}
}

func TestDetermineBloomTarget(t *testing.T) {
	tests := []struct {
		url     string
		wantBT  BloomType
		wantKey string
	}{
		{"https://cdn.example.com/virus.exe?ref=evil", BloomFullURL, "cdn.example.com/virus.exe?ref=evil"},
		{"https://cdn.example.com/virus.exe", BloomFile, "virus.exe"},
		{"https://www.example.com/exploit", BloomHostPath, "www.example.com/exploit"},
		{"https://sub.example.com", BloomHost, "sub.example.com"},
		{"https://example.com", BloomDomain, "example.com"}, // bare domain, no subdomain
	}

	for _, tc := range tests {
		keys, err := ParseURL(tc.url)
		if err != nil {
			t.Fatalf("ParseURL(%q) failed: %v", tc.url, err)
		}
		bt, key := determineBloomTarget(keys)
		if bt != tc.wantBT {
			t.Errorf("determineBloomTarget(%q): bt = %q, want %q", tc.url, bt, tc.wantBT)
		}
		if key != tc.wantKey {
			t.Errorf("determineBloomTarget(%q): key = %q, want %q", tc.url, key, tc.wantKey)
		}
	}
}
