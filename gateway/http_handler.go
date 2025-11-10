package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/discovery"
)

type handler struct {
	ordersClient api.OrderServiceClient
	registry     discovery.Registry
	logger       *slog.Logger
}

func NewHandler(registry discovery.Registry, logger *slog.Logger) *handler {
	return &handler{
		registry: registry,
		logger:   logger,
	}
}

func (h *handler) getOrdersClient(ctx context.Context) (api.OrderServiceClient, error) {
	// ⭐ NEW: ServiceConnection() Helper mit OpenTelemetry!
	// Warum discovery.ServiceConnection?
	// → Service Discovery + gRPC Dial + OpenTelemetry in EINER Funktion!
	// → Automatisches Tracing für HTTP → gRPC Calls
	// → Load Balancing (random) eingebaut
	conn, err := discovery.ServiceConnection(ctx, "orders", h.registry)
	if err != nil {
		return nil, err
	}

	return api.NewOrderServiceClient(conn), nil
}

func (h *handler) registerRoute(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/customers/{customerID}/orders", h.handleCreateOrder)
	mux.HandleFunc("GET /api/customers/{customerID}/orders/{orderID}", h.handleGetOrder)
	mux.HandleFunc("PUT /api/customers/{customerID}/orders/{orderID}", h.handleUpdateOrder)
	mux.HandleFunc("GET /api/menu", h.handleGetMenu) // ⭐ NEW: Menu endpoint with Stripe Product data
	mux.HandleFunc("GET /api/orders", h.handleGetOrders)

	// Serve static files from public directory
	fs := http.FileServer(http.Dir("./public"))
	mux.Handle("/", fs)
}

func (h *handler) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	customerID := r.PathValue("customerID")
	orderID := r.PathValue("orderID")

	h.logger.Info("get order request",
		slog.String("customer_id", customerID),
		slog.String("order_id", orderID),
	)

	// Call Orders Service via gRPC
	ordersClient, err := h.getOrdersClient(context.Background())
	if err != nil {
		h.logger.Error("failed to discover orders service", slog.Any("error", err))
		http.Error(w, "Orders service unavailable", http.StatusServiceUnavailable)
		return
	}

	order, err := ordersClient.GetOrder(context.Background(), &api.GetOrderRequest{
		OrderId:    orderID,
		CustomerId: customerID,
	})
	if err != nil {
		h.logger.Error("failed to get order",
			slog.String("order_id", orderID),
			slog.Any("error", err),
		)
		http.Error(w, "Failed to get order", http.StatusInternalServerError)
		return
	}

	h.logger.Info("order retrieved successfully",
		slog.String("order_id", order.Id),
		slog.String("status", order.Status),
	)

	// Return full order with payment link
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(order)
}

