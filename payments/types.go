package main

import (
	"context"

	pb "github.com/timour/order-microservices/common/api"
)

// PaymentService defines the business logic interface
type PaymentService interface {
	CreatePayment(context.Context, *pb.Order) (string, error)
}
