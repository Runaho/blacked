package query

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
)

type SearchPayload struct {
	URL       string          `json:"url,omitempty"`
	Exists    bool            `json:"exists"`
	Hits      []entries.Hit   `json:"hits"`
	QueryType enums.QueryType `json:"query_type"`
	Count     int             `json:"count"`
}

func NewSearchPayload(hits []entries.Hit, queryType enums.QueryType) *SearchPayload {
	count := len(hits)
	return &SearchPayload{
		Exists:    count > 0,
		Hits:      hits,
		QueryType: queryType,
		Count:     count,
	}
}
