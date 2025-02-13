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

	// If the error is an Echo HTTP error, preserve the code/message.
	if httpErr, ok := err.(*echo.HTTPError); ok {
		code = httpErr.Code
		message = httpErr.Message
	} else {
		message = err.Error()
	}

	// Example: handling a 404 with a custom route
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

// MapRoutes sets the custom error handler and any relevant routes for error scenarios.
func MapRoutes(e *echo.Echo) {
	e.HTTPErrorHandler = customHTTPErrorHandler

	// A quick route to simulate a 404
	e.GET("/404", handle404)
}

// handle404 is your custom 404 route.
func handle404(c echo.Context) error {
	referer := c.QueryParam("referer")
	var referStr *string

	if referer != "" {
		// Some logic to detect if it’s valid, etc.
		referStr = &referer
	}

	return c.JSON(http.StatusNotFound, map[string]interface{}{
		"error":   "Not Found",
		"message": "The requested resource was not found",
		"referer": referStr,
	})
}
