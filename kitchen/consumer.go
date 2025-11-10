package main

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

type Consumer struct {
	gateway Gateway
	channel *amqp.Channel
	logger  *slog.Logger
}

func NewConsumer(gateway Gateway, channel *amqp.Channel, logger *slog.Logger) *Consumer {
	return &Consumer{
		gateway: gateway,
		channel: channel,
		logger:  logger,
	}
}

// Listen - Konsumiert order.paid Events und setzt Status auf "preparing"
// Flow:
// 1. Payment Service publiziert order.paid
// 2. Kitchen Service empfängt Event
// 3. Kitchen Service ruft UpdateOrder auf → Status "preparing"
// 4. Orders Service publiziert order.preparing Event
func (c *Consumer) Listen() {
	// Warum QueueDeclare?
	// → Erstellt Queue "order.paid" falls nicht existiert
	// → Idempotent: Mehrfaches Aufrufen = kein Problem
	// → x-dead-letter-exchange: Failed messages → DLX → order.paid.dlq
	q, err := c.channel.QueueDeclare(
		broker.OrderPaidEvent, // name: "order.paid"
		true,                  // durable: Queue überlebt RabbitMQ Restart
		false,                 // auto-delete: NEIN
		false,                 // exclusive: Andere können zugreifen
		false,                 // no-wait
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX, // ⭐ DLX Integration! Failed messages → "dlx" exchange
		},
	)
	if err != nil {
		c.logger.Error("failed to declare queue",
			slog.String("service", "kitchen"),
			slog.String("queue", broker.OrderPaidEvent),
			slog.Any("error", err),
		)
		return
	}

	c.logger.Info("queue declared",
		slog.String("service", "kitchen"),
		slog.String("queue", q.Name),
	)

	// ⭐ Warum QueueBind?
	// → Bindet Queue an Exchange "order.paid"
	// → Payment Service published zu Exchange → Messages landen in Queue!
	// → OHNE Bind: Messages gehen verloren!
	err = c.channel.QueueBind(
		q.Name,                // queue name: "order.paid"
		"",                    // routing key: "" = matches all
		broker.OrderPaidEvent, // exchange name: "order.paid"
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		c.logger.Error("failed to bind queue to exchange",
			slog.String("service", "kitchen"),
			slog.String("queue", q.Name),
			slog.String("exchange", broker.OrderPaidEvent),
			slog.Any("error", err),
		)
		return
	}

	c.logger.Info("queue bound to exchange",
		slog.String("service", "kitchen"),
		slog.String("queue", q.Name),
		slog.String("exchange", broker.OrderPaidEvent),
	)

	// Warum Consume?
	// → Registriert Consumer für die Queue
	// → Returns channel mit Messages
	// → Auto-Ack = false: Wir müssen d.Ack() manuell aufrufen!
	msgs, err := c.channel.Consume(
		q.Name,  // queue name
		"",      // consumer tag (auto-generated)
		false,   // auto-ack: NEIN! Wir wollen manuell ACK
		false,   // exclusive
		false,   // no-local
		false,   // no-wait
		nil,     // args
	)
	if err != nil {
		c.logger.Error("failed to register consumer",
			slog.String("service", "kitchen"),
			slog.Any("error", err),
		)
		return
	}

	c.logger.Info("kitchen consumer started",
		slog.String("service", "kitchen"),
		slog.String("queue", q.Name),
	)

	c.logger.Info("waiting for messages...",
		slog.String("service", "kitchen"),
		slog.String("queue", q.Name),
	)

	// Warum infinite loop?
	// → Consumer läuft DAUERHAFT! Wartet auf Messages
	// → Blockiert bis Message ankommt
	for d := range msgs {
		c.logger.Info("received message",
			slog.String("service", "kitchen"),
			slog.String("event", broker.OrderPaidEvent),
			slog.Int("body_size", len(d.Body)),
		)

		// Warum Unmarshal?
		// → Message Body ist JSON bytes
		// → Konvertieren zu Go struct (*api.Order)
		var order api.Order
		if err := json.Unmarshal(d.Body, &order); err != nil {
			c.logger.Error("failed to unmarshal order",
				slog.String("service", "kitchen"),
				slog.Any("error", err),
			)

			// Warum Nack mit requeue=false?
			// → Message ist kaputt (invalid JSON)
			// → Retry macht keinen Sinn!
			// → Send to DLQ
			if err := broker.HandleRetry(c.channel, &d); err != nil {
				c.logger.Error("failed to handle retry",
					slog.String("service", "kitchen"),
					slog.Any("error", err),
				)
			}
			continue
		}

		c.logger.Info("order unmarshalled",
			slog.String("service", "kitchen"),
			slog.String("order_id", order.Id),
			slog.String("customer_id", order.CustomerId),
			slog.String("status", order.Status),
		)

		// ⭐ BUSINESS LOGIC: Update Order Status zu "preparing"
		// Warum "preparing" und nicht "ready"?
		// → order.paid → Kitchen empfängt Order → Automatisch "preparing"
		// → Chef kocht das Essen 👨‍🍳
		// → Chef bestätigt manuell via REST API → "ready"
		if order.Status == "paid" {
			c.logger.Info("updating order status to preparing",
				slog.String("service", "kitchen"),
				slog.String("order_id", order.Id),
			)

			// Warum nur Status und ID senden?
			// → UpdateOrder merged mit existierender Order
			// → Wir wollen nur Status ändern, nichts anderes!
			err := c.gateway.UpdateOrder(context.Background(), &api.Order{
				Id:         order.Id,
				CustomerId: order.CustomerId,
				Status:     "preparing", // ⭐ AUTOMATISCH
			})
			if err != nil {
				c.logger.Error("failed to update order",
					slog.String("service", "kitchen"),
					slog.String("order_id", order.Id),
					slog.Any("error", err),
				)

				// Warum Retry?
				// → UpdateOrder kann fehlschlagen (Orders Service down, Network issue)
				// → Retry mit exponential backoff
				// → Nach 3 Retries → DLQ
				if err := broker.HandleRetry(c.channel, &d); err != nil {
					c.logger.Error("failed to handle retry",
						slog.String("service", "kitchen"),
						slog.Any("error", err),
					)
				}
				continue
			}

			c.logger.Info("order status updated to preparing",
				slog.String("service", "kitchen"),
				slog.String("order_id", order.Id),
			)
		} else {
			c.logger.Warn("unexpected order status, skipping",
				slog.String("service", "kitchen"),
				slog.String("order_id", order.Id),
				slog.String("status", order.Status),
			)
		}

		// Warum Ack?
		// → Bestätigt RabbitMQ: "Message erfolgreich verarbeitet!"
		// → RabbitMQ löscht Message aus Queue
		// → Ohne Ack: Message bleibt in Queue (wird nochmal delivered!)
		if err := d.Ack(false); err != nil {
			c.logger.Error("failed to ack message",
				slog.String("service", "kitchen"),
				slog.Any("error", err),
			)
		} else {
			c.logger.Info("message acknowledged",
				slog.String("service", "kitchen"),
				slog.String("order_id", order.Id),
			)
		}
	}

	log.Println("Consumer stopped")
}
