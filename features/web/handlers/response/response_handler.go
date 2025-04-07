package response

import "github.com/labstack/echo/v4"

// Success returns a standardized success response
func Success(c echo.Context, data any) error {
	return c.JSON(200, map[string]any{
		"success": true,
		"data":    data,
	})
}

// Error returns a standardized error response
func Error(c echo.Context, code int, message string) error {
	return c.JSON(code, map[string]any{
		"success": false,
		"error":   message,
	})
}

// ErrorWithDetails returns an error response with additional details
func ErrorWithDetails(c echo.Context, code int, message string, details any) error {
	return c.JSON(code, map[string]any{
		"success": false,
		"error":   message,
		"details": details,
	})
}

// NotFound returns a standardized not found response
func NotFound(c echo.Context, message string, input string) error {
	return c.JSON(404, map[string]any{
		"success": false,
		"error":   message,
		"input":   input,
	})
}

// BadRequest returns a standardized bad request response
func BadRequest(c echo.Context, message string) error {
	return c.JSON(400, map[string]any{
		"success": false,
		"error":   message,
	})
}
