package entry_collector

import (
	"testing"

	"blacked/features/bloom"

	"github.com/stretchr/testify/assert"
)

// TestGetAllBloomTypes tests that GetAllBloomTypes returns all bloom types
func TestGetAllBloomTypes(t *testing.T) {
	types := GetAllBloomTypes()
	assert.Len(t, types, 7)
	assert.Contains(t, types, bloom.BloomDomain)
	assert.Contains(t, types, bloom.BloomHost)
	assert.Contains(t, types, bloom.BloomHostPath)
	assert.Contains(t, types, bloom.BloomFile)
	assert.Contains(t, types, bloom.BloomFullURL)
	assert.Contains(t, types, bloom.BloomLogin)
	assert.Contains(t, types, bloom.BloomIP)
}

// TestExtractFileFromPath tests file extraction from paths
func TestExtractFileFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"", ""},
		{"/", ""},
		{"/path/to/file.txt", "file.txt"},
		{"/path/to/file", ""},
		{"/path.with.dots/file.exe", "file.exe"},
		{"/download/malware.bin", "malware.bin"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := extractFileFromPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractIPFromHost tests IP extraction from host
func TestExtractIPFromHost(t *testing.T) {
	tests := []struct {
		name     string
		scheme   string
		host     string
		expected string
	}{
		{"empty host", "", "", ""},
		{"pure IP", "", "192.168.1.1", "192.168.1.1"},
		{"IP with port", "", "192.168.1.1:8080", "192.168.1.1:8080"},
		{"hostname without IP", "", "example.com", ""},
		{"URL with scheme should not extract IP", "https", "192.168.1.1", ""},
		{"localhost is not a valid IP", "", "localhost", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIPFromHost(tt.scheme, tt.host)
			assert.Equal(t, tt.expected, result)
		})
	}
}