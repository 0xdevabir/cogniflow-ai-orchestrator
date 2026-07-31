// Package obs owns OpenTelemetry initialization for the orchestrator.
//
// The orchestrator is instrumented with OTel spans for the highest-value
// units of work: the HTTP request, the DAG run, each per-node invocation,
// the fusion stage, and the eval judge. Spans carry attributes that map
// 1:1 to the evaluation fields investors and operators care about
// (model used, tokens, cost, latency, faithfulness).
//
// Default exporter: stdout (a JSON-line span per end). Production users
// set OTEL_EXPORTER_OTLP_ENDPOINT to send to an OTLP collector (Jaeger,
// Honeycomb, Tempo, Datadog, etc.); we auto-detect and switch.
//
// The package is intentionally a thin wrapper so the rest of the codebase
// stays decoupled from the OTel SDK; tests can use the no-op tracer
// without onboarding any of the SDK.
package obs

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tracer is the orchestrator-wide tracer. All call sites grab this rather
// than the global tracer so we can swap providers in tests.
var Tracer trace.Tracer = noop.NewTracerProvider().Tracer("cogniflow")

// Shutdown is the optional flush-and-close callback returned by Init. The
// caller should defer it from main().
type Shutdown func(context.Context) error

// Init configures the global tracer + propagator. exporter is one of:
//   "stdout" — default; pretty-prints spans to stderr (one per end)
//   "otlp"   — gRPC OTLP exporter to OTEL_EXPORTER_OTLP_ENDPOINT
//   "none"   — no-op tracer (lowest cost, no telemetry)
func Init(ctx context.Context, serviceName, exporter string) (Shutdown, error) {
	if exporter == "" {
		exporter = "stdout"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("0.1.0"),
			attribute.String("deployment.environment", envOr("OTEL_ENV", "dev")),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("obs: build resource: %w", err)
	}

	switch exporter {
	case "none":
		Tracer = noop.NewTracerProvider().Tracer(serviceName)
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil

	case "otlp":
		// Allow insecure for local dev; production should set the env var.
		opts := []otlptracegrpc.Option{}
		if os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") != "false" {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exp, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("obs: build OTLP exporter: %w", err)
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp,
				sdktrace.WithBatchTimeout(2*time.Second),
			),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		Tracer = tp.Tracer(serviceName)
		return tp.Shutdown, nil

	case "stdout":
		fallthrough
	default:
		exp, err := stdouttrace.New(
			stdouttrace.WithWriter(stderrOnly{}),
			stdouttrace.WithoutTimestamps(),
		)
		if err != nil {
			return nil, fmt.Errorf("obs: build stdout exporter: %w", err)
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exp),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		Tracer = tp.Tracer(serviceName)
		return tp.Shutdown, nil
	}
}

// Start is a convenience wrapper around Tracer.Start that adds common
// attributes. Use it everywhere instead of Tracer.Start directly so the
// attribute set stays consistent.
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if len(attrs) == 0 {
		return Tracer.Start(ctx, name)
	}
	return Tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// RecordError tags the span with err and sets status=error. Safe to call
// even if err is nil (no-op). Returns err unchanged so callers can `return
// obs.RecordError(span, err)`.
func RecordError(span trace.Span, err error) error {
	if err == nil {
		return nil
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// Attribute shortcuts for the common cases.
func AttrStr(k, v string) attribute.KeyValue { return attribute.String(k, v) }
func AttrInt(k string, v int) attribute.KeyValue {
	return attribute.Int(k, v)
}
func AttrInt64(k string, v int64) attribute.KeyValue {
	return attribute.Int64(k, v)
}
func AttrFloat(k string, v float64) attribute.KeyValue {
	return attribute.Float64(k, v)
}
func AttrBool(k string, v bool) attribute.KeyValue {
	return attribute.Bool(k, v)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// stderrOnly routes the stdout exporter to stderr so it doesn't pollute
// the SSE stream that the API server prints alongside.
type stderrOnly struct{}

func (stderrOnly) Write(b []byte) (int, error) { return os.Stderr.Write(b) }

// Ensure stderrOnly satisfies io.Writer at compile time.
var _ io.Writer = stderrOnly{}
