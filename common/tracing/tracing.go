package tracing

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// InitTracer: Initialisiert OpenTelemetry Tracing
// Warum brauchen wir das?
// → Jeder Service ruft InitTracer("gateway") in main.go auf
// → Erstellt TracerProvider mit OTLP Exporter
// → Sendet Traces zu OpenTelemetry Collector
// → Global registriert: discovery.ServiceConnection() nutzt automatisch!
//
// Usage in main.go:
// shutdown, err := tracing.InitTracer("gateway")
// if err != nil { log.Fatal(err) }
// defer shutdown()
func InitTracer(serviceName string) (func(), error) {
	// Warum OTEL_EXPORTER_OTLP_ENDPOINT aus ENV?
	// → Dev: localhost:4317 (local OTel Collector)
	// → Production: otel-collector:4317 (Docker/K8s)
	// → Flexibel: Kein hardcoded endpoint!
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317" // Default für local dev
	}

	log.Printf("Initializing OpenTelemetry tracer for service=%s, endpoint=%s", serviceName, endpoint)

	// Warum context.Background()?
	// → Initialisierung braucht Context (für gRPC Connection zu Collector)
	// → Timeout: Wenn Collector down ist, nicht ewig warten
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Warum otlptracegrpc.New?
	// → OTLP = OpenTelemetry Protocol
	// → gRPC = Schnell, binär, bidirectional
	// → Alternative: otlptracehttp (für HTTP/JSON)
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // Kein TLS in dev (production: WithTLSCredentials)
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Warum resource.NewWithAttributes?
	// → Resource = "Was ist dieser Service?"
	// → service.name: Gateway, Orders, Payments
	// → service.version: v1.0.0 (für Production)
	// → Jaeger UI gruppiert Traces nach Service Name!
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion("v1.0.0"),
	)

	// Warum TracerProvider?
	// → Zentrale Stelle für Tracing Config
	// → Sampler: AlwaysSample() = Trace ALLE requests (dev)
	// → BatchSpanProcessor: Sammelt Spans und sendet in Batches (effizienz!)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Production: ParentBased()
	)

	// Warum otel.SetTracerProvider?
	// → Registriert GLOBAL: discovery/grpc.go nutzt automatisch!
	// → otelgrpc.UnaryClientInterceptor() findet TracerProvider
	// → Kein extra Code nötig in Gateways!
	otel.SetTracerProvider(tp)

	// Warum propagation.TraceContext?
	// → Propagiert Trace ID zwischen Services
	// → HTTP: W3C Trace Context Header
	// → gRPC: Metadata
	// → Flow: Gateway (Trace ID 123) → Orders (Trace ID 123) → Payment (Trace ID 123)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	log.Printf("OpenTelemetry tracer initialized successfully for service=%s", serviceName)

	// Warum Shutdown Function returnen?
	// → main.go: defer shutdown()
	// → Flusht alle pending Spans bevor Service stoppt
	// → Wichtig: Sonst gehen Traces verloren!
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}, nil
}
