package utils

import (
	"errors"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/publicsuffix"
)

// Error variables for URL utils
var (
	ErrInvalidHost         = errors.New("invalid host")
	ErrPublicSuffixParsing = errors.New("public suffix parsing failed")
	ErrMalformedURL        = errors.New("malformed URL")
)

// Given a full host (like "foo.bar.example.co.uk"),
// return domain = "example.co.uk", subdomains = []string{"foo", "bar"}.
func ExtractDomainAndSubDomains(host string) (domain string, subs []string, err error) {
	// 1) Try to determine the “effective top-level domain + 1” using the PSL library.
	eTLDPlusOne, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		if log.Trace().Enabled() {
			log.Trace().Err(err).Str("host", host).Msg("EffectiveTLDPlusOne failed, using naive fallback")
		}
		// If PSL fails, fall back to the naive approach.
		parts := strings.Split(host, ".")
		if len(parts) <= 1 {
			return host, nil, nil // Just a single word or localhost
		}
		domain = strings.Join(parts[len(parts)-2:], ".") // Last two parts are domain + TLD
		subs = parts[:len(parts)-2]                      // Everything before the last two parts are subdomains
		return domain, subs, nil
	}

	if eTLDPlusOne == host {
		if log.Trace().Enabled() {
			log.Trace().Str("host", host).Msg("EffectiveTLDPlusOne returned full host, using naive fallback")
		}
		// PSL returned the full host, which is not expected, use naive approach
		parts := strings.Split(host, ".")
		if len(parts) <= 1 {
			return host, nil, nil // Just a single word or localhost
		}
		domain = strings.Join(parts[len(parts)-2:], ".") // Last two parts are domain + TLD
		subs = parts[:len(parts)-2]                      // Everything before the last two parts are subdomains
		return domain, subs, nil
	}

	// 2) Trim away the domain portion from the host to get the subdomains portion.
	subPortion := strings.TrimSuffix(host, eTLDPlusOne)

	// 3) Remove any trailing dot left over.
	subPortion = strings.TrimSuffix(subPortion, ".")

	// 4) Split the remainder on dots for the subdomains. If empty, no subdomains.
	if subPortion == "" {
		subs = nil
	} else {
		subs = strings.Split(subPortion, ".")
	}

	return eTLDPlusOne, subs, nil
}

func NormalizeURL(link string) string {
	// 1. Convert to lowercase:
	link = strings.ToLower(link)

	// 2. Parse the URL to handle encoding and path manipulation correctly:
	parsedURL, err := url.Parse(link)
	if err != nil {
		log.Err(err).Str("url", link).Msg("Failed to parse URL for normalization")
		return link
	}

	// 3. Normalize the path (remove trailing slashes, etc.):
	parsedURL.Path = strings.TrimRight(parsedURL.Path, "/")

	// 4. Re-encode the URL to ensure consistent encoding:
	normalizedURL := parsedURL.String()

	return normalizedURL
}
