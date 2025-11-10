package main

import (
	"context"
	"encoding/json"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

type consumer struct {
	service PaymentService
	logger  *slog.Logger
}

func NewConsumer(service PaymentService, logger *slog.Logger) *consumer {
	return &consumer{
		service: service,
		logger:  logger,
	}
}

// Listen: Startet RabbitMQ Consumer (wartet auf Events)
// Warum Listen?
// → Payment Service ist PASSIV: Wartet auf "order.created" Events
// → Orders Service ist AKTIV: Published Events
func (c *consumer) Listen(ch *amqp.Channel) {
	// Warum QueueDeclare?
	// → Erstellt Queue für order.created events
	// → DLX + DLQs werden automatisch in broker.Connect() erstellt!
	// → x-dead-letter-exchange: Failed messages → DLX → order.created.dlq
	q, err := ch.QueueDeclare(
		broker.OrderCreatedEvent, // queue name: "order.created"
		true,  // durable: Überlebt RabbitMQ Restart
		false, // delete when unused: NEIN
		false, // exclusive: Andere Consumer können auch lesen
		false, // no-wait
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX, // ⭐ DLX Integration! Failed messages → "dlx" exchange
		},
	)
	if err != nil {
		c.logger.Error("failed to declare queue", slog.Any("error", err))
		return
	}
	c.logger.Info("queue declared",
		slog.String("queue", broker.OrderCreatedEvent),
	)

	c.logger.Info("payment consumer started",
		slog.String("queue", broker.OrderCreatedEvent),
	)

	// Warum ch.Consume?
	// → Registriert diesen Service als CONSUMER für Queue "order.created"
	// → Gibt Channel zurück: Empfängt Messages als Go Channel!
	msgs, err := ch.Consume(
		q.Name, // queue: "order.created"
		"",     // consumer tag: "" = Auto-generiert
		false,  // auto-ack: FALSE! (Wichtig für DLQ!) → Manuelles Ack/Nack
		false,  // exclusive: Andere Consumer können auch lesen (Load Balancing!)
		false,  // no-local: Irrelevant (RabbitMQ Feature)
		false,  // no-wait: Warte auf Server Bestätigung
		nil,    // args: Keine extra Config
	)
	if err != nil {
		c.logger.Error("failed to start consuming", slog.Any("error", err))
		return
	}

	// Warum var forever chan struct{}?
	// → Uninitialisierter Channel = blockiert EWIG bei <-forever
	// → Verhindert dass Listen() returnt (Consumer soll IMMER laufen!)
	var forever chan struct{}

	// Warum Goroutine?
	// → for d := range msgs blockiert!
	// → In Goroutine: Haupt-Thread kann weiter (für Shutdown Handling)
	go func() {
		// Warum for d := range msgs?
		// → Wartet auf neue Messages von RabbitMQ
		// → Blockiert bis Message kommt!
		// → d = Delivery (RabbitMQ Message mit Body, Headers, etc.)
		for d := range msgs {
			// ⭐ OpenTelemetry: Extract trace context from AMQP headers FIRST
			// → Must be done before any processing to continue distributed trace
			ctx := broker.ExtractTraceContext(context.Background(), d.Headers)

			// ⭐ OpenTelemetry: Start span for message processing
			// → This span represents the consumer processing the message
			// → Will be visible in Jaeger as "AMQP - consume - order.created"
			tracer := otel.Tracer("payment")
			ctx, span := tracer.Start(ctx, "AMQP - consume - order.created")

			c.logger.Info("received message",
				slog.String("body", string(d.Body)),
			)

			// Warum json.Unmarshal?
			// → d.Body ist []byte (JSON)
			// → Konvertiert zurück zu *pb.Order struct
			// → GLEICHE Order die Orders Service published hat!
			o := &pb.Order{}
			if err := json.Unmarshal(d.Body, o); err != nil {
				c.logger.Error("failed to unmarshal order", slog.Any("error", err))
				// Warum HandleRetry?
				// → Smart retry: Will retry up to 3 times
				// → After 3 retries → sends to DLQ
				if err := broker.HandleRetry(ch, &d); err != nil {
					c.logger.Error("error handling retry", slog.Any("error", err))
				}
				// Warum Nack nach HandleRetry?
				// → Acknowledges THIS message (already republished by HandleRetry)
				// → Prevents double processing
				d.Nack(false, false)
				span.End() // ⭐ End span before continue!
				continue
			}

			// 🧪 TEST: Deliberately fail payments for testing DLQ
			// Warum dieser Test?
			// → Zum Testen ob DLQ funktioniert!
			// → Order mit CustomerID "FAIL_TEST" → wird 3x retried → dann DLQ
			// → In RabbitMQ UI: Message sollte nach 3 retries in "dlq_main" erscheinen
			if o.CustomerId == "FAIL_TEST" {
				c.logger.Warn("deliberately failing payment for DLQ test",
					slog.String("customer_id", o.CustomerId),
					slog.String("order_id", o.Id),
				)
				// Warum HandleRetry + Nack?
				// → HandleRetry: Manages retry logic and DLQ routing
				// → Nack: Acknowledges this delivery
				if err := broker.HandleRetry(ch, &d); err != nil {
					c.logger.Error("error handling retry", slog.Any("error", err))
				}
				d.Nack(false, false)
				span.End() // ⭐ End span before continue!
				continue
			}

			// Warum service.CreatePayment?
			// → Business Logic: Erstellt Stripe Payment Link
			// → Siehe service.go für Details
			// → Bekommt ctx mit Trace Context (für weitere Propagation!)
			paymentLink, err := c.service.CreatePayment(ctx, o)
			if err != nil {
				c.logger.Error("failed to create payment", slog.Any("error", err))
				// Warum HandleRetry bei Payment Failure?
				// → Stripe API down? → Retry up to 3 times with backoff
				// → After 3 retries → DLQ for manual investigation
				// → Invalid Data? → Will fail 3 times → DLQ for debugging
				if err := broker.HandleRetry(ch, &d); err != nil {
					c.logger.Error("error handling retry", slog.Any("error", err))
				}
				d.Nack(false, false)
				span.End() // ⭐ End span before continue!
				continue
			}

			// ✅ SUCCESS: Payment Link erstellt!
			// Warum d.Ack?
			// → Bestätigt RabbitMQ: "Message erfolgreich verarbeitet"
			// → Message wird aus Queue GELÖSCHT
			// → Arg (multiple=false): Nur DIESE Message acknowledgen
			d.Ack(false)

			c.logger.Info("payment link created",
				slog.String("payment_link", paymentLink),
				slog.String("order_id", o.Id),
			)

			// ⭐ End span after successful processing
			span.End()
		}
	}()

	c.logger.Info("waiting for messages...",
		slog.String("queue", broker.OrderCreatedEvent),
	)

	// Warum <-forever?
	// → Blockiert EWIG (forever = nil Channel)
	// → Listen() returnt NIE (Consumer läuft bis Process killed wird)
	// → Wichtig: Sonst würde Listen() sofort returnen!
	<-forever
}
