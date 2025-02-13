package entries

import "blacked/features/entries/enums"

type QueryResponse struct {
	URL       string          `json:"url"`
	Exists    bool            `json:"exists"`
	Hits      []Hit           `json:"hits"`
	QueryType enums.QueryType `json:"query_type"`
	Count     int             `json:"count"`
}

func NewQueryResponse(url string, hits []Hit, queryType enums.QueryType, verbose bool) *QueryResponse {
	count := len(hits)

	uqr := &QueryResponse{
		URL:       url,
		Exists:    count > 0,
		QueryType: queryType,
		Count:     count,
	}

	if verbose {
		uqr.Hits = hits
	} else {
		if count > 0 {
			uqr.Hits = []Hit{hits[0]}
		}
	}

	return uqr
}
