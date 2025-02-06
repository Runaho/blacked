package oisd

import "io"

const (
	nsfwSourceName = "OISD_NSFW"
	nsfwSource     = "https://nsfw.oisd.nl/domainswild2"
)

type OISDNSFW struct{}

func (o *OISDNSFW) Name() string {
	return nsfwSourceName
}

func (o *OISDNSFW) Source() string {
	return nsfwSource
}

func (o *OISDNSFW) Fetch() (io.Reader, error) {
	panic("not implemented")
}

func (o *OISDNSFW) Parse(data io.Reader) error {
	panic("not implemented")
}
