package query

import (
	"blacked/features/cache"
	"blacked/features/cache/cache_errors"
	"blacked/features/entries"
	"blacked/features/web/handlers/response"
	"net/http"

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
	input := new(URLQueryInput)
	if err := c.Bind(input); err != nil {
		return response.BadRequest(c, "Failed to bind URL")
	}

	if err := c.Validate(input); err != nil {
		return response.BadRequest(c, "Validation failed")
	}

	log.Debug().Str("url", input.URL).Msg("Searching for entry")

	entry, err := cache.GetEntryStream(input.URL)

	if err == cache.ErrBloomKeyNotFound || err == cache_errors.ErrKeyNotFound {
		log.Debug().Str("url", input.URL).Msg("Entry not found")
		var entryError string

		if err == cache.ErrBloomKeyNotFound {
			entryError = "Entry is not likely to be found"
		} else {
			entryError = "Entry not found"
		}

		return response.NotFound(c, entryError, input.URL)
	} else if err != nil {
		log.Error().Err(err).Str("url", input.URL).Msg("Failed to search for entry")

		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Failed to retrieve entry", err.Error())
	}

	foundEntry := NewEntryFound(entry)
	log.Debug().Str("url", input.URL).Msg("Entry found")

	return c.JSON(http.StatusOK, foundEntry)
}
