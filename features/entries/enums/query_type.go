package enums

//go:generate go run github.com/dmarkham/enumer -type=QueryType -trimprefix=QueryType -transform=snake-upper -json -sql -values
type QueryType int

// Query type enums.
const (
	QueryTypeFull QueryType = iota
	QueryTypeHost
	QueryTypeDomain
	QueryTypePath
	QueryTypeMixed
)
