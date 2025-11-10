package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/timour/order-microservices/common/api"
)

type HTTPHandler struct {
	gateway Gateway
	logger  *slog.Logger
}

func NewHTTPHandler(gateway Gateway, logger *slog.Logger) *HTTPHandler {
	return &HTTPHandler{
		gateway: gateway,
		logger:  logger,
	}
}

func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// ⭐ REST API: Chef markiert Order als "ready"
	// POST /api/orders/{orderID}/ready
	// Example: POST http://localhost:8083/api/orders/42/ready
	mux.HandleFunc("/api/orders/", h.handleOrderReady)
}

// handleOrderReady - Chef bestätigt dass Order fertig ist
// Flow:
// 1. Chef klickt Button im Terminal UI
// 2. Terminal sendet POST /api/orders/42/ready
// 3. Kitchen Service ruft UpdateOrder auf → Status "ready"
// 4. Orders Service publiziert order.ready Event
// 5. Notification Service zeigt Customer: "Your order #42 is ready!" ✅
func (h *HTTPHandler) handleOrderReady(w http.ResponseWriter, r *http.Request) {
	// Warum Method Check?
	// → Nur POST erlaubt! GET/PUT/DELETE nicht sinnvoll
	if r.Method != http.MethodPost {
		h.logger.Warn("method not allowed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract orderID from path: /api/orders/{orderID}/ready
	// Example: /api/orders/42/ready → orderID = "42"
	path := strings.TrimPrefix(r.URL.Path, "/api/orders/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "ready" {
		h.logger.Warn("invalid path format",
			slog.String("path", r.URL.Path),
		)
		http.Error(w, "Invalid path format. Expected: /api/orders/{orderID}/ready", http.StatusBadRequest)
		return
	}

	orderID := parts[0]

	h.logger.Info("chef marking order as ready",
		slog.String("service", "kitchen"),
		slog.String("order_id", orderID),
	)

	// Warum nur orderID und Status senden?
	// → UpdateOrder merged mit existierender Order
	// → Wir wissen nur: Order ist fertig!
	// → CustomerID, Items, etc. sind im Orders Service gespeichert
	err := h.gateway.UpdateOrder(context.Background(), &api.Order{
		Id:     orderID,
		Status: "ready", // ⭐ MANUELL vom Chef bestätigt!
	})
	if err != nil {
		h.logger.Error("failed to update order to ready",
			slog.String("service", "kitchen"),
			slog.String("order_id", orderID),
			slog.Any("error", err),
		)
		http.Error(w, "Failed to update order", http.StatusInternalServerError)
		return
	}

	h.logger.Info("order marked as ready",
		slog.String("service", "kitchen"),
		slog.String("order_id", orderID),
	)

	// Response: JSON mit Bestätigung
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"order_id": orderID,
		"status":   "ready",
		"message":  "Order successfully marked as ready",
	})
}
