package main

import (
	"context"
	"fmt"

	pb "github.com/timour/order-microservices/common/api"
)

type Store struct {
	stock map[string]*pb.Item
}

func NewStore() *Store {
	return &Store{
		stock: map[string]*pb.Item{
			"1": {
				ID:       "1",
				Name:     "Burger",
				PriceID:  "price_1SQYsL3th7a1Jo3bsOVNnRpm",
				Quantity: 20,
			},
			"2": {
				ID:       "2",
				Name:     "Pommes",
				PriceID:  "price_POMMES_TODO",  // TODO: Erstelle price ID in Stripe für Pommes
				Quantity: 15,
			},
		},
	}
}

func (s *Store) GetItem(ctx context.Context, id string) (*pb.Item, error) {
	for _, item := range s.stock {
		if item.ID == id {
			return item, nil
		}
	}

	return nil, fmt.Errorf("item not found")
}

func (s *Store) GetItems(ctx context.Context, ids []string) ([]*pb.Item, error) {
	var res []*pb.Item
	for _, id := range ids {
		if i, ok := s.stock[id]; ok {
			res = append(res, i)
		}
	}

	return res, nil
}
