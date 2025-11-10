package gateway

import (
	"context"
	"log"

	pb "github.com/timour/order-microservices/common/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type OrdersGateway interface {
	UpdateOrderAfterPaymentLink(ctx context.Context, orderID, paymentLink string) error
	UpdateOrderStatus(ctx context.Context, orderID, customerID, status string) error
}

type ordersGateway struct {
	ordersAddr string
}

func NewOrdersGateway(ordersAddr string) OrdersGateway {
	return &ordersGateway{
		ordersAddr: ordersAddr,
	}
}

// UpdateOrderAfterPaymentLink updates the order with payment link and status "waiting_payment"
// This is called after Stripe checkout session is created
func (g *ordersGateway) UpdateOrderAfterPaymentLink(ctx context.Context, orderID, paymentLink string) error {
	// Connect to Orders service via gRPC
	conn, err := grpc.NewClient(g.ordersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	ordersClient := pb.NewOrderServiceClient(conn)

	// Update order with payment link and new status
	_, err = ordersClient.UpdateOrder(ctx, &pb.Order{
		Id:          orderID,
		Status:      "waiting_payment", // Status changes from "pending" to "waiting_payment"
		PaymentLink: paymentLink,
	})
	if err != nil {
		log.Printf("Failed to update order via gRPC: %v", err)
		return err
	}

	log.Printf("Order %s updated with payment link via gRPC", orderID)
	return nil
}

// UpdateOrderStatus updates the order status after payment
// This is called by the webhook handler when Stripe payment succeeds
func (g *ordersGateway) UpdateOrderStatus(ctx context.Context, orderID, customerID, status string) error {
	// Connect to Orders service via gRPC
	conn, err := grpc.NewClient(g.ordersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	ordersClient := pb.NewOrderServiceClient(conn)

	// Update order status
	_, err = ordersClient.UpdateOrder(ctx, &pb.Order{
		Id:         orderID,
		CustomerId: customerID,
		Status:     status,
	})
	if err != nil {
		log.Printf("Failed to update order status via gRPC: %v", err)
		return err
	}

	log.Printf("Order %s updated to status '%s' via gRPC", orderID, status)
	return nil
}
