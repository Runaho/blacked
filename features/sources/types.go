package sources

import (
	"blacked/internal/db/models"
	"time"
)

// SourceType defines how a source provides its data.
type SourceType string

const (
	SourceTypeFlat SourceType = "flat"
	SourceTypeJSON SourceType = "json"
	SourceTypeCSV  SourceType = "csv"
	SourceTypeAPI  SourceType = "api"
	SourceTypeRSS  SourceType = "rss"
)

// BloomType defines the dimensions of URL decomposition for bloom filtering.
type BloomType string

const (
	BloomDomain   BloomType = "domain"
	BloomHost     BloomType = "host"
	BloomHostPath BloomType = "host_path"
	BloomPath     BloomType = "path"
	BloomQuery    BloomType = "query"
	BloomFile     BloomType = "file"
	BloomLogin    BloomType = "login"
	BloomIP       BloomType = "ip"
)

// Provider is the high-level entity that groups related Sources.
// It mirrors models.Provider but is a domain type in the sources package.
type Provider struct {
	ID              string
	Name            string
	Description     string
	TrustScore      float64
	DefaultInterval time.Duration
}

// ToModel converts this Provider to its database model.
func (p Provider) ToModel() models.Provider {
	return models.Provider{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		TrustScore:  p.TrustScore,
	}
}

// Source represents a unique feed/list under a Provider.
type Source struct {
	ID             string
	ProviderID     string
	Name           string
	SourceURL      string
	SourceType     SourceType
	TrustScore     *float64
	UpdateInterval *time.Duration
	BloomTypes     []BloomType
	Category       string
	Enabled        bool

	// Runtime dependencies (set by SourceBuilder)
	Fetcher Fetcher
	Parser  Parser
}


// FetchResult holds the outcome of a fetch operation.
type FetchResult struct {
	Data   []byte
	Status string
	Error  error
}

// ParseResult holds the outcome of a parse operation.
type ParseResult struct {
	EntryCount int
	Duration   time.Duration
	Error      error
}

// DepthWeight maps BloomType to its scoring weight.
var DepthWeight = map[BloomType]float64{
	BloomDomain:   0.3,
	BloomHost:     0.5,
	BloomHostPath: 1.0,
	BloomPath:     0.6,
	BloomQuery:    0.4,
	BloomFile:     0.7,
	BloomLogin:    0.8,
	BloomIP:       0.8,
}

// ConfidenceLevel maps a score to a human-readable level.
func ConfidenceLevel(score float64) string {
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
