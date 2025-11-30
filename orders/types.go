package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type OrdersService interface {
	CreateOrder(context.Context) error
	UpdateOrder(context.Context, *api.Order) (*api.Order, error)
	GetOrder(context.Context, string) (*api.Order, error)
}

type OrdersStore interface {
	Create(context.Context, *api.Order) (primitive.ObjectID, error)
	Update(context.Context, string, *api.Order) error
	Get(context.Context, string) (*api.Order, error)
	GetByStatus(context.Context, string) ([]*api.Order, error)
}
