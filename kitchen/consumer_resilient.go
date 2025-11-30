package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"

	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

// ResilientConsumer: Auto-restarting consumer that handles channel closures
// Warum?
// → Normale Consumer: Channel stirbt → Consumer stirbt
// → Resilient Consumer: Channel stirbt → Holt neuen Channel → Startet neu!
type ResilientConsumer struct {
	gateway       Gateway
	logger        *slog.Logger
	resilientConn *broker.ResilientConnection
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewResilientConsumer(
	gateway Gateway,
	logger *slog.Logger,
	resilientConn *broker.ResilientConnection,
) *ResilientConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ResilientConsumer{
		gateway:       gateway,
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
			rc.logger.Info("consumer stopped",
				slog.String("service", "kitchen"),
			)
			return
		default:
			// Hole frischen Channel von ResilientConnection
			ch, err := rc.resilientConn.Channel()
			if err != nil {
				rc.logger.Error("failed to get channel, retrying in 5s",
					slog.String("service", "kitchen"),
					slog.Any("error", err),
				)
				time.Sleep(5 * time.Second)
				continue
			}

			// Starte Consumer auf diesem Channel
			rc.logger.Info("starting consumer on fresh channel",
				slog.String("service", "kitchen"),
			)
			if err := rc.consumeOnChannel(ch); err != nil {
				rc.logger.Warn("consumer stopped, restarting...",
					slog.String("service", "kitchen"),
					slog.Any("error", err),
				)
				// Channel geschlossen → Warte kurz → Hole neuen Channel
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}
}

func (rc *ResilientConsumer) consumeOnChannel(ch *amqp.Channel) error {
	// Queue deklarieren - WICHTIG: Eigene Queue für Kitchen (Fan-out Pattern!)
	// Stock und Kitchen brauchen SEPARATE Queues, beide binden an den gleichen Exchange
	// So bekommt JEDER Service JEDE Message (Fan-out), nicht Round-Robin!
	q, err := ch.QueueDeclare(
		"kitchen.order.paid", // queue name - EIGENE Queue für Kitchen!
		true,                 // durable
		false,                // delete when unused
		false,                // exclusive
		false,                // no-wait
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX,
		},
	)
	if err != nil {
		return err
	}

	rc.logger.Info("queue declared",
		slog.String("service", "kitchen"),
		slog.String("queue", q.Name),
	)

	// Queue an Exchange binden
	err = ch.QueueBind(
		q.Name,                // queue name
		"",                    // routing key
		broker.OrderPaidEvent, // exchange name
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		return err
	}

	rc.logger.Info("queue bound to exchange",
		slog.String("service", "kitchen"),
		slog.String("queue", q.Name),
		slog.String("exchange", broker.OrderPaidEvent),
	)

	// Consumer registrieren
	msgs, err := ch.Consume(
		q.Name,
		"",    // consumer tag
		false, // auto-ack: FALSE (manual ack/nack)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	rc.logger.Info("kitchen consumer started (resilient)",
		slog.String("service", "kitchen"),
		slog.String("queue", q.Name),
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
			rc.logger.Warn("channel closed, will restart consumer",
				slog.String("service", "kitchen"),
				slog.Any("error", err),
			)
			return err

		case d, ok := <-msgs:
			if !ok {
				// msgs channel closed
				rc.logger.Warn("msgs channel closed, will restart consumer",
					slog.String("service", "kitchen"),
				)
				return nil
			}

			// Process message
			rc.processMessage(d)
		}
	}
}

func (rc *ResilientConsumer) processMessage(d amqp.Delivery) {
	// 🔍 Extract trace context from AMQP headers (W3C traceparent)
	ctx := broker.ExtractAMQPHeader(context.Background(), d.Headers)

	// 📊 Create a new span for this message processing - use "kitchen" tracer so spans appear under kitchen service in Jaeger
	tr := otel.Tracer("kitchen")
	ctx, messageSpan := tr.Start(ctx, fmt.Sprintf("AMQP - consume - %s", broker.OrderPaidEvent))
	defer messageSpan.End()

	// Get trace ID for logging (trace correlation)
	traceID := broker.GetTraceIDFromAMQP(d.Headers)

	rc.logger.Info("received message",
		slog.String("service", "kitchen"),
		slog.String("event", broker.OrderPaidEvent),
		slog.Int("body_size", len(d.Body)),
		slog.String("trace_id", traceID),
	)

	// Unmarshal order
	var order api.Order
	if err := json.Unmarshal(d.Body, &order); err != nil {
		rc.logger.Error("failed to unmarshal order",
			slog.String("service", "kitchen"),
			slog.Any("error", err),
			slog.String("trace_id", traceID),
		)

		// Get channel for HandleRetry
		ch, err := rc.resilientConn.Channel()
		if err != nil {
			rc.logger.Error("failed to get channel for retry",
				slog.String("service", "kitchen"),
				slog.Any("error", err),
			)
			d.Nack(false, false)
			return
		}

		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("failed to handle retry",
				slog.String("service", "kitchen"),
				slog.Any("error", err),
			)
		}
		d.Nack(false, false)
		return
	}

	rc.logger.Info("order unmarshalled",
		slog.String("service", "kitchen"),
		slog.String("order_id", order.Id),
		slog.String("customer_id", order.CustomerId),
		slog.String("status", order.Status),
		slog.String("trace_id", traceID),
	)

	// Update Order Status zu "preparing"
	if order.Status == "paid" {
		rc.logger.Info("updating order status to preparing",
			slog.String("service", "kitchen"),
			slog.String("order_id", order.Id),
			slog.String("trace_id", traceID),
		)

		// Use ctx with trace context for propagation to Orders service
		err := rc.gateway.UpdateOrder(ctx, &api.Order{
			Id:         order.Id,
			CustomerId: order.CustomerId,
			Status:     "preparing",
		})
		if err != nil {
			rc.logger.Error("failed to update order",
				slog.String("service", "kitchen"),
				slog.String("order_id", order.Id),
				slog.Any("error", err),
			)

			// Get channel for HandleRetry
			ch, err := rc.resilientConn.Channel()
			if err != nil {
				rc.logger.Error("failed to get channel for retry",
					slog.String("service", "kitchen"),
					slog.Any("error", err),
				)
				d.Nack(false, false)
				return
			}

			if err := broker.HandleRetry(ch, &d); err != nil {
				rc.logger.Error("failed to handle retry",
					slog.String("service", "kitchen"),
					slog.Any("error", err),
				)
			}
			d.Nack(false, false)
			return
		}

		rc.logger.Info("order status updated to preparing",
			slog.String("service", "kitchen"),
			slog.String("order_id", order.Id),
			slog.String("trace_id", traceID),
		)
	} else {
		rc.logger.Warn("unexpected order status, skipping",
			slog.String("service", "kitchen"),
			slog.String("order_id", order.Id),
			slog.String("status", order.Status),
			slog.String("trace_id", traceID),
		)
	}

	// Ack message
	if err := d.Ack(false); err != nil {
		rc.logger.Error("failed to ack message",
			slog.String("service", "kitchen"),
			slog.Any("error", err),
			slog.String("trace_id", traceID),
		)
	} else {
		rc.logger.Info("message acknowledged",
			slog.String("service", "kitchen"),
			slog.String("order_id", order.Id),
			slog.String("trace_id", traceID),
		)
	}
}

func (rc *ResilientConsumer) Stop() {
	rc.cancel()
}
