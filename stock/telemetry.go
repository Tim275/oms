package main

import (
	"context"
	"fmt"

	pb "github.com/timour/order-microservices/common/api"
	"go.opentelemetry.io/otel/trace"
)

type TelemetryMiddleware struct {
	next StockService
}

func NewTelemetryMiddleware(next StockService) StockService {
	return &TelemetryMiddleware{next}
}

func (s *TelemetryMiddleware) GetItems(ctx context.Context, ids []string) ([]*pb.Item, error) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(fmt.Sprintf("GetItems: %v", ids))

	return s.next.GetItems(ctx, ids)
}

func (s *TelemetryMiddleware) CheckIfItemAreInStock(ctx context.Context, p []*pb.ItemsWithQuantity) (bool, []*pb.Item, error) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(fmt.Sprintf("CheckIfItemAreInStock: %v", p))

	return s.next.CheckIfItemAreInStock(ctx, p)
}

func (s *TelemetryMiddleware) ReserveStock(ctx context.Context, orderID string, items []*pb.Item) (string, error) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(fmt.Sprintf("ReserveStock: orderID=%s, items=%d", orderID, len(items)))

	return s.next.ReserveStock(ctx, orderID, items)
}
