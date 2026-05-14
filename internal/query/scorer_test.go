package query

import (
	"testing"
)

func TestScorer_Score_NoMatches(t *testing.T) {
	s := NewScorer(nil)
	score, level := s.Score(nil)
	if score != 0.0 || level != "informational" {
		t.Fatalf("expected 0.0/informational, got %.2f/%s", score, level)
	}
}

func TestScorer_Score_SingleMatch(t *testing.T) {
	trust := map[string]float64{"spamhaus": 0.95}
	s := NewScorer(trust)

	matches := []Match{
		{SourceID: "spamhaus", Type: "host_path", Key: "evil.com/payload"},
	}

	// host_path weight = 1.0, spamhaus trust = 0.95
	// score = (0.95 * 1.0) / 0.95 = 1.0 → "critical"
	score, level := s.Score(matches)
	if score < 0.90 || level != "critical" {
		t.Fatalf("expected critical (~1.0), got %.2f/%s", score, level)
	}
}

func TestScorer_Score_MultipleMatches(t *testing.T) {
	trust := map[string]float64{
		"spamhaus": 0.95,
		"urlhaus":  0.85,
	}
	s := NewScorer(trust)

	matches := []Match{
		{SourceID: "spamhaus", Type: "domain", Key: "evil.com"},
		{SourceID: "urlhaus", Type: "host_path", Key: "evil.com/payload"},
		{SourceID: "urlhaus", Type: "file", Key: "payload.exe"},
	}

	// spamhaus (domain: 0.3): 0.95 * 0.3 = 0.285
	// urlhaus (host_path: 1.0): 0.85 * 1.0 = 0.850
	// urlhaus (file: 0.7): 0.85 * 0.7 = 0.595
	// totalWeighted = 0.285 + 0.850 + 0.595 = 1.730
	// totalTrust = 0.95 + 0.85 + 0.85 = 2.65
	// score = 1.730 / 2.65 ≈ 0.653 → "medium"
	score, level := s.Score(matches)
	if level != "medium" {
		t.Fatalf("expected medium (~0.65), got %.2f/%s", score, level)
	}
}

func TestScorer_Score_UnknownSourceDefaults(t *testing.T) {
	s := NewScorer(nil) // no trust config

	matches := []Match{
		{SourceID: "unknown", Type: "domain", Key: "evil.com"},
	}

	// trust=0.5, weight=0.3 → score = 0.3 (0.5*0.3/0.5) → "low"
	score, level := s.Score(matches)
	if level != "low" {
		t.Fatalf("expected low (~0.3), got %.2f/%s", score, level)
	}
}

func TestScorer_Score_AllSameTrust(t *testing.T) {
	trust := map[string]float64{
		"source1": 1.0,
		"source2": 1.0,
	}
	s := NewScorer(trust)

	matches := []Match{
		{SourceID: "source1", Type: "domain", Key: "x"},
		{SourceID: "source2", Type: "ip", Key: "1.2.3.4"},
	}

	// source1: 1.0*0.3 = 0.3, source2: 1.0*0.8 = 0.8
	// total = 1.1 / (1.0+1.0) = 0.55 → "medium"
	score, level := s.Score(matches)
	if level != "medium" {
		t.Fatalf("expected medium (~0.55), got %.2f/%s", score, level)
	}
}

func TestScorer_Score_DivergentTrusts(t *testing.T) {
	trust := map[string]float64{
		"lowtrust":  0.1,
		"hightrust": 0.95,
	}
	s := NewScorer(trust)

	matches := []Match{
		{SourceID: "lowtrust", Type: "domain", Key: "x"},
		{SourceID: "hightrust", Type: "host_path", Key: "x/path"},
	}

	// lowtrust: 0.1*0.3 = 0.03, hightrust: 0.95*1.0 = 0.95
	// total = 0.98 / (0.1+0.95) = 0.933 → "critical"
	score, level := s.Score(matches)
	if level != "critical" {
		t.Fatalf("expected critical (~0.93), got %.2f/%s", score, level)
	}
}

func TestScorer_ScoreWithResult_Empty(t *testing.T) {
	s := NewScorer(nil)
	score, level := s.ScoreWithResult(nil)
	if score != 0.0 || level != "informational" {
		t.Fatalf("expected 0.0/informational, got %.2f/%s", score, level)
	}
}

func TestScorer_ScoreWithResult_Multiple(t *testing.T) {
	trust := map[string]float64{
		"source1": 0.9,
		"source2": 0.5,
	}
	s := NewScorer(trust)
	score, level := s.ScoreWithResult([]string{"source1", "source2"})
	// (0.9 + 0.5) / 2 = 0.7 → "high"
	if level != "high" || score < 0.69 || score > 0.71 {
		t.Fatalf("expected high (0.7), got %.2f/%s", score, level)
	}
}

func TestConfidenceLevel_AllBands(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0.95, "critical"},
		{0.90, "critical"},
		{0.80, "high"},
		{0.70, "high"},
		{0.60, "medium"},
		{0.50, "medium"},
		{0.30, "low"},
		{0.25, "low"},
		{0.10, "informational"},
		{0.00, "informational"},
	}
	for _, tc := range tests {
		got := confidenceLevel(tc.score)
		if got != tc.want {
			t.Errorf("confidenceLevel(%.2f) = %s, want %s", tc.score, got, tc.want)
		}
	}
}
