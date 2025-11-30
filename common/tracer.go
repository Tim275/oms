package common

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// SetGlobalTracer: Initialisiert OpenTelemetry Tracing für einen Service
// Uses gRPC exporter (Port 4317) - compatible with Platform ConfigMap
func SetGlobalTracer(ctx context.Context, serviceName, exporterEndpoint string) error {
	// gRPC connection to OTEL Collector
	conn, err := grpc.NewClient(exporterEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return err
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return nil
}

// GetTraceID: Extrahiert Trace ID aus Context für Logging
// Warum?
// → Logs mit Trace IDs können in Jaeger nachgeschaut werden
// → Correlation zwischen Logs und Traces!
func GetTraceID(ctx context.Context) string {
	span := oteltrace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// GetSpanID: Extrahiert Span ID aus Context für Logging
func GetSpanID(ctx context.Context) string {
	span := oteltrace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		return span.SpanContext().SpanID().String()
	}
	return ""
}
