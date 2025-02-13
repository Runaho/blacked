package web

import (
	"blacked/features/web/middlewares"
	"blacked/internal/config"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"github.com/unrolled/secure"
	"github.com/ziflex/lecho/v3"
)

// Global variables (singleton pattern)
var (
	onceApplication sync.Once
	application     *Application

	ErrApplicationNotInitialized = errors.New("Application not initialized")
)

// Application holds our Echo instance, Config, Logger, and Services.
type Application struct {
	Echo     *echo.Echo
	config   *config.ServerConfig
	logger   *lecho.Logger
	services *Services
}

// GetApplication retrieves the singleton instance of Application.
func GetApplication() (*Application, error) {
	if application == nil {
		return nil, ErrApplicationNotInitialized
	}
	return application, nil
}

// NewApplication initializes the Echo server, configures services, and sets up routes.
func NewApplication(cfg *config.ServerConfig) (*Application, error) {
	var initErr error
	onceApplication.Do(func() {
		e := echo.New()
		e.Server.Addr = ":" + strconv.Itoa(cfg.Port)
		log.Info().Str("address", e.Server.Addr).Msg("Server address")

		app := &Application{
			Echo:   e,
			config: cfg,
		}

		app.configureLogger()

		// Initialize all services
		svcs, err := NewServices()
		if err != nil {
			initErr = fmt.Errorf("failed to create services: %w", err)
			log.Error().Err(initErr).Msg("Service initialization error")
			return
		}
		app.services = svcs

		// Configure middlewares
		app.configureMiddleware()

		// Map all routes
		if mapErr := app.ConfigureRoutes(); mapErr != nil {
			initErr = fmt.Errorf("failed to configure routes: %w", mapErr)
			log.Error().Err(initErr).Msg("Routes configuration error")
			return
		}

		application = app
	})

	return application, initErr
}

func (app *Application) configureMiddleware() {
	e := app.Echo

	e.Use(middleware.Recover())
	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Generator: func() string {
			return xid.New().String()
		},
	}))

	secureMiddleware := secure.New(secure.Options{
		FrameDeny:        true,
		BrowserXssFilter: true,
	})

	e.Use(echo.WrapMiddleware(secureMiddleware.Handler))
	e.Use(lecho.Middleware(lecho.Config{Logger: app.logger}))
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     app.config.AllowOrigins,
		AllowCredentials: true,
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderXRequestedWith, echo.HeaderAuthorization},
	}))
	e.Use(lecho.Middleware(lecho.Config{Logger: app.logger}))
	e.Pre(middleware.RemoveTrailingSlash())

	middlewares.ConfigureValidator(e)
}

func (a Application) configureLogger() {
	lechoLogger := lecho.From(log.Logger, lecho.WithTimestamp())
	a.Echo.Logger = lechoLogger
	a.logger = lechoLogger
}
