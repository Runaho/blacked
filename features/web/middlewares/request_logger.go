package middlewares

import (
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// RequestLogger middleware logs information about each incoming HTTP request and its response
func RequestLogger() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Generate request ID for tracing
			requestID := uuid.New().String()
			c.Set("request_id", requestID)

			// Add request ID to response header for client-side debugging
			c.Response().Header().Set("X-Request-ID", requestID)

			req := c.Request()
			start := time.Now()

			// Create context with request data
			logCtx := log.With().
				Str("request_id", requestID).
				Str("method", req.Method).
				Str("path", req.URL.Path).
				Str("remote_ip", c.RealIP())

			// Log the incoming request (create logger from context)
			logCtx.Str("query", req.URL.RawQuery).
				Str("user_agent", req.UserAgent())

			// Process the request
			err := next(c)

			// Get response status
			status := c.Response().Status
			latency := time.Since(start)

			// Complete the request log with response information
			respLogCtx := logCtx.
				Int("status", status).
				Dur("latency", latency).
				Str("bytes_out", formatByteCount(c.Response().Size))

			// Create logger from context
			respLogger := respLogCtx.Logger()

			// Add error information if present
			if err != nil {
				respLogger.Error().Err(err).Msg("Request failed")
			} else {
				// Log based on status code category
				switch {
				case status >= 500:
					respLogger.Error().Msg("Server error")
				case status >= 400:
					respLogger.Warn().Msg("Client error")
				case status >= 300:
					respLogger.Debug().Msg("Redirection")
				default:
					respLogger.Debug().Msg("Request completed")
				}
			}

			return err
		}
	}
}

// formatByteCount formats the byte count for logging
func formatByteCount(bytes int64) string {
	// Return "-" for zero bytes
	if bytes == 0 {
		return "-"
	}
	return humanizeBytes(bytes)
}

// humanizeBytes converts bytes to human readable format
func humanizeBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return strconv.FormatInt(bytes, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatInt(bytes/div, 10) + " " + string("KMGTPE"[exp]) + "B"
}
