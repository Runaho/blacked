package entries

type LinkCheckResponse struct {
	Exists bool  `json:"exists"`
	Hits   []Hit `json:"hits"`
}

type Hit struct {
	ID           string `json:"id"`
	MatchType    string `json:"match_type"`
	MatchedValue string `json:"matched_value"`
}
