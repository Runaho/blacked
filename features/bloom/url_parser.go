package bloom

import (
	"errors"
	"net"
	"net/url"
	"path"
	"strings"
	"sync"

	"blacked/internal/utils"
)

var ErrInvalidURL = errors.New("invalid URL")

var (
	urlCache sync.Map // key: URL string, value: *URLKeys
)

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

// CheckKey pairs a BloomType with a key to check against.
type CheckKey struct {
	Type BloomType
	Key  string
}

// ParseURL breaks a URL into all bloom-filtered components.
// Returns ErrInvalidURL if the input is empty or unparseable.
// File detection uses path.Ext(): only the final path segment with an extension
// is considered a file (per WHATWG URL Standard — path segments have no
// predefined meaning, so we use the extension heuristic).
func ParseURL(raw string) (*URLKeys, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidURL
	}

	// Check cache
	if cached, ok := urlCache.Load(raw); ok {
		return cached.(*URLKeys), nil
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
		// Use strings.Builder for efficient concatenation
		var builder strings.Builder
		builder.Grow(len(host) + len(clean))
		builder.WriteString(host)
		builder.WriteString(clean)
		keys.HostPath = builder.String()
		keys.Path = clean
	}

	if u.RawQuery != "" {
		keys.Query = u.RawQuery
	}

	// File detection: only the final path segment with a real extension.
	// path.Ext("file.") returns "." — skip single-dot pseudo-extensions.
	if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
		if ext := path.Ext(base); ext != "" && len(ext) > 1 {
			keys.File = base
		}
	}

	if u.User != nil {
		login := u.User.Username()
		if pass, ok := u.User.Password(); ok {
			login += ":" + pass
		}
		keys.Login = login
	}

	if trimmed := strings.TrimSpace(host); trimmed != "" {
		if net.ParseIP(trimmed) != nil {
			keys.IP = trimmed
		}
	}

	// Store in cache
	urlCache.Store(raw, keys)
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
		case BloomFullURL:
			if uk.Host != "" && uk.Path != "" {
				full := uk.Host + uk.Path
				if uk.Query != "" {
					full += "?" + uk.Query
				}
				out[t] = full
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

// GenerateCheckKeys builds the ordered check chain for a URL.
// Order: Domain → Host → IP → HostPath (parents) → File → FullURL.
// Shallowest bloom first; first hit wins in parallel check.
func (uk *URLKeys) GenerateCheckKeys() []CheckKey {
	// Preallocate with estimated capacity:
	// domain(1) + host(1) + ip(1) + hostPaths(max) + file(1) + fullURL(1)
	// hostPaths max = len(parentPaths(uk.Path)) if host and path present
	hostPathCount := 0
	var parentPathsResult []string
	if uk.Host != "" && uk.Path != "" {
		parentPathsResult = parentPaths(uk.Path)
		hostPathCount = len(parentPathsResult)
	}
	estimatedCap := 4 + hostPathCount // domain+host+ip+fullURL + hostPaths
	
	keys := make([]CheckKey, 0, estimatedCap)

	// 1. Domain (widest)
	if uk.Domain != "" {
		keys = append(keys, CheckKey{BloomDomain, uk.Domain})
	}

	// 2. Host
	if uk.Host != "" {
		keys = append(keys, CheckKey{BloomHost, uk.Host})
	}

	// 3. IP (exact match — after host, before path)
	if uk.IP != "" {
		keys = append(keys, CheckKey{BloomIP, uk.IP})
	}

	// 4. HostPath variants — shallowest → deepest
	if uk.Host != "" && uk.Path != "" {
		for _, p := range parentPathsResult {
			keys = append(keys, CheckKey{BloomHostPath, uk.Host + p})
		}
	}

	// 4. File
	if uk.File != "" {
		keys = append(keys, CheckKey{BloomFile, uk.File})
	}

	// 5. FullURL (most specific)
	if uk.Host != "" && uk.Path != "" {
		full := uk.Host + uk.Path
		if uk.Query != "" {
			full += "?" + uk.Query
		}
		keys = append(keys, CheckKey{BloomFullURL, full})
	}

	return keys
}

// parentPaths returns all path prefixes from shallowest → deepest,
// including the full path itself. Used by GenerateCheckKeys to scan
// every HostPath level the check URL might fall under.
// "/a/b/c/file.exe" → ["/a", "/a/b", "/a/b/c", "/a/b/c/file.exe"]
func parentPaths(p string) []string {
	p = strings.TrimSuffix(p, "/")
	if p == "" || p == "/" {
		return nil
	}
	parts := strings.Split(p, "/")
	
	// Preallocate result with exact capacity: len(parts)-1 prefixes
	result := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		result = append(result, strings.Join(parts[:i+1], "/"))
	}
	return result
}
