package main

import (
	"context"
	"fmt"
	"log/slog"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/payments/gateway"
	"github.com/timour/order-microservices/payments/processor"
)

// Warum 2 Dependencies (processor + gateway)?
// → processor: Externe API (Stripe) - für Payment Link Creation
// → gateway: gRPC Client - für Fanning Out pattern (callback to Orders Service)
// → Webhook handler wird später "order.paid" Event publishen!
type service struct {
	processor processor.PaymentProcessor
	gateway   gateway.OrdersGateway
	logger    *slog.Logger
}

func NewService(processor processor.PaymentProcessor, gateway gateway.OrdersGateway, logger *slog.Logger) *service {
	return &service{
		processor: processor,
		gateway:   gateway,
		logger:    logger,
	}
}

// CreatePayment: Business Logic für Payment Creation
// Flow:
// 1. Consumer empfängt Order Event → ruft CreatePayment
// 2. CreatePayment → ruft Stripe API (Payment Link)
// 3. CreatePayment → gRPC call to Orders Service (Fanning Out pattern!)
// 4. Später: Stripe Webhook → publishes "order.paid" Event
func (s *service) CreatePayment(ctx context.Context, order *pb.Order) (string, error) {
	if order == nil {
		return "", fmt.Errorf("order is nil")
	}

	// Warum processor.CreatePaymentLink?
	// → Ruft Stripe API: Erstellt Checkout Session
	// → Gibt Payment Link zurück (z.B. "https://checkout.stripe.com/...")
	paymentLink, err := s.processor.CreatePaymentLink(order)
	if err != nil {
		return "", fmt.Errorf("failed to create payment link: %w", err)
	}

	// ⭐ FANNING OUT PATTERN: gRPC call back to Orders Service
	// → Update Order with payment_link and status "waiting_payment"
	// → Synchronous request-response (not Event!)
	// → Stripe Webhook wird später "order.paid" Event publishen!
	err = s.gateway.UpdateOrderAfterPaymentLink(ctx, order.Id, paymentLink)
	if err != nil {
		return "", fmt.Errorf("failed to update order via gRPC: %w", err)
	}

	s.logger.Info("payment link created and order updated",
		slog.String("order_id", order.Id),
		slog.String("payment_link", paymentLink),
	)

	return paymentLink, nil
}
// rebuild trigger
