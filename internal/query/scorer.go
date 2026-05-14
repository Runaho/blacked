package query

import (
	"github.com/rs/zerolog/log"
)

// Scorer computes confidence scores from bloom matches.
// Stub implementation — returns 0.5 / "medium" until Phase 5 wires real scoring.
type Scorer struct {
	trust map[string]float64
}

// NewScorer creates a new Scorer. The trust parameter may be nil in stub mode.
func NewScorer(trustLookup map[string]float64) *Scorer {
	if trustLookup == nil {
		trustLookup = make(map[string]float64)
	}
	return &Scorer{trust: trustLookup}
}

// Score calculates a confidence score and level from a set of bloom matches.
// Stub: returns fixed 0.5 / "medium".
func (s *Scorer) Score(matches []Match) (float64, string) {
	if len(matches) == 0 {
		return 0.0, "informational"
	}

	// TODO(phase5): real scoring with trust scores and depth weights
	// For now, return a stub that distinguishes "has matches" from "no matches".
	return 0.5, "medium"
}

// depthWeight maps bloom type to its depth weight for confidence calculation.
var depthWeight = map[string]float64{
	"domain":    0.3,
	"host":      0.5,
	"host_path": 1.0,
	"path":      0.6,
	"query":     0.4,
	"file":      0.7,
	"login":     0.8,
	"ip":        0.8,
}

// ScoreWithResult calculates confidence from bloom result matches.
func (s *Scorer) ScoreWithResult(sourceIDs []string) (float64, string) {
	if len(sourceIDs) == 0 {
		return 0.0, "informational"
	}

	var totalWeight, totalTrust float64
	for _, sourceID := range sourceIDs {
		trust, ok := s.trust[sourceID]
		if !ok {
			trust = 0.5
		}
		totalWeight += trust
		totalTrust += trust
	}

	if totalTrust == 0 {
		return 0.0, "informational"
	}

	score := totalWeight / totalTrust
	level := confidenceLevel(score)
	log.Trace().Float64("score", score).Str("level", level).Int("matches", len(sourceIDs)).Msg("Scorer scored matches")
	return score, level
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
