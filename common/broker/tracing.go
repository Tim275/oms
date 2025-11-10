package broker

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
)

// InjectTraceContext: Inject OpenTelemetry trace context into AMQP message headers
// Warum brauchen wir das?
// → RabbitMQ Messages haben KEINE automatische Trace-Propagation wie gRPC
// → Wir müssen manuell den Trace Context in die Headers packen
// → Damit der Consumer den Trace fortsetzen kann!
//
// Usage:
// headers := broker.InjectTraceContext(ctx)
// ch.PublishWithContext(ctx, "", "queue", false, false, amqp.Publishing{
//     Headers: headers,
//     Body:    []byte(data),
// })
func InjectTraceContext(ctx context.Context) amqp.Table {
	headers := make(amqp.Table)

	// W3C Trace Context Propagator (Standard!)
	propagator := otel.GetTextMapPropagator()

	// Carrier = Adapter zwischen OpenTelemetry und AMQP Headers
	carrier := &AMQPHeadersCarrier{headers: headers}

	// Inject trace context (TraceID, SpanID, etc.) into headers
	propagator.Inject(ctx, carrier)

	return headers
}

// ExtractTraceContext: Extract OpenTelemetry trace context from AMQP message headers
// Warum brauchen wir das?
// → Consumer empfängt Message mit Trace Context in Headers
// → Wir extrahieren den Context und erstellen einen neuen Span
// → Trace wird fortgesetzt! Gateway → Orders → Payment (same trace ID!)
//
// Usage:
// ctx := broker.ExtractTraceContext(context.Background(), msg.Headers)
// tracer := otel.Tracer("payment-service")
// ctx, span := tracer.Start(ctx, "ProcessOrderCreated")
// defer span.End()
func ExtractTraceContext(ctx context.Context, headers amqp.Table) context.Context {
	propagator := otel.GetTextMapPropagator()
	carrier := &AMQPHeadersCarrier{headers: headers}

	// Extract trace context from headers and attach to context
	return propagator.Extract(ctx, carrier)
}

// AMQPHeadersCarrier: Adapter zwischen OpenTelemetry TextMapPropagator und AMQP Headers
// Warum brauchen wir einen Carrier?
// → OpenTelemetry erwartet propagation.TextMapCarrier Interface
// → AMQP nutzt amqp.Table (map[string]interface{})
// → Carrier konvertiert zwischen beiden!
type AMQPHeadersCarrier struct {
	headers amqp.Table
}

// Get: Retrieve a value from AMQP headers (für Extract)
func (c *AMQPHeadersCarrier) Get(key string) string {
	if val, ok := c.headers[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// Set: Store a value in AMQP headers (für Inject)
func (c *AMQPHeadersCarrier) Set(key, value string) {
	c.headers[key] = value
}

// Keys: Return all header keys (für Iteration)
func (c *AMQPHeadersCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for k := range c.headers {
		keys = append(keys, k)
	}
	return keys
}
