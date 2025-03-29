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
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error":   "Failed to bind URL",
			"details": err.Error(),
		})
	}

	if err := c.Validate(input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"validation_error": err.Error(),
		})
	}

	log.Debug().Str("url", input.URL).Msg("Searching for entry")

	entry, err := cache.SearchBlacklistEntryStream(input.URL)

	if err == cache.ErrBloomKeyNotFound || err == badger.ErrKeyNotFound {
		log.Debug().Str("url", input.URL).Msg("Entry not found")
		errorMap := map[string]any{
			"error": "",
			"url":   input.URL,
		}

		if err == cache.ErrBloomKeyNotFound {
			errorMap["error"] = "Entry is not likely to be found"
		} else {
			errorMap["error"] = "Entry not found"
		}

		return c.JSON(http.StatusNotFound, errorMap)
	} else if err != nil {
		log.Error().Err(err).Str("url", input.URL).Msg("Failed to search for entry")

		return c.JSON(http.StatusInternalServerError, map[string]any{
			"error":   "Failed to retrieve entry",
			"details": err.Error(),
		})
	}

	foundEntry := NewEntryFound(entry)
	log.Debug().Str("url", input.URL).Msg("Entry found")

	return c.JSON(http.StatusOK, foundEntry)
}
