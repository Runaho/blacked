package query

import (
	"blacked/features/entries/enums"
	"blacked/features/entries/services"
	"blacked/features/web/handlers/response"
	"net/http"

	"github.com/labstack/echo/v4"
)

type SearchHandler struct {
	Service *services.QueryService
}

func NewSearchHandler(service *services.QueryService) *SearchHandler {
	return &SearchHandler{Service: service}
}

func (h *SearchHandler) Search(c echo.Context) error {
	req := &SearchInput{}
	if err := c.Bind(req); err != nil {
		return response.BadRequest(c, err.Error())
	}
	if err := c.Validate(req); err != nil {
		return response.BadRequest(c, "Validation error: "+err.Error())
	}

	queryType, err := enums.QueryTypeString(req.QueryType)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	results, err := h.Service.Query(c.Request().Context(), req.URL, &queryType)
	if err != nil {
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Failed to perform query", err.Error())
	}

	resp := NewSearchPayload(results, queryType)
	return response.Success(c, resp)
}
