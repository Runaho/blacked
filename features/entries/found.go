package entries

import "blacked/features/entries/enums"

type URLQueryResponse struct {
	URL       string          `json:"url"`
	Exists    bool            `json:"exists"`
	Hits      []Hit           `json:"hits"`
	QueryType enums.QueryType `json:"query_type"`
	Count     int             `json:"count"`
}

func NewURLQueryResponse(url string, hits []Hit, queryType enums.QueryType, verbose bool) *URLQueryResponse {
	count := len(hits)

	uqr := &URLQueryResponse{
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

type Hit struct {
	ID           string `json:"id"`
	MatchType    string `json:"match_type"`
	MatchedValue string `json:"matched_value"`
}
