package query

import (
	"blacked/features/web/handlers/response"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *SearchHandler) QueryByID(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return response.BadRequest(c, "Entry ID is required")
	}

	entry, err := h.Service.GetEntryByID(c.Request().Context(), id)
	if err != nil {
		if err.Error() == "entry not found" {
			return response.NotFound(c, "Entry not found", id)
		}
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Failed to retrieve entry", err.Error())
	}

	if entry == nil {
		return response.NotFound(c, "Entry not found", id)
	}

	return response.Success(c, entry)
}
