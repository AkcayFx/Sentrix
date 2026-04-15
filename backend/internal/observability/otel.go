package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/yourorg/sentrix/internal/config"
)

const serviceVersion = "0.4.0"

// Runtime owns the app-wide telemetry providers and instruments.
type Runtime struct {
	enabled         bool
	serviceName     string
	httpCounter     metric.Int64Counter
	httpLatency     metric.Float64Histogram
	shutdownTargets []func(context.Context) error
}

// Init configures global OpenTelemetry providers and returns a runtime facade.
// When observability is disabled or setup fails, it falls back to a no-op runtime.
func Init(ctx context.Context, cfg config.ObservabilityConfig) (*Runtime, error) {
	rt := newRuntime(cfg.ServiceName)
	if !cfg.Enabled {
		return rt, nil
	}

	endpoint := strings.TrimSpace(cfg.OTLPEndpoint)
	if endpoint == "" {
		return rt, fmt.Errorf("observability: OTLP endpoint is required when enabled")
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(rt.serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return rt, fmt.Errorf("observability: build resource: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return rt, fmt.Errorf("observability: trace exporter: %w", err)
	}

	metricExporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return rt, fmt.Errorf("observability: metric exporter: %w", err)
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(clampSampleRate(cfg.TraceSampleRate))),
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	metricProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(metricProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	rt.enabled = true
	rt.shutdownTargets = []func(context.Context) error{
		traceProvider.Shutdown,
		metricProvider.Shutdown,
	}
	rt.rebuildInstruments()

	log.WithFields(log.Fields{
		"endpoint":     endpoint,
		"service_name": rt.serviceName,
		"sample_rate":  clampSampleRate(cfg.TraceSampleRate),
	}).Info("observability: OpenTelemetry enabled")

	return rt, nil
}

func newRuntime(serviceName string) *Runtime {
	rt := &Runtime{
		serviceName: strings.TrimSpace(serviceName),
	}
	if rt.serviceName == "" {
		rt.serviceName = "sentrix"
	}
	rt.rebuildInstruments()
	return rt
}

func (r *Runtime) rebuildInstruments() {
	meter := otel.Meter(r.serviceName)

	httpCounter, err := meter.Int64Counter(
		"sentrix.http.requests_total",
		metric.WithDescription("Total number of HTTP requests handled by Sentrix."),
	)
	if err != nil {
		log.Warnf("observability: create http counter: %v", err)
	}

	httpLatency, err := meter.Float64Histogram(
		"sentrix.http.request.duration_ms",
		metric.WithDescription("End-to-end HTTP request latency in milliseconds."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		log.Warnf("observability: create http latency histogram: %v", err)
	}

	r.httpCounter = httpCounter
	r.httpLatency = httpLatency
}

// Enabled reports whether real telemetry export is active.
func (r *Runtime) Enabled() bool {
	if r == nil {
		return false
	}
	return r.enabled
}

// Tracer returns a tracer from the global provider.
func (r *Runtime) Tracer(name string) trace.Tracer {
	if name == "" {
		name = r.serviceName
	}
	return otel.Tracer(name)
}

// HTTPCounter returns the HTTP request counter instrument.
func (r *Runtime) HTTPCounter() metric.Int64Counter {
	return r.httpCounter
}

// HTTPLatency returns the HTTP request latency histogram.
func (r *Runtime) HTTPLatency() metric.Float64Histogram {
	return r.httpLatency
}

// Shutdown flushes and closes telemetry providers.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	var errs []string
	for i := len(r.shutdownTargets) - 1; i >= 0; i-- {
		if err := r.shutdownTargets[i](ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func clampSampleRate(v float64) float64 {
	switch {
	case v <= 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
