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
	service        PaymentService
	logger         *slog.Logger
	resilientConn  *broker.ResilientConnection
	ctx            context.Context
	cancel         context.CancelFunc
}

func NewResilientConsumer(
	service PaymentService,
	logger *slog.Logger,
	resilientConn *broker.ResilientConnection,
) *ResilientConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ResilientConsumer{
		service:       service,
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
		broker.OrderCreatedEvent, // queue name
		true,                     // durable
		false,                    // delete when unused
		false,                    // exclusive
		false,                    // no-wait
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX,
		},
	)
	if err != nil {
		return err
	}

	rc.logger.Info("queue declared",
		slog.String("queue", broker.OrderCreatedEvent),
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

	rc.logger.Info("payment consumer started (resilient)",
		slog.String("queue", broker.OrderCreatedEvent),
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
				slog.Any("error", err),
			)
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
	ctx := broker.ExtractTraceContext(rc.ctx, d.Headers)
	tracer := otel.Tracer("payment")
	ctx, span := tracer.Start(ctx, "AMQP - consume - order.created")
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

	// Test case: Deliberately fail for DLQ testing
	if o.CustomerId == "FAIL_TEST" {
		rc.logger.Warn("deliberately failing payment for DLQ test",
			slog.String("customer_id", o.CustomerId),
			slog.String("order_id", o.Id),
		)
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false)
		return
	}

	// Create payment
	paymentLink, err := rc.service.CreatePayment(ctx, o)
	if err != nil {
		rc.logger.Error("failed to create payment", slog.Any("error", err))
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false)
		return
	}

	// Success!
	d.Ack(false)
	rc.logger.Info("payment link created",
		slog.String("payment_link", paymentLink),
		slog.String("order_id", o.Id),
	)
}

func (rc *ResilientConsumer) Stop() {
	rc.cancel()
}
