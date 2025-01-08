package providers

import (
	"blacked/features/black_links/providers/oisd"
	"blacked/features/black_links/providers/phishtank"
	"io"
)

type Provider interface {
	Name() string
	Source() string
	Fetch() (io.Reader, error)
	Parse(data io.Reader) error
}

type Providers []Provider

func (Providers) Get() Providers {
	return Providers{
		&oisd.OisdBigProvider{},
		&oisd.OISDNSFW{},
		&phishtank.OnlineValid{},
	}
}

func (p Providers) Process() error {
	for _, provider := range p {
		data, err := provider.Fetch()
		if err != nil {
			return err
		}
		if err := provider.Parse(data); err != nil {
			return err
		}
	}
	return nil
}
