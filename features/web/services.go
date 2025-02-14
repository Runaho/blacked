package web

import (
	"blacked/features/entries/services"
	provider_processor "blacked/features/providers/services"
)

type Services struct {
	EntryQueryService      *services.QueryService
	ProviderProcessService *provider_processor.ProviderProcessService
}

func NewServices() (*Services, error) {
	queryService, err := services.NewQueryService()
	if err != nil {
		return nil, err
	}

	providerProcessService, err := provider_processor.NewProviderProcessService()
	if err != nil {
		return nil, err
	}

	return &Services{
		EntryQueryService:      queryService,
		ProviderProcessService: providerProcessService,
	}, nil
}
