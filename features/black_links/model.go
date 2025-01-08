package blackLinks

import (
	"net/url"
)

type BlackListEntry struct {
	Scheme     string   `json:"scheme"`
	Domain     string   `json:"domain"`
	Host       string   `json:"host"`
	SubDomains []string `json:"sub_domain"`
	Path       string   `json:"path"`
	RawQuery   string   `json:"raw_query"`
	SourceURL  string   `json:"source_url"`
	Source     string   `json:"source"`
	Category   string   `json:"category"`
	Confidence float64  `json:"confidence"`
	CreatedAT  string   `json:"timestamp"`
	UpdatedAt  string   `json:"updated_at"`
}

func (b *BlackListEntry) GetURL() url.URL {
	return url.URL{
		Scheme:   b.Scheme,
		Host:     b.Host,
		Path:     b.Path,
		RawQuery: b.RawQuery,
	}
}

func (b *BlackListEntry) SetURL(link string) error {
	b.SourceURL = link

	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	b.Scheme = u.Scheme
	b.Host = u.Host
	b.Path = u.Path
	b.SubDomains = subdomains(u.Host)

	return nil
}
