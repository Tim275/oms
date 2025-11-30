package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

// ResilientConsumer: Auto-restarting consumer that handles channel closures
// Warum?
// → Normale Consumer: Channel stirbt → Consumer stirbt
// → Resilient Consumer: Channel stirbt → Holt neuen Channel → Startet neu!
type ResilientConsumer struct {
	store         StockStore
	logger        *zap.Logger
	resilientConn *broker.ResilientConnection
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewResilientConsumer(
	store StockStore,
	logger *zap.Logger,
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
				rc.logger.Error("failed to get channel, retrying in 5s", zap.Error(err))
				time.Sleep(5 * time.Second)
				continue
			}

			// Starte Consumer auf diesem Channel
			rc.logger.Info("starting consumer on fresh channel")
			if err := rc.consumeOnChannel(ch); err != nil {
				rc.logger.Warn("consumer stopped, restarting...", zap.Error(err))
				// Channel geschlossen → Warte kurz → Hole neuen Channel
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}
}

func (rc *ResilientConsumer) consumeOnChannel(ch *amqp.Channel) error {
	// Queue deklarieren (exclusive queue for this consumer)
	q, err := ch.QueueDeclare(
		"",    // name: auto-generated
		true,  // durable
		false, // delete when unused
		true,  // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return err
	}

	// Queue an Exchange binden
	err = ch.QueueBind(
		q.Name,                // queue name
		"",                    // routing key
		broker.OrderPaidEvent, // exchange
		false,                 // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	rc.logger.Info("stock consumer started (resilient)",
		zap.String("queue", q.Name),
		zap.String("exchange", broker.OrderPaidEvent),
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
			rc.logger.Warn("channel closed, will restart consumer", zap.Error(err))
			return err

		case d, ok := <-msgs:
			if !ok {
				// msgs channel closed
				rc.logger.Warn("msgs channel closed, will restart consumer")
				return nil
			}

			// Process message
			rc.processMessage(d)
		}
	}
}

func (rc *ResilientConsumer) processMessage(d amqp.Delivery) {
	// Extract headers
	ctx := broker.ExtractAMQPHeader(context.Background(), d.Headers)

	// Create a new span - use "stock" tracer so spans appear under stock service in Jaeger
	tr := otel.Tracer("stock")
	_, messageSpan := tr.Start(ctx, fmt.Sprintf("AMQP - consume - %s", broker.OrderPaidEvent))
	defer messageSpan.End()

	traceID := broker.GetTraceIDFromAMQP(d.Headers)
	rc.logger.Info("received order.paid message",
		zap.String("body", string(d.Body)),
		zap.String("trace_id", traceID),
	)

	// Parse order from JSON
	var order pb.Order
	if err := json.Unmarshal(d.Body, &order); err != nil {
		rc.logger.Error("failed to unmarshal order",
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		d.Nack(false, false)
		return
	}

	rc.logger.Info("processing paid order - confirming stock reservation",
		zap.String("order_id", order.Id),
		zap.String("trace_id", traceID),
	)

	// ⭐ Confirm Stock Reservation
	// Warum ConfirmReservation statt DecrementQuantity?
	// → Order wurde bereits bei CreateOrder reserviert (reserved_quantity++)
	// → Jetzt: Payment erfolgreich → Reservation bestätigen!
	// → ConfirmReservation macht:
	//   1. Decrement quantity (actual stock removal)
	//   2. Decrement reserved_quantity (release reservation)
	//   3. Update reservation status = 'confirmed'
	// → Alles in EINER Transaktion - ACID garantiert!
	err := rc.store.ConfirmReservation(ctx, order.Id)
	if err != nil {
		rc.logger.Error("failed to confirm reservation",
			zap.String("order_id", order.Id),
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		// NACK message → goes to DLQ for retry
		d.Nack(false, false)
		rc.logger.Error("reservation confirmation failed - message sent to DLQ",
			zap.String("order_id", order.Id),
			zap.String("trace_id", traceID),
		)
		return
	}

	rc.logger.Info("stock reservation confirmed",
		zap.String("order_id", order.Id),
		zap.Int("items", len(order.Items)),
		zap.String("trace_id", traceID),
	)

	d.Ack(false)
	rc.logger.Info("stock update completed",
		zap.String("order_id", order.Id),
		zap.String("trace_id", traceID),
	)
}

func (rc *ResilientConsumer) Stop() {
	rc.cancel()
}
