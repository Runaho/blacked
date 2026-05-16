package query

import (
	"github.com/rs/zerolog/log"
)

// Scorer computes confidence scores from bloom matches.
// Formula: confidence = Σ(trust_score × depth_weight) / Σ(trust_score)
type Scorer struct {
	trust map[string]float64
}

// NewScorer creates a new Scorer. Pass nil to use defaults (0.5 per source).
func NewScorer(trustLookup map[string]float64) *Scorer {
	if trustLookup == nil {
		trustLookup = make(map[string]float64)
	}
	return &Scorer{trust: trustLookup}
}

// Score calculates confidence from bloom match results.
// Uses the full formula with depth weights per bloom type.
func (s *Scorer) Score(matches []Match) (float64, string) {
	if len(matches) == 0 {
		return 0.0, "informational"
	}

	var totalWeighted, totalTrust float64
	for _, m := range matches {
		// Get trust score for this source (default 0.5)
		trust := 0.5
		if t, ok := s.trust[m.SourceID]; ok {
			trust = t
		}

		// Get depth weight for this bloom type (default 0.5 if unknown)
		weight := 0.5
		if w, ok := depthWeight[m.Type]; ok {
			weight = w
		}

		totalWeighted += trust * weight
		totalTrust += trust
	}

	if totalTrust == 0 {
		return 0.0, "informational"
	}

	score := totalWeighted / totalTrust
	level := confidenceLevel(score)

	log.Trace().
		Float64("score", score).
		Str("level", level).
		Int("matches", len(matches)).
		Msg("Scorer computed confidence")
	return score, level
}

// ScoreWithResult is a simplified version that uses source IDs only.
// Each unique source contributes trust, averaged (no depth weight).
// Used when match detail isn't available.
func (s *Scorer) ScoreWithResult(sourceIDs []string) (float64, string) {
	if len(sourceIDs) == 0 {
		return 0.0, "informational"
	}

	var totalTrust, count float64
	for _, sourceID := range sourceIDs {
		trust := 0.5
		if t, ok := s.trust[sourceID]; ok {
			trust = t
		}
		totalTrust += trust
		count++
	}

	if count == 0 {
		return 0.0, "informational"
	}

	score := totalTrust / count
	level := confidenceLevel(score)

	log.Trace().
		Float64("score", score).
		Str("level", level).
		Int("sources", len(sourceIDs)).
		Msg("Scorer scored sources (no depth weights)")
	return score, level
}

// depthWeight maps bloom type to its depth weight for confidence calculation.
var depthWeight = map[string]float64{
	"domain":    0.3,
	"host":      0.5,
	"host_path": 1.0,
	"path":      0.6,
	"query":     0.4,
	"file":      0.7,
	"full_url":  1.5,
	"login":     0.8,
	"ip":        0.8,
}

func confidenceLevel(score float64) string {
	switch {
	case score >= 0.90:
		return "critical"
	case score >= 0.70:
		return "high"
	case score >= 0.50:
		return "medium"
	case score >= 0.25:
		return "low"
	default:
		return "informational"
	}
}
