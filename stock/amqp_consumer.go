package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
	"go.opentelemetry.io/otel"
)

type Consumer struct {
	store StockStore
}

func NewConsumer(store StockStore) *Consumer {
	return &Consumer{
		store: store,
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
		log.Fatal(err)
	}

	err = ch.QueueBind(
		q.Name,                // queue name
		"",                    // routing key
		broker.OrderPaidEvent, // exchange
		false,                 // no-wait
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		log.Fatal(err)
	}

	var forever chan struct{}

	go func() {
		for d := range msgs {
			// Extract headers
			ctx := broker.ExtractAMQPHeader(context.Background(), d.Headers)

			// Create a new span
			tr := otel.Tracer("amqp")
			_, messageSpan := tr.Start(ctx, fmt.Sprintf("AMQP - consume - %s", q.Name))

			log.Printf("Received order.paid message: %s", d.Body)

			// Parse order from JSON
			var order pb.Order
			if err := json.Unmarshal(d.Body, &order); err != nil {
				log.Printf("ERROR: Failed to unmarshal order: %v", err)
				d.Nack(false, false)
				messageSpan.End()
				continue
			}

			log.Printf("Processing paid order %s - Confirming stock reservation", order.Id)

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
				log.Printf("ERROR: Failed to confirm reservation for order %s: %v", order.Id, err)
				// NACK message → goes to DLQ for retry
				d.Nack(false, false)
				messageSpan.End()
				log.Printf("❌ Reservation confirmation failed - Message sent to DLQ: %s", order.Id)
				continue
			}

			log.Printf("✅ Stock reservation confirmed for order: %s (%d items)", order.Id, len(order.Items))

			d.Ack(false)
			messageSpan.End()
			log.Printf("✅ Stock update completed for order: %s", order.Id)
		}
	}()

	log.Printf("AMQP Listening. To exit press CTRL+C")
	<-forever
}
