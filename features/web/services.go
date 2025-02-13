package web

import "blacked/features/entries/services"

type Services struct {
	EntryQueryService *services.QueryService
}

func NewServices() (*Services, error) {
	queryService, err := services.NewQueryService()
	if err != nil {
		return nil, err
	}

	return &Services{
		EntryQueryService: queryService,
	}, nil
}
