package oisd

import (
	"io"
)

const (
	oisdBigName   = "OISD_BIG"
	oisdBigSource = "https://big.oisd.nl/domainswild2"
)

type OisdBigProvider struct{}

// Name returns the name of the provider
func (o *OisdBigProvider) Name() string {
	return oisdBigName
}
func (o *OisdBigProvider) Source() string {
	return oisdBigSource
}

// Fetch retrieves the data from the source URL
func (o *OisdBigProvider) Fetch() (io.Reader, error) {
	panic("not implemented")
}

// Parse processes the fetched data
func (o *OisdBigProvider) Parse(data io.Reader) error {
	panic("not implemented")
}
