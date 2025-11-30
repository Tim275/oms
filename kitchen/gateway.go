package main

import (
	"context"
	"log/slog"

	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/discovery"
)

// Gateway - Interface zum Orders Service
type Gateway interface {
	UpdateOrder(ctx context.Context, order *api.Order) error
}

type gateway struct {
	registry discovery.Registry
	logger   *slog.Logger
}

func NewGateway(registry discovery.Registry, logger *slog.Logger) Gateway {
	return &gateway{
		registry: registry,
		logger:   logger,
	}
}

// UpdateOrder - Ruft Orders Service auf um Order Status zu aktualisieren
// Warum eigene Funktion?
// → Kitchen Service hat KEINE Datenbank!
// → Orders Service ist Source of Truth für Order Status
// → gRPC Call propagiert automatisch Trace Context (OpenTelemetry)
func (g *gateway) UpdateOrder(ctx context.Context, order *api.Order) error {
	// Service Discovery: Finde Orders Service
	// Warum Discovery?
	// → Orders Service IP kann sich ändern (Kubernetes!)
	// → Consul weiß immer die aktuelle Adresse
	conn, err := discovery.ServiceConnection(ctx, "orders", g.registry)
	if err != nil {
		g.logger.Error("failed to connect to orders service", slog.Any("error", err))
		return err
	}
	defer conn.Close()

	// Create gRPC client
	ordersClient := api.NewOrderServiceClient(conn)

	// Call UpdateOrder RPC
	// Warum Context?
	// → Timeouts respektieren
	// → Trace Context propagieren (OpenTelemetry!)
	// → Request Cancellation
	updatedOrder, err := ordersClient.UpdateOrder(ctx, order)
	if err != nil {
		g.logger.Error("failed to update order via grpc",
			slog.String("order_id", order.Id),
			slog.String("status", order.Status),
			slog.Any("error", err),
		)
		return err
	}

	g.logger.Info("order updated via grpc",
		slog.String("order_id", updatedOrder.Id),
		slog.String("status", updatedOrder.Status),
	)

	return nil
}
