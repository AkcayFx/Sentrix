package observability

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// GinMiddleware traces incoming HTTP requests and emits request metrics.
func GinMiddleware(runtime *Runtime) gin.HandlerFunc {
	if runtime == nil {
		runtime = newRuntime("sentrix")
	}

	tracer := runtime.Tracer("sentrix.http")

	return func(c *gin.Context) {
		parent := propagation.HeaderCarrier(c.Request.Header)
		ctx := propagation.TraceContext{}.Extract(c.Request.Context(), parent)

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}

		ctx, span := tracer.Start(
			ctx,
			c.Request.Method+" "+route,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		start := time.Now()
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		status := c.Writer.Status()
		duration := time.Since(start)
		route = c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}

		attrs := []attribute.KeyValue{
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.route", route),
			attribute.String("http.target", c.Request.URL.Path),
			attribute.Int("http.status_code", status),
		}

		span.SetAttributes(attrs...)
		span.SetAttributes(attribute.Int64("http.duration_ms", duration.Milliseconds()))

		if len(c.Errors) > 0 {
			lastErr := c.Errors.Last()
			span.RecordError(lastErr)
			span.SetStatus(codes.Error, lastErr.Error())
		} else if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		runtime.HTTPCounter().Add(ctx, 1, metric.WithAttributes(attrs...))
		runtime.HTTPLatency().Record(ctx, float64(duration.Milliseconds()), metric.WithAttributes(attrs...))
		span.End()
	}
}
