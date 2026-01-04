package web

import (
	"blacked/features/providers"
	"blacked/features/web/middlewares"
	"blacked/internal/collector"
	"blacked/internal/config"
	"errors"
	"strconv"
	"sync"

	"net/http"
	"net/http/pprof"
	rpprof "runtime/pprof"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	promhttp "github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"github.com/unrolled/secure"
	"github.com/ziflex/lecho/v3"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

// Application errors
var (
	ErrApplicationNotInitialized = errors.New("application not initialized")
	ErrServiceInitFailed         = errors.New("services initialization failed")
	ErrRoutesMapFailed           = errors.New("routes configuration failed")
	ErrMetricCollectorFailed     = errors.New("metric collector configuration failed")
)

// Global variables (singleton pattern)
var (
	onceApplication sync.Once
	application     *Application
)

// Application holds our Echo instance, Config, Logger, and Services.
type Application struct {
	Echo      *echo.Echo
	config    *config.ServerConfig
	logger    *lecho.Logger
	services  *Services
	providers *providers.Providers
}

func (app *Application) GetProviders() *providers.Providers {
	return app.providers
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
			log.Err(err).Msg("Service initialization error")
			initErr = ErrServiceInitFailed
			return
		}
		app.services = svcs

		// Configure middlewares
		app.configureMiddleware()

		// Map all routes
		if mapErr := app.ConfigureRoutes(); mapErr != nil {
			log.Err(mapErr).Msg("Routes configuration error")
			initErr = ErrRoutesMapFailed
			return
		}

		app.ConfigurePprof()

		app.providers = providers.GetProviders()

		if err := app.configureMetricCollector(); err != nil {
			log.Err(err).Msg("Metric collector configuration error")
			initErr = ErrMetricCollectorFailed
			return
		}

		application = app
	})

	return application, initErr
}

func (app *Application) configureMetricCollector() error {
	collector.NewMetricsCollector(app.providers.GetNames())

	mc, err := collector.GetMetricsCollector()
	if err != nil {
		log.Err(err).Msg("Failed to get metrics collector")
		return err
	}

	mc.ExposeWebMetrics(app.Echo)

	// Add OpenTelemetry Prometheus metrics endpoint
	// The metrics are exposed via the global MeterProvider automatically
	app.Echo.GET("/otel-metrics", echo.WrapHandler(promhttp.Handler()))
	log.Info().Msg("OpenTelemetry metrics endpoint configured at /otel-metrics")

	return nil
}

func (app *Application) configureMiddleware() {
	e := app.Echo

	e.Use(middleware.Recover())
	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Generator: func() string {
			return xid.New().String()
		},
	}))

	// Add OpenTelemetry tracing middleware
	e.Use(otelecho.Middleware("blacked"))

	// Add Echo Prometheus metrics middleware (use "echo" prefix for dashboard compatibility)
	e.Use(echoprometheus.NewMiddleware("echo"))

	secureMiddleware := secure.New(secure.Options{
		FrameDeny:        true,
		BrowserXssFilter: true,
	})
	e.Use(echo.WrapMiddleware(secureMiddleware.Handler))

	e.Use(lecho.Middleware(lecho.Config{Logger: app.logger}))
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     app.config.AllowOrigins,
		AllowCredentials: true,
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderXRequestedWith,
			echo.HeaderAuthorization,
		},
	}))

	e.Use(middlewares.RequestLogger())
	e.Pre(middleware.RemoveTrailingSlash())

	middlewares.ConfigureValidator(e)
}

func (a Application) configureLogger() {
	lechoLogger := lecho.From(log.Logger, lecho.WithTimestamp())
	a.Echo.Logger = lechoLogger
	a.logger = lechoLogger
}

func (app *Application) ConfigurePprof() {
	pprofGroup := app.Echo.Group("/debug/pprof")

	// Index page
	pprofGroup.GET("", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	pprofGroup.GET("/", echo.WrapHandler(http.HandlerFunc(pprof.Index)))

	// Individual profiles - these match the standard pprof endpoints
	pprofGroup.GET("/cmdline", echo.WrapHandler(http.HandlerFunc(pprof.Cmdline)))
	pprofGroup.GET("/profile", echo.WrapHandler(http.HandlerFunc(pprof.Profile)))
	pprofGroup.GET("/symbol", echo.WrapHandler(http.HandlerFunc(pprof.Symbol)))
	pprofGroup.GET("/trace", echo.WrapHandler(http.HandlerFunc(pprof.Trace)))

	for _, profile := range rpprof.Profiles() {
		name := profile.Name()
		pprofGroup.GET("/"+name, echo.WrapHandler(pprof.Handler(name)))
	}
}
