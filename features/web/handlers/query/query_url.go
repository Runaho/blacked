package query

import (
	"blacked/features/cache"
	"blacked/features/entries"
	"net/http"

	"github.com/dgraph-io/badger/v4"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

type EntryFound struct {
	URL    string   `json:"url"`
	Exists bool     `json:"exists"`
	Count  int      `json:"count"`
	IDs    []string `json:"ids"`
}

func NewEntryFound(entryStream entries.EntryStream) EntryFound {
	return EntryFound{
		URL:    entryStream.SourceUrl,
		Exists: true,
		Count:  len(entryStream.IDs),
		IDs:    entryStream.IDs,
	}
}

type URLQueryInput struct {
	URL string `param:"url" query:"url" form:"url" json:"url" xml:"url" validate:"required"`
}

func (h *SearchHandler) QueryByURL(c echo.Context) error {
	// Get URL from query parameter
	input := new(URLQueryInput)
	if err := c.Bind(input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error":   "Failed to bind URL",
			"details": err.Error(),
		})
	}

	if err := c.Validate(input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"validation_error": err.Error(),
		})
	}

	log.Debug().Str("url", input.URL).Msg("Searching for entry")

	entry, err := cache.SearchBlacklistEntryStream(input.URL)
	if err != nil {
		log.Error().Err(err).Str("url", input.URL).Msg("Failed to search for entry")
		return c.JSON(http.StatusNotFound, map[string]interface{}{
			"error":   "Failed to retrieve entry",
			"details": err.Error(),
		})
	}

	if err == badger.ErrKeyNotFound {
		log.Debug().Str("url", input.URL).Msg("Entry not found")
		return c.JSON(http.StatusNotFound, map[string]interface{}{
			"error": "Entry not found",
			"url":   input.URL,
		})
	}
	foundEntry := NewEntryFound(entry)
	log.Debug().Str("url", input.URL).Msg("Entry found")

	return c.JSON(http.StatusOK, foundEntry)
}
