package main

import (
	"context"

	pb "github.com/timour/order-microservices/common/api"
)

type StockService interface {
	CheckIfItemAreInStock(context.Context, []*pb.ItemsWithQuantity) (bool, []*pb.Item, error)
	GetItems(ctx context.Context, ids []string) ([]*pb.Item, error)
	ReserveStock(ctx context.Context, orderID string, items []*pb.Item) (string, error)
}

type StockStore interface {
	GetItem(ctx context.Context, id string) (*pb.Item, error)
	GetItems(ctx context.Context, ids []string) ([]*pb.Item, error)
	DecrementQuantity(ctx context.Context, id string, amount int32) error
	// Reservation methods
	ReserveStock(ctx context.Context, orderID string, items []*pb.Item) (string, error)
	ConfirmReservation(ctx context.Context, orderID string) error
	ReleaseReservation(ctx context.Context, orderID string) error
}
