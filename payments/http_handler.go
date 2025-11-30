package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/payments/gateway"
	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/webhook"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type PaymentHTTPHandler struct {
	resilientConn *broker.ResilientConnection
	ordersGateway gateway.OrdersGateway
	ordersAddr    string
}

func NewPaymentHTTPHandler(resilientConn *broker.ResilientConnection, ordersGateway gateway.OrdersGateway, ordersAddr string) *PaymentHTTPHandler {
	return &PaymentHTTPHandler{
		resilientConn: resilientConn,
		ordersGateway: ordersGateway,
		ordersAddr:    ordersAddr,
	}
}

func (h *PaymentHTTPHandler) registerRoutes(router *http.ServeMux) {
	// ⭐ OpenTelemetry Auto-Instrumentation for Stripe Webhook
	// → Traces incoming Stripe webhook events
	// → Captures payment flow end-to-end
	router.Handle("/webhook",
		otelhttp.NewHandler(http.HandlerFunc(h.handleCheckoutWebhook), "POST /webhook"))

	// Metrics endpoint (no tracing needed)
	router.Handle("/metrics", promhttp.Handler())
}

func (h *PaymentHTTPHandler) handleCheckoutWebhook(w http.ResponseWriter, r *http.Request) {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading request body: %v\n", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Verify webhook signature using Stripe's official webhook package
	event, err := webhook.ConstructEventWithOptions(
		body,
		r.Header.Get("Stripe-Signature"),
		endpointStripeSecret,
		webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true,
		},
	)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error verifying webhook signature: %v\n", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if event.Type == "checkout.session.completed" {
		var session stripe.CheckoutSession
		err := json.Unmarshal(event.Data.Raw, &session)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if session.PaymentStatus == "paid" {
			log.Printf("Payment for Checkout Session %v succeeded!", session.ID)

			orderID := session.Metadata["orderID"]
			customerID := session.Metadata["customerID"]

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// ⭐ STEP 1: Update Order Status to "paid" in MongoDB FIRST!
			// → Warum ZUERST?
			// → Kitchen Service subscribt "order.paid" Event und updated Status zu "preparing"
			// → Wenn wir NICHT zuerst "paid" in DB schreiben, siehst du NIE "paid" Status!
			// → Flow MUSS sein: pending → waiting_payment → paid → preparing
			err = h.ordersGateway.UpdateOrderStatus(ctx, orderID, customerID, "paid")
			if err != nil {
				log.Printf("Error updating order status to paid: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			log.Printf("Order %s status updated to 'paid' in database", orderID)

			o := &pb.Order{
				Id:         orderID,
				CustomerId: customerID,
				Status:     "paid",
			}

			marshalledOrder, err := json.Marshal(o)
			if err != nil {
				log.Fatal(err.Error())
			}

			// ⭐ STEP 2: NOW publish event to RabbitMQ with RETRY LOGIC
			// → Kitchen Service empfängt Event und updated Status zu "preparing"
			// → Aber "paid" Status ist BEREITS in MongoDB gespeichert!
			// ✅ PRODUCTION PATTERN: Retry with fresh channel
			// → resilientConn.Publish() gets fresh channel automatically
			// → Retries on failure (up to 3 attempts with fresh channels)
			maxRetries := 3
			var publishErr error

			for attempt := 1; attempt <= maxRetries; attempt++ {
				publishErr = h.resilientConn.Publish(ctx, broker.OrderPaidEvent, "", marshalledOrder)
				if publishErr == nil {
					log.Println("✅ Message published order.paid")
					break
				}

				log.Printf("⚠️  Failed to publish order.paid (attempt %d/%d): %v", attempt, maxRetries, publishErr)
				if attempt < maxRetries {
					time.Sleep(time.Duration(attempt) * time.Second) // Exponential backoff
				}
			}

			if publishErr != nil {
				log.Printf("❌ ERROR: Failed to publish order.paid after %d attempts: %v", maxRetries, publishErr)
				// NOTE: Order status is already "paid" in DB, so user can still see it
				// Kitchen Display won't show it though - manual intervention needed
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}
