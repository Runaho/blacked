package entries

import (
	"net/url"
	"strings"
	"time"
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

func (b *Entry) GetURL() url.URL {
	return url.URL{
		Scheme:   b.Scheme,
		Host:     b.Host,
		Path:     b.Path,
		RawQuery: b.RawQuery,
	}
}

func (b *Entry) SetURL(link string) error {
	_link := strings.TrimSpace(link)
	if !strings.Contains(_link, "://") && !strings.HasPrefix(_link, "//") {
		_link = "//" + _link
	}

	b.SourceURL = link
	u, err := url.Parse(_link)
	if err != nil {
		return err
	}

	b.Scheme = u.Scheme
	b.Host = u.Host

	// Extract domain + subdomains properly via PSL
	domain, subdomains, err := extractDomainAndSubDomains(u.Host)
	if err != nil {
		return err
	}

	b.Domain = domain
	b.SubDomains = subdomains
	b.Path = u.Path
	b.RawQuery = u.RawQuery

	return nil
}
