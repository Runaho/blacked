package phishtank

import "io"

const (
	onlineValidJSONSource = "http://data.phishtank.com/data/online-valid.json.bz2"
	name                  = "PHISHTANK_ONLINE_VALID"
)

type OnlineValid struct{}

func (o *OnlineValid) Name() string {
	return name
}

func (o *OnlineValid) Source() string {
	return onlineValidJSONSource
}

func (o *OnlineValid) Fetch() (io.Reader, error) {
	panic("not implemented")
}

func (o *OnlineValid) Parse(data io.Reader) error {
	panic("not implemented")
}
