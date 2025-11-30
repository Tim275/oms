package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/price"
	"github.com/stripe/stripe-go/v81/product"
	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/discovery"
)

// MenuItem represents a menu item with Stripe data
type MenuItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
	Image       string  `json:"image"`
	PriceID     string  `json:"priceId"`
	Quantity    int32   `json:"quantity"`
}

// handleGetMenu: GET /api/menu
// Fetches menu from Stock Service and enriches with Stripe Product data
func (h *handler) handleGetMenu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	h.logger.Info("get menu request")

	// 1️⃣ Get Stock Client
	stockClient, err := h.getStockClient(ctx)
	if err != nil {
		h.logger.Error("failed to discover stock service", slog.Any("error", err))
		http.Error(w, "Stock service unavailable", http.StatusServiceUnavailable)
		return
	}

	// 2️⃣ Get Items from Stock Service
	stockItems, err := stockClient.GetItems(ctx, &api.GetItemsRequest{})
	if err != nil {
		h.logger.Error("failed to get items from stock", slog.Any("error", err))
		http.Error(w, "Failed to get menu items", http.StatusInternalServerError)
		return
	}

	// 3️⃣ Enrich with Stripe Product Data
	menuItems := make([]MenuItem, 0, len(stockItems.Items))
	for _, item := range stockItems.Items {
		menuItem, err := h.getMenuItemWithStripeData(ctx, item)
		if err != nil {
			h.logger.Warn("failed to get stripe data for item",
				slog.String("item_id", item.ID),
				slog.Any("error", err),
			)
			// Fallback to basic data without Stripe enrichment
			menuItem = &MenuItem{
				ID:          item.ID,
				Name:        item.Name,
				Price:       float64(item.Quantity) / 100.0,
				Description: "",
				Image:       "",
				PriceID:     item.PriceID,
			Quantity:    item.Quantity,
			}
		}
		menuItems = append(menuItems, *menuItem)
	}

	h.logger.Info("menu retrieved successfully", slog.Int("items_count", len(menuItems)))

	// 4️⃣ Return JSON
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300") // 5 min browser cache
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(menuItems)
}

// getMenuItemWithStripeData: Fetch Stripe Product + Price data
func (h *handler) getMenuItemWithStripeData(ctx context.Context, item *api.Item) (*MenuItem, error) {
	// Set Stripe API key
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
	if stripe.Key == "" {
		return nil, fmt.Errorf("STRIPE_SECRET_KEY not set")
	}

	// Get Price (includes Product ID)
	priceData, err := price.Get(item.PriceID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get price from stripe: %w", err)
	}

	// Get Product (includes images, description)
	productData, err := product.Get(priceData.Product.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get product from stripe: %w", err)
	}

	// Build MenuItem from Stripe data
	menuItem := &MenuItem{
		ID:          item.ID,
		Name:        productData.Name,
		Price:       float64(priceData.UnitAmount) / 100.0,
		Description: productData.Description,
		Image:       "",
		PriceID:     item.PriceID,
			Quantity:    item.Quantity,
	}

	// Get first image if available
	if len(productData.Images) > 0 {
		menuItem.Image = productData.Images[0]
	}

	return menuItem, nil
}

// getStockClient: Service Discovery for Stock Service
func (h *handler) getStockClient(ctx context.Context) (api.StockServiceClient, error) {
	conn, err := discovery.ServiceConnection(ctx, "stock", h.registry)
	if err != nil {
		return nil, err
	}
	return api.NewStockServiceClient(conn), nil
}
