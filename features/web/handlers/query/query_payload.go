package query

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
)

type QueryPayload struct {
	URL       string          `json:"url,omitempty"`
	Exists    bool            `json:"exists"`
	Hits      []entries.Hit   `json:"hits"`
	QueryType enums.QueryType `json:"query_type"`
	Count     int             `json:"count"`
}

func NewQueryPayload(hits []entries.Hit, queryType enums.QueryType) *QueryPayload {
	count := len(hits)
	return &QueryPayload{
		Exists:    count > 0,
		Hits:      hits,
		QueryType: queryType,
		Count:     count,
	}
}
