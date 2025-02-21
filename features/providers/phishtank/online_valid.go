package phishtank

// NOTE: This is not implement because we cannot access the source. Without an account.
import (
	"io"

	"github.com/google/uuid"
)

type OnlineValid struct{}

func (o *OnlineValid) Name() string {
	return "PHISHTANK_ONLINE_VALID"
}

func (o *OnlineValid) Source() string {
	return "http://data.phishtank.com/data/online-valid.json.bz2"
}

func (o *OnlineValid) Fetch() (io.Reader, error) {
	panic("not implemented")
}

func (o *OnlineValid) Parse(data io.Reader) error {
	panic("not implemented")
}

func (o *OnlineValid) SetProcessID(id uuid.UUID) {
	panic("not implemented")
}
