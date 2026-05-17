package query

import "context"

// Match represents a single hit from a specific source at a specific bloom type/
// decomposition level.
type Match struct {
	SourceID   string  `json:"source_id"`
	Type       string  `json:"type"`      // e.g. "domain", "host_path", "path", "query", "file", "login", "ip"
	Key        string  `json:"key"`
	TrustScore float64 `json:"trust_score,omitempty"`
}

// QueryResponse is the full result from a Hit check (bloom + DB + score).
type QueryResponse struct {
	URL        string  `json:"url"`
	Blocked    bool    `json:"blocked"`
	Confidence float64 `json:"confidence"`
	Level      string  `json:"level"` // critical, high, medium, low, informational
	Matches    []Match `json:"matches"`
}

// LikelyResponse is the fast bloom-only result (~0.4ms).
type LikelyResponse struct {
	URL      string  `json:"url"`
	Likely   bool    `json:"likely"`
	MaxDepth int     `json:"max_depth"` // 0-100 scale
	Matches  []Match `json:"matches,omitempty"`
}

// SearchFilter holds parameters for filtered search.
type SearchFilter struct {
	Domain   string
	Host     string
	Path     string
	Query    string
	IP       string
	SourceID string
	Category string
	Limit    int
	Offset   int
}

// Entry is a lightweight representation of a database entry for query results.
// Mirrors the new schema entries table.
type Entry struct {
	ID         string
	SourceID   string
	Domain     string
	Host       string
	Path       string
	Scheme     string
	Confidence float64
	Category   string
}

// EntryRepository defines the DB operations needed by QueryService.
// Zero HTTP dependencies.
type EntryRepository interface {
	// SearchEntries queries the entries table with optional filters.
	SearchEntries(ctx context.Context, filter SearchFilter) ([]Entry, error)

	// ExistsByHost confirms whether any non-deleted entry exists for a hostname.
	// Used by Hit to verify bloom positives against the DB.
	ExistsByHost(ctx context.Context, host string) (bool, error)
}
