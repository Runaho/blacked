package query

import (
	"blacked/features/entries/enums"
	"blacked/features/entries/services"
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
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
	}
	if err := c.Validate(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"validation_error": err.Error()})
	}

	queryType, err := enums.QueryTypeString(req.QueryType)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
	}

	results, err := h.Service.Query(c.Request().Context(), req.URL, &queryType)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
	}

	resp := NewSearchPayload(results, queryType)
	return c.JSON(http.StatusOK, resp)
}