// handleUpdateOrder: PUT /api/customers/{customerID}/orders/{orderID}
// Updates order status (used by Kitchen Display to mark orders as ready)
func (h *handler) handleUpdateOrder(w http.ResponseWriter, r *http.Request) {
	customerID := r.PathValue("customerID")
	orderID := r.PathValue("orderID")

	h.logger.Info("update order request",
		slog.String("customer_id", customerID),
		slog.String("order_id", orderID),
	)

	// Parse request body
	var updateRequest struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&updateRequest); err != nil {
		h.logger.Error("failed to decode request body", slog.Any("error", err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get Orders Client via service discovery
	ordersClient, err := h.getOrdersClient(context.Background())
	if err != nil {
		h.logger.Error("failed to discover orders service", slog.Any("error", err))
		http.Error(w, "Orders service unavailable", http.StatusServiceUnavailable)
		return
	}

	// First get the existing order to get all fields
	existingOrder, err := ordersClient.GetOrder(context.Background(), &api.GetOrderRequest{
		OrderId:    orderID,
		CustomerId: customerID,
	})
	if err != nil {
		h.logger.Error("failed to get existing order",
			slog.String("order_id", orderID),
			slog.Any("error", err),
		)
		http.Error(w, "Failed to get order", http.StatusInternalServerError)
		return
	}

	// Update only the status field
	existingOrder.Status = updateRequest.Status

	// Call UpdateOrder gRPC method
	updatedOrder, err := ordersClient.UpdateOrder(context.Background(), existingOrder)
	if err != nil {
		h.logger.Error("failed to update order",
			slog.String("order_id", orderID),
			slog.String("new_status", updateRequest.Status),
			slog.Any("error", err),
		)
		http.Error(w, "Failed to update order", http.StatusInternalServerError)
		return
	}

	h.logger.Info("order updated successfully",
		slog.String("order_id", orderID),
		slog.String("new_status", updatedOrder.Status),
	)

	// Return updated order
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedOrder)
}

type CreateOrderItem struct {
	ID       string `json:"id"`
	Quantity int32  `json:"quantity"`
	PriceID  string `json:"price_id"`
}

func (h *handler) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	customerID := r.PathValue("customerID")

	// Parse JSON body
	var items []CreateOrderItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		h.logger.Error("failed to decode request body", slog.Any("error", err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate items
	if err := validateItems(items); err != nil {
		h.logger.Warn("validation error",
			slog.String("customer_id", customerID),
			slog.Any("error", err),
		)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.logger.Info("order request received",
		slog.String("customer_id", customerID),
		slog.Int("items_count", len(items)),
	)

	// Convert to protobuf format
	protoItems := make([]*api.ItemsWithQuantity, len(items))
	for i, item := range items {
		protoItems[i] = &api.ItemsWithQuantity{
			ID:       item.ID,
			Quantity: item.Quantity,
		}
	}

	// Call Orders Service via gRPC
	ordersClient, err := h.getOrdersClient(context.Background())
	if err != nil {
		h.logger.Error("failed to discover orders service", slog.Any("error", err))
		http.Error(w, "Orders service unavailable", http.StatusServiceUnavailable)
		return
	}

	order, err := ordersClient.CreateOrder(context.Background(), &api.CreateOrderRequest{
		CustomerId: customerID,
		Items:      protoItems,
	})
	if err != nil {
		h.logger.Error("failed to create order",
			slog.String("customer_id", customerID),
			slog.Any("error", err),
		)
		http.Error(w, "Failed to create order", http.StatusInternalServerError)
		return
	}

	h.logger.Info("order created successfully",
		slog.String("order_id", order.Id),
		slog.String("customer_id", customerID),
	)

	// Return full order with payment link
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}

// validateItems: Prüft ob Items gültig sind
func validateItems(items []CreateOrderItem) error {
	if len(items) == 0 {
		return errors.New("order must contain at least one item")
	}

	for _, item := range items {
		if item.ID == "" {
			return errors.New("item ID is required")
		}

		if item.Quantity <= 0 {
			return errors.New("items must have valid quantity")
		}
	}

	return nil
}

// handleGetOrders: GET /api/orders?status={status}
// Fetches orders filtered by status from Orders Service
func (h *handler) handleGetOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get status from query parameter
	status := r.URL.Query().Get("status")

	h.logger.Info("get orders request",
		slog.String("status", status),
	)

	// Get Orders Client via service discovery
	ordersClient, err := h.getOrdersClient(ctx)
	if err != nil {
		h.logger.Error("failed to discover orders service", slog.Any("error", err))
		http.Error(w, "Orders service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Call GetOrdersByStatus gRPC method
	response, err := ordersClient.GetOrdersByStatus(ctx, &api.GetOrdersByStatusRequest{
		Status: status,
	})
	if err != nil {
		h.logger.Error("failed to get orders by status",
			slog.String("status", status),
			slog.Any("error", err),
		)
		http.Error(w, "Failed to get orders", http.StatusInternalServerError)
		return
	}

	h.logger.Info("orders retrieved successfully",
		slog.String("status", status),
		slog.Int("orders_count", len(response.Orders)),
	)

	// Return JSON array of orders
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response.Orders)
}
