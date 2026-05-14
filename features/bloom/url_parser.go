package bloom

import (
	"errors"
	"net/url"
	"path"
	"strings"

	"blacked/internal/utils"
)

var ErrInvalidURL = errors.New("invalid URL")

// URLKeys holds all decomposed keys from a URL for bloom filtering.
type URLKeys struct {
	Domain   string
	Host     string
	HostPath string
	Path     string
	Query    string
	File     string
	Login    string
	IP       string
}

// ParseURL breaks a URL into all bloom-filtered components.
// Returns ErrInvalidURL if the input is empty or unparseable.
func ParseURL(raw string) (*URLKeys, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidURL
	}

	if !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "//") {
		raw = "//" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, ErrInvalidURL
	}

	host := u.Hostname()
	if host == "" {
		return nil, ErrInvalidURL
	}

	domain, _, err := utils.ExtractDomainAndSubDomains(host)
	if err != nil {
		domain = host
	}

	keys := &URLKeys{}

	if domain != "" {
		keys.Domain = domain
	}
	keys.Host = host

	if u.Path != "" && u.Path != "/" {
		clean := path.Clean(u.Path)
		keys.HostPath = host + clean
		keys.Path = clean
	}

	if u.RawQuery != "" {
		keys.Query = u.RawQuery
	}

	if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
		keys.File = base
	}

	if u.User != nil {
		login := u.User.Username()
		if pass, ok := u.User.Password(); ok {
			login += ":" + pass
		}
		keys.Login = login
	}

	if trimmed := strings.TrimSpace(host); trimmed != "" {
		isIP := true
		if strings.Contains(trimmed, ":") {
			if !strings.HasPrefix(trimmed, "[") {
				isIP = false
			}
		} else if strings.Count(trimmed, ".") == 3 {
			parts := strings.Split(trimmed, ".")
			for _, p := range parts {
				if p == "" || p == "." {
					isIP = false
					break
				}
			}
		} else {
			isIP = false
		}
		if isIP {
			keys.IP = trimmed
		}
	}

	return keys, nil
}

// KeysFor returns a map of only the bloom types that have a value.
func (uk *URLKeys) KeysFor(types []BloomType) map[BloomType]string {
	out := make(map[BloomType]string, len(types))
	for _, t := range types {
		switch t {
		case BloomDomain:
			if uk.Domain != "" {
				out[t] = uk.Domain
			}
		case BloomHost:
			if uk.Host != "" {
				out[t] = uk.Host
			}
		case BloomHostPath:
			if uk.HostPath != "" {
				out[t] = uk.HostPath
			}
		case BloomPath:
			if uk.Path != "" {
				out[t] = uk.Path
			}
		case BloomQuery:
			if uk.Query != "" {
				out[t] = uk.Query
			}
		case BloomFile:
			if uk.File != "" {
				out[t] = uk.File
			}
		case BloomLogin:
			if uk.Login != "" {
				out[t] = uk.Login
			}
		case BloomIP:
			if uk.IP != "" {
				out[t] = uk.IP
			}
		}
	}
	return out
}
