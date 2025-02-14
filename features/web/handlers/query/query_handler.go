package query

import (
	"blacked/features/entries/enums"
	"blacked/features/entries/services"
	"net/http"

	"github.com/labstack/echo/v4"
)

type QueryHandler struct {
	Service *services.QueryService
}

func NewQueryHandler(service *services.QueryService) *QueryHandler {
	return &QueryHandler{Service: service}
}

// Query endpoint receives a JSON body, validates, and performs a blacklist query.
func (h *QueryHandler) Query(c echo.Context) error {
	req := &QueryInput{}
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
	}
	if err := c.Validate(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"validation_error": err.Error()})
	}

	// Convert QueryType from string to enum
	queryType, err := enums.QueryTypeString(req.QueryType)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
	}

	// Execute query
	results, err := h.Service.Query(c.Request().Context(), req.URL, &queryType)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
	}

	// Build and return payload
	resp := NewQueryPayload(results, queryType)
	return c.JSON(http.StatusOK, resp)
}
