package query

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *SearchHandler) QueryByID(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error": "Entry ID is required",
		})
	}

	entry, err := h.Service.GetEntryByID(c.Request().Context(), id)
	if err != nil {
		if err.Error() == "entry not found" {
			return c.JSON(http.StatusNotFound, map[string]any{
				"error": "Entry not found",
				"id":    id,
			})
		}
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"error":   "Failed to retrieve entry",
			"details": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, entry)
}
