package entries

type EntryStream struct {
	SourceUrl string   `json:"raw_query"`
	IDs       []string `json:"ids"`
	IDsRaw    string   `json:"ids_value"`
}
