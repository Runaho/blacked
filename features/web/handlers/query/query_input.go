package query

type QueryInput struct {
	URL       string `json:"url" validate:"required"`
	QueryType string `json:"query_type" validate:"omitempty,oneof=full host domain path mixed"`
}
