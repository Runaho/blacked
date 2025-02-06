package blackLinks

import (
	"net/url"
	"time"
)

type Entry struct {
	ID         string    `json:"id"`
	Scheme     string    `json:"scheme"`
	Domain     string    `json:"domain"`
	Host       string    `json:"host"` // Includes Domain + TLD
	SubDomains []string  `json:"sub_domains"`
	Path       string    `json:"path"`
	RawQuery   string    `json:"raw_query"`
	SourceURL  string    `json:"source_url"`           // URL where this data was fetched from
	Source     string    `json:"source"`               // Name of the provider
	Category   string    `json:"category,omitempty"`   // Optional category
	Confidence float64   `json:"confidence,omitempty"` // Optional confidence score
	CreatedAt  time.Time `json:"created_at"`           // Use time.Time for proper time handling and comparisons
	UpdatedAt  time.Time `json:"updated_at"`           // Use time.Time for update tracking
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
	b.SourceURL = link

	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	b.Scheme = u.Scheme
	b.Host = u.Host
	b.Domain = getDomain(u.Host) // Extract domain part
	b.Path = u.Path
	b.RawQuery = u.RawQuery
	b.SubDomains = subdomains(u.Host)

	return nil
}
