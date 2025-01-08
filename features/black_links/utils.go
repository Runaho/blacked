package blackLinks

import "strings"

func subdomains(host string) []string {
	subdomains := []string{}
	parts := strings.Split(host, ".")
	for i := 0; i < len(parts)-2; i++ {
		subdomains = append(subdomains, strings.Join(parts[i:], "."))
	}

	return subdomains
}
