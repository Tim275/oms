package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

// ResilientConsumer: Auto-restarting consumer that handles channel closures
// Warum?
// → Normale Consumer: Channel stirbt → Consumer stirbt
// → Resilient Consumer: Channel stirbt → Holt neuen Channel → Startet neu!
type ResilientConsumer struct {
	store         OrdersStore
	logger        *slog.Logger
	resilientConn *broker.ResilientConnection
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewResilientConsumer(
	store OrdersStore,
	logger *slog.Logger,
	resilientConn *broker.ResilientConnection,
) *ResilientConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ResilientConsumer{
		store:         store,
		logger:        logger,
		resilientConn: resilientConn,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start: Startet resilient consumer with auto-restart
// Warum?
// → Überwacht Channel Closures
// → Startet Consumer neu wenn Channel stirbt
// → Verhindert Message Loss!
func (rc *ResilientConsumer) Start() {
	go rc.consume()
}

func (rc *ResilientConsumer) consume() {
	for {
		select {
		case <-rc.ctx.Done():
			rc.logger.Info("consumer stopped")
			return
		default:
			// Hole frischen Channel von ResilientConnection
			ch, err := rc.resilientConn.Channel()
			if err != nil {
				rc.logger.Error("failed to get channel, retrying in 5s", slog.Any("error", err))
				time.Sleep(5 * time.Second)
				continue
			}

			// Starte Consumer auf diesem Channel
			rc.logger.Info("starting consumer on fresh channel")
			if err := rc.consumeOnChannel(ch); err != nil {
				rc.logger.Warn("consumer stopped, restarting...", slog.Any("error", err))
				// Channel geschlossen → Warte kurz → Hole neuen Channel
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}
}

func (rc *ResilientConsumer) consumeOnChannel(ch *amqp.Channel) error {
	// Queue deklarieren
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
		return err
	}

	rc.logger.Info("queue declared",
		slog.String("queue", broker.OrderPaidEvent),
	)

	// Queue an Exchange binden
	err = ch.QueueBind(
		q.Name,                // queue name: "order.paid"
		"",                    // routing key: "" = matches all
		broker.OrderPaidEvent, // exchange name: "order.paid"
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		return err
	}

	rc.logger.Info("queue bound to exchange",
		slog.String("queue", broker.OrderPaidEvent),
		slog.String("exchange", broker.OrderPaidEvent),
	)

	// Consumer registrieren
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
		return err
	}

	rc.logger.Info("order.paid consumer started (resilient)",
		slog.String("queue", broker.OrderPaidEvent),
	)

	// Monitor channel closures
	closeCh := make(chan *amqp.Error)
	ch.NotifyClose(closeCh)

	// Process messages
	for {
		select {
		case <-rc.ctx.Done():
			return nil

		case err := <-closeCh:
			// Channel wurde geschlossen!
			rc.logger.Warn("channel closed, will restart consumer", slog.Any("error", err))
			return err

		case d, ok := <-msgs:
			if !ok {
				// msgs channel closed
				rc.logger.Warn("msgs channel closed, will restart consumer")
				return nil
			}

			// Process message
			rc.processMessage(ch, d)
		}
	}
}

func (rc *ResilientConsumer) processMessage(ch *amqp.Channel, d amqp.Delivery) {
	// Extract trace context
	ctx := broker.ExtractTraceContext(context.Background(), d.Headers)

	// Start span for message processing
	tracer := otel.Tracer("orders")
	ctx, span := tracer.Start(ctx, "AMQP - consume - order.paid")
	defer span.End()

	rc.logger.Info("received message",
		slog.String("body", string(d.Body)),
	)

	// Unmarshal order
	o := &pb.Order{}
	if err := json.Unmarshal(d.Body, o); err != nil {
		rc.logger.Error("failed to unmarshal order", slog.Any("error", err))
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false)
		return
	}

	// Update order in store
	err := rc.store.Update(ctx, o.Id, o)
	if err != nil {
		rc.logger.Error("failed to update order", slog.Any("error", err))
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false)
		return
	}

	// Success!
	d.Ack(false)

	rc.logger.Info("updating order",
		slog.String("order_id", o.Id),
		slog.String("status", o.Status),
		slog.String("payment_link", o.PaymentLink),
	)
	rc.logger.Info("order updated successfully",
		slog.String("order_id", o.Id),
		slog.String("status", o.Status),
	)
}

func (rc *ResilientConsumer) Stop() {
	rc.cancel()
}
