package processor

import (
	pb "github.com/timour/order-microservices/common/api"
)

type PaymentProcessor interface {
	CreatePaymentLink(*pb.Order) (string, error)
}
