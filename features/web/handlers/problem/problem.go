package problem

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

// customHTTPErrorHandler intercepts errors from Echo’s pipeline.
func customHTTPErrorHandler(err error, c echo.Context) {
	// If the response has already been committed, do nothing.
	if c.Response().Committed {
		return
	}

	code := http.StatusInternalServerError
	var message interface{}

	if httpErr, ok := err.(*echo.HTTPError); ok {
		code = httpErr.Code
		message = httpErr.Message
	} else {
		message = err.Error()
	}

	switch code {
	case http.StatusNotFound:
		if handleErr := handle404(c); handleErr != nil {
			c.Logger().Error(handleErr)
		}
		return
	default:
		c.Logger().Error(err)
		_ = c.String(code, fmt.Sprintf("%v", message))
	}
}

func MapRoutes(e *echo.Echo) {
	e.HTTPErrorHandler = customHTTPErrorHandler

	e.GET("/404", handle404)
}

func handle404(c echo.Context) error {
	referer := c.QueryParam("referer")
	var referStr *string

	if referer != "" {
		referStr = &referer
	}

	return c.JSON(http.StatusNotFound, map[string]interface{}{
		"error":   "Not Found",
		"message": "The requested resource was not found",
		"referer": referStr,
	})
}
