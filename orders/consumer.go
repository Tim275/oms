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
	store  OrdersStore
	logger *slog.Logger
}

func NewConsumer(store OrdersStore, logger *slog.Logger) *consumer {
	return &consumer{
		store:  store,
		logger: logger,
	}
}

// Listen: Startet RabbitMQ Consumer für order.paid Events
// Warum Listen?
// → Orders Service empfängt "order.paid" von Payment Service
// → Updated Order mit payment_link + status "waiting_payment"
// → Event-Driven Architecture statt gRPC!
func (c *consumer) Listen(ch *amqp.Channel) {
	// Warum QueueDeclare?
	// → Erstellt Queue für order.paid events
	// → Payment Service published hier rein!
	// Warum QueueDeclare?
	// → Erstellt Queue "order.paid" (falls nicht existiert)
	// → Durable: Queue überlebt RabbitMQ Restart
	// → x-dead-letter-exchange: Failed messages → DLX → order.paid.dlq
	q, err := ch.QueueDeclare(
		broker.OrderPaidEvent, // queue name: "order.paid"
		true,                  // durable: Überlebt RabbitMQ Restart
		false,                 // delete when unused: NEIN
		false,                 // exclusive: Andere Consumer können auch lesen
		false,                 // no-wait
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX, // ⭐ DLX Integration! Failed messages → "dlx" exchange
		},
	)
	if err != nil {
		c.logger.Error("failed to declare queue", slog.Any("error", err))
		return
	}
	c.logger.Info("queue declared",
		slog.String("queue", broker.OrderPaidEvent),
	)

	// ⭐ Warum QueueBind?
	// → Bindet Queue an Exchange "order.paid"
	// → Payment Service published zu Exchange → Messages landen in Queue!
	// → OHNE Bind: Messages gehen verloren!
	err = ch.QueueBind(
		q.Name,                // queue name: "order.paid"
		"",                    // routing key: "" = matches all
		broker.OrderPaidEvent, // exchange name: "order.paid"
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		c.logger.Error("failed to bind queue to exchange", slog.Any("error", err))
		return
	}
	c.logger.Info("queue bound to exchange",
		slog.String("queue", broker.OrderPaidEvent),
		slog.String("exchange", broker.OrderPaidEvent),
	)

	c.logger.Info("order.paid consumer started",
		slog.String("queue", broker.OrderPaidEvent),
	)

	// Warum ch.Consume?
	// → Registriert diesen Service als CONSUMER für Queue "order.paid"
	// → Gibt Channel zurück: Empfängt Messages als Go Channel!
	msgs, err := ch.Consume(
		q.Name, // queue: "order.paid"
		"",     // consumer tag: "" = Auto-generiert
		false,  // auto-ack: FALSE! (Wichtig für DLQ!) → Manuelles Ack/Nack
		false,  // exclusive: Andere Consumer können auch lesen
		false,  // no-local
		false,  // no-wait
		nil,    // args
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
	// → In Goroutine: Haupt-Thread kann weiter (für gRPC Server!)
	go func() {
		// Warum for d := range msgs?
		// → Wartet auf neue Messages von RabbitMQ
		// → Blockiert bis Message kommt!
		for d := range msgs {
			// ⭐ OpenTelemetry: Extract trace context from AMQP headers FIRST
			// → Must be done before any processing to continue distributed trace
			ctx := broker.ExtractTraceContext(context.Background(), d.Headers)

			// ⭐ OpenTelemetry: Start span for message processing
			// → This span represents the consumer processing the message
			// → Will be visible in Jaeger as "AMQP - consume - order.paid"
			tracer := otel.Tracer("orders")
			ctx, span := tracer.Start(ctx, "AMQP - consume - order.paid")

			c.logger.Info("received message",
				slog.String("body", string(d.Body)),
			)

			// Warum json.Unmarshal?
			// → d.Body ist []byte (JSON)
			// → Konvertiert zurück zu *pb.Order struct
			// → GLEICHE Order die Payment Service published hat!
			o := &pb.Order{}
			if err := json.Unmarshal(d.Body, o); err != nil {
				c.logger.Error("failed to unmarshal order", slog.Any("error", err))
				// Warum HandleRetry?
				// → Smart retry: Will retry up to 3 times
				// → After 3 retries → sends to DLQ
				if err := broker.HandleRetry(ch, &d); err != nil {
					c.logger.Error("error handling retry", slog.Any("error", err))
				}
				d.Nack(false, false)
				span.End() // ⭐ End span before continue!
				continue
			}

			// Warum store.Update?
			// → Business Logic: Updated Order mit payment_link + status
			// → Store wird updated (in-memory)
			err = c.store.Update(ctx, o.Id, o)
			if err != nil {
				c.logger.Error("failed to update order", slog.Any("error", err))
				// Warum HandleRetry bei Update Failure?
				// → Order not found? → Will fail 3 times → DLQ for investigation
				// → Store error? → Retry with backoff
				if err := broker.HandleRetry(ch, &d); err != nil {
					c.logger.Error("error handling retry", slog.Any("error", err))
				}
				d.Nack(false, false)
				span.End() // ⭐ End span before continue!
				continue
			}

			// ✅ SUCCESS: Order updated!
			// Warum d.Ack?
			// → Bestätigt RabbitMQ: "Message erfolgreich verarbeitet"
			// → Message wird aus Queue GELÖSCHT
			d.Ack(false)

			c.logger.Info("updating order",
				slog.String("order_id", o.Id),
				slog.String("status", o.Status),
				slog.String("payment_link", o.PaymentLink),
			)
			c.logger.Info("order updated successfully",
				slog.String("order_id", o.Id),
				slog.String("status", o.Status),
			)

			// ⭐ End span after successful processing
			span.End()
		}
	}()

	c.logger.Info("waiting for messages...",
		slog.String("queue", broker.OrderPaidEvent),
	)

	// Warum <-forever?
	// → Blockiert EWIG (forever = nil Channel)
	// → Listen() returnt NIE (Consumer läuft bis Process killed wird)
	<-forever
}
// rebuild trigger
