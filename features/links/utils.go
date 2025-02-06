package blackLinks

import "strings"

// Helper function to extract subdomains from a host
func subdomains(host string) []string {
	parts := strings.Split(host, ".")
	if len(parts) <= 2 { // domain.com or localhost, no subdomains to extract
		return nil
	}
	return parts[:len(parts)-2] // everything before the last two parts (domain + TLD) are considered subdomains
}

// Helper function to extract domain part from a host. e.g., from "sub.example.com" it returns "example.com"
func getDomain(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) <= 1 { // Just a single word or localhost
		return host // Return as is
	}
	return strings.Join(parts[len(parts)-2:], ".") // Last two parts are domain + TLD
}
