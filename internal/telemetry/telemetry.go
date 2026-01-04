package telemetry

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/rs/zerolog/log"
)

var (
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *metric.MeterProvider
)

// InitTelemetry initializes OpenTelemetry tracing and metrics
func InitTelemetry(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithContainer(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Initialize tracing
	tracerShutdown, err := initTracing(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tracing: %w", err)
	}

	// Initialize metrics
	metricsShutdown, err := initMetrics(ctx, res)
	if err != nil {
		tracerShutdown(ctx)
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	log.Info().
		Str("service", serviceName).
		Str("version", serviceVersion).
		Msg("OpenTelemetry initialized")

	// Return combined shutdown function
	return func(ctx context.Context) error {
		if err := tracerShutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown tracer")
		}
		if err := metricsShutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown metrics")
		}
		return nil
	}, nil
}

// initTracing initializes the tracing pipeline
func initTracing(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	// Get OTLP endpoint from environment or use default
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4317"
	}

	// Create OTLP trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Create trace provider with batch processor
	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter,
			sdktrace.WithBatchTimeout(time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tracerProvider)

	// Set global propagator for distributed tracing
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	log.Info().
		Str("endpoint", otlpEndpoint).
		Msg("OTLP trace exporter configured")

	return tracerProvider.Shutdown, nil
}

// initMetrics initializes the metrics pipeline
func initMetrics(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	// Create Prometheus exporter
	prometheusExporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}

	// Create meter provider
	meterProvider = metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(prometheusExporter),
	)

	// Set global meter provider
	otel.SetMeterProvider(meterProvider)

	log.Info().Msg("Prometheus metrics exporter configured")

	return meterProvider.Shutdown, nil
}

// GetTracerProvider returns the global tracer provider
func GetTracerProvider() *sdktrace.TracerProvider {
	return tracerProvider
}

// GetMeterProvider returns the global meter provider
func GetMeterProvider() *metric.MeterProvider {
	return meterProvider
}
