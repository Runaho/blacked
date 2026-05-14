package bloom

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

// BloomMatch represents a single bloom filter match for a specific type and source.
type BloomMatch struct {
	Type     BloomType
	SourceID string
	Key      string
}

// BloomResult is the outcome of a bloom likelihood check.
type BloomResult struct {
	Likely   bool
	Matches  []BloomMatch
	MaxDepth int
}

// Entry is a lightweight representation of a database entry for bloom rebuild.
type Entry struct {
	SourceID string
	Domain   string
	Host     string
	Path     string
	File     string
	Query    string
	Login    string
	IP       string
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
