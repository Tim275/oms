package main

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

type Consumer struct {
	store  StockStore
	logger *zap.Logger
}

func NewConsumer(store StockStore, logger *zap.Logger) *Consumer {
	return &Consumer{
		store:  store,
		logger: logger,
	}
}

func (c *Consumer) Listen(ch *amqp.Channel) {
	q, err := ch.QueueDeclare(
		"",    // name
		true,  // durable
		false, // delete when unused
		true,  // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		c.logger.Fatal("failed to declare queue", zap.Error(err))
	}

	err = ch.QueueBind(
		q.Name,                // queue name
		"",                    // routing key
		broker.OrderPaidEvent, // exchange
		false,                 // no-wait
		nil,
	)
	if err != nil {
		c.logger.Fatal("failed to bind queue", zap.Error(err))
	}

	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		c.logger.Fatal("failed to start consuming", zap.Error(err))
	}

	var forever chan struct{}

	go func() {
		for d := range msgs {
			// Extract headers
			ctx := broker.ExtractAMQPHeader(context.Background(), d.Headers)

			// Create a new span
			tr := otel.Tracer("amqp")
			_, messageSpan := tr.Start(ctx, fmt.Sprintf("AMQP - consume - %s", q.Name))

			c.logger.Info("received order.paid message", zap.ByteString("body", d.Body))

			// Parse order from JSON
			var order pb.Order
			if err := json.Unmarshal(d.Body, &order); err != nil {
				c.logger.Error("failed to unmarshal order", zap.Error(err))
				d.Nack(false, false)
				messageSpan.End()
				continue
			}

			c.logger.Info("processing paid order - confirming stock reservation", zap.String("order_id", order.Id))

			// ⭐ Confirm Stock Reservation (NEW!)
			// Warum ConfirmReservation statt DecrementQuantity?
			// → Order wurde bereits bei CreateOrder reserviert (reserved_quantity++)
			// → Jetzt: Payment erfolgreich → Reservation bestätigen!
			// → ConfirmReservation macht:
			//   1. Decrement quantity (actual stock removal)
			//   2. Decrement reserved_quantity (release reservation)
			//   3. Update reservation status = 'confirmed'
			// → Alles in EINER Transaktion - ACID garantiert!
			err = c.store.ConfirmReservation(ctx, order.Id)
			if err != nil {
				c.logger.Error("failed to confirm reservation",
					zap.String("order_id", order.Id),
					zap.Error(err))
				// NACK message → goes to DLQ for retry
				d.Nack(false, false)
				messageSpan.End()
				c.logger.Warn("reservation confirmation failed - message sent to DLQ", zap.String("order_id", order.Id))
				continue
			}

			c.logger.Info("stock reservation confirmed",
				zap.String("order_id", order.Id),
				zap.Int("item_count", len(order.Items)))

			d.Ack(false)
			messageSpan.End()
			c.logger.Info("stock update completed", zap.String("order_id", order.Id))
		}
	}()

	c.logger.Info("AMQP consumer listening - press CTRL+C to exit")
	<-forever
}
