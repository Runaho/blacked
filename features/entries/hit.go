package entries

type Hit struct {
	ID           string `json:"id"`
	MatchType    string `json:"match_type"`
	MatchedValue string `json:"matched_value"`
}
