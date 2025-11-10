package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
)

type service struct {
	store OrdersStore
}

func NewService(store OrdersStore) *service {
	return &service{store}
}

func (s *service) CreateOrder(ctx context.Context) error {
	return nil
}

func (s *service) UpdateOrder(ctx context.Context, order *api.Order) (*api.Order, error) {
	if err := s.store.Update(ctx, order.Id, order); err != nil {
		return nil, err
	}

	// Return updated order
	return s.store.Get(ctx, order.Id)
}

func (s *service) GetOrder(ctx context.Context, orderID string) (*api.Order, error) {
	return s.store.Get(ctx, orderID)
}
