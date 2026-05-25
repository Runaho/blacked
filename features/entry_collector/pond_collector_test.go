package entry_collector

import (
	"testing"

	"blacked/features/entries"
)

// TestEntryToURLKeys_SchemeRouting verifies that entries with a non-empty Scheme
// do NOT populate URLKeys.IP — scheme-prefixed URLs must go to the full_url
// bloom filter, not the IP bloom filter. Only bare IPs (no scheme) should be
// routed to the IP bloom. Port is preserved in bloom key.
func TestEntryToURLKeys_SchemeRouting(t *testing.T) {
	tests := []struct {
		name   string
		entry  *entries.Entry
		wantIP string
	}{
		{
			name:   "bare IP, no scheme — goes to IP bloom",
			entry:  &entries.Entry{Host: "185.234.72.15", Scheme: ""},
			wantIP: "185.234.72.15",
		},
		{
			name:   "IP:port, no scheme — port preserved in bloom key",
			entry:  &entries.Entry{Host: "185.234.72.15:8080", Scheme: ""},
			wantIP: "185.234.72.15:8080",
		},
		{
			name:   "scheme-prefixed IP — must NOT go to IP bloom (routes to full_url)",
			entry:  &entries.Entry{Host: "185.234.72.15", Scheme: "http"},
			wantIP: "",
		},
		{
			name:   "scheme-prefixed IP:port — must NOT go to IP bloom",
			entry:  &entries.Entry{Host: "185.234.72.15:8080", Scheme: "ftp"},
			wantIP: "",
		},
		{
			name:   "domain only, no scheme — no IP populated",
			entry:  &entries.Entry{Host: "example.com", Domain: "example.com", Scheme: ""},
			wantIP: "",
		},
		{
			name:   "domain with scheme — no IP populated",
			entry:  &entries.Entry{Host: "example.com", Domain: "example.com", Scheme: "https"},
			wantIP: "",
		},
		{
			name:   "IPv6, no scheme — populated",
			entry:  &entries.Entry{Host: "2001:db8::1", Scheme: ""},
			wantIP: "2001:db8::1",
		},
		{
			name:   "IPv6 with scheme — NOT populated",
			entry:  &entries.Entry{Host: "2001:db8::1", Scheme: "http"},
			wantIP: "",
		},
		{
			name:   "empty host — no IP",
			entry:  &entries.Entry{Host: "", Scheme: ""},
			wantIP: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entryToURLKeys(tt.entry)
			if got.IP != tt.wantIP {
				t.Errorf("entryToURLKeys(%+v).IP = %q, want %q", tt.entry, got.IP, tt.wantIP)
			}
		})
	}
}