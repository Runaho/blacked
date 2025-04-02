package entries

import (
	"blacked/internal/collector"
	"blacked/internal/utils"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	ID         string     `json:"id"`
	ProcessID  string     `json:"process_id"`
	Scheme     string     `json:"scheme"`
	Domain     string     `json:"domain"`
	Host       string     `json:"host"` // Includes Domain + TLD
	SubDomains []string   `json:"sub_domains"`
	Path       string     `json:"path"`
	RawQuery   string     `json:"raw_query"`
	SourceURL  string     `json:"source_url"`           // URL where this data was fetched from
	Source     string     `json:"source"`               // Name of the provider
	Category   string     `json:"category,omitempty"`   // Optional category
	Confidence float64    `json:"confidence,omitempty"` // Optional confidence score
	CreatedAt  time.Time  `json:"created_at"`           // Use time.Time for proper time handling and comparisons
	UpdatedAt  time.Time  `json:"updated_at"`           // Use time.Time for update tracking
	DeletedAt  *time.Time `json:"deleted_at,omitempty"` // Pointer to time.Time, nil if not deleted, *time.Time if soft-deleted
}

// NewEntry creates a new Entry with default values
func NewEntry() *Entry {
	now := time.Now()
	return &Entry{
		ID:        uuid.New().String(),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// GetURL returns a url.URL representation of the entry
func (b *Entry) GetURL() url.URL {
	return url.URL{
		Scheme:   b.Scheme,
		Host:     b.Host,
		Path:     b.Path,
		RawQuery: b.RawQuery,
	}
}

// SetURL parses a URL string and populates the Entry fields
// This method can't be part of the fluent interface because it may fail
func (b *Entry) SetURL(link string) error {
	mc, err := collector.GetMetricsCollector()

	_link := strings.TrimSpace(link)
	if !strings.Contains(_link, "://") && !strings.HasPrefix(_link, "//") {
		_link = "//" + _link
	}

	b.SourceURL = link
	u, err := url.Parse(_link)
	if err != nil {
		if mc != nil {
			mc.IncrementImportErrors(b.Source)
		}
		return err
	}

	b.Scheme = u.Scheme
	b.Host = u.Host

	// Extract domain + subdomains properly via PSL
	domain, subdomains, err := utils.ExtractDomainAndSubDomains(u.Host)
	if err != nil {
		if mc != nil {
			mc.IncrementImportErrors(b.Source)
		}
		return err
	}

	b.Domain = domain
	b.SubDomains = subdomains
	b.Path = u.Path
	b.RawQuery = u.RawQuery
	b.UpdatedAt = time.Now()

	return nil
}

// WithSource sets the source name and returns the entry for chaining
func (b *Entry) WithSource(source string) *Entry {
	b.Source = source
	b.UpdatedAt = time.Now()
	return b
}

// WithProcessID sets the process ID and returns the entry for chaining
func (b *Entry) WithProcessID(processID string) *Entry {
	b.ProcessID = processID
	b.UpdatedAt = time.Now()
	return b
}

// WithCategory sets the category and returns the entry for chaining
func (b *Entry) WithCategory(category string) *Entry {
	b.Category = category
	b.UpdatedAt = time.Now()
	return b
}

// WithConfidence sets the confidence score and returns the entry for chaining
func (b *Entry) WithConfidence(confidence float64) *Entry {
	b.Confidence = confidence
	b.UpdatedAt = time.Now()
	return b
}

// Clone creates a copy of the Entry with a new ID
func (b *Entry) Clone() *Entry {
	clone := *b
	clone.ID = uuid.New().String()
	clone.UpdatedAt = time.Now()
	return &clone
}

// FromURL creates a new Entry from a URL string
func FromURL(link, source, processID string) (*Entry, error) {
	entry := NewEntry()
	entry.Source = source
	entry.ProcessID = processID

	if err := entry.SetURL(link); err != nil {
		return nil, err
	}
	return entry, nil
}
