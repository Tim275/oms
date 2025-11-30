package metrics

import "github.com/prometheus/client_golang/prometheus"

// Business Metrics - Revenue & Analytics Tracking
// Warum?
// → Technische Metrics (HTTP requests, latency) sind wichtig für Ops
// → Business Metrics (Revenue, Sales) sind wichtig für Business Decisions!
// → Beide zusammen = Full Observability Stack

var (
	// 💰 Revenue Counter - Total Revenue by Order Status
	// Usage: metrics.OrderRevenueTotal.WithLabelValues("paid").Add(totalAmount)
	OrderRevenueTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "order_revenue_euros_total",
			Help: "Total revenue from orders in EUR (by status)",
		},
		[]string{"status"}, // pending, paid, preparing, ready, completed
	)

	// 📊 Item Sales Counter - Items sold by name
	// Usage: metrics.ItemSoldCounter.WithLabelValues("Cheeseburger").Add(2)
	ItemSoldCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "items_sold_total",
			Help: "Total number of items sold by item name",
		},
		[]string{"item_name"},
	)

	// 💵 Order Value Distribution - Histogram für Average Order Value
	// Usage: metrics.OrderValueHistogram.Observe(totalAmount)
	OrderValueHistogram = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "order_value_euros",
			Help:    "Order value distribution in EUR",
			Buckets: []float64{5, 10, 20, 30, 50, 75, 100, 150, 200},
		},
	)

	// 📈 Orders by Status - Current number of orders in each status
	// Usage: metrics.OrdersByStatus.WithLabelValues("preparing").Inc()
	OrdersByStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "orders_by_status_current",
			Help: "Current number of orders by status (Gauge)",
		},
		[]string{"status"},
	)

	// 🕐 Order Preparation Time - How long does it take to prepare orders?
	// Usage: metrics.OrderPrepTimeSeconds.Observe(durationSeconds)
	OrderPrepTimeSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "order_prep_time_seconds",
			Help:    "Order preparation time distribution in seconds",
			Buckets: []float64{60, 120, 180, 300, 600, 900}, // 1min, 2min, 3min, 5min, 10min, 15min
		},
	)

	// 🎯 Orders Total Counter - Total number of orders created
	// Usage: metrics.OrdersTotal.Inc()
	OrdersTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orders_total",
			Help: "Total number of orders created",
		},
	)
)

func init() {
	// Register all business metrics with Prometheus
	prometheus.MustRegister(
		OrderRevenueTotal,
		ItemSoldCounter,
		OrderValueHistogram,
		OrdersByStatus,
		OrderPrepTimeSeconds,
		OrdersTotal,
	)
}

// Helper Functions

// TrackOrderCreated: Track a new order creation with all metrics
func TrackOrderCreated(orderValue float64, items map[string]int32) {
	// Increment total orders counter
	OrdersTotal.Inc()

	// Track revenue (pending state)
	OrderRevenueTotal.WithLabelValues("pending").Add(orderValue)

	// Track order value distribution
	OrderValueHistogram.Observe(orderValue)

	// Track items sold
	for itemName, quantity := range items {
		ItemSoldCounter.WithLabelValues(itemName).Add(float64(quantity))
	}

	// Track order status gauge
	OrdersByStatus.WithLabelValues("pending").Inc()
}

// TrackOrderStatusChange: Track when order status changes
func TrackOrderStatusChange(oldStatus, newStatus string, orderValue float64) {
	// Decrement old status
	if oldStatus != "" {
		OrdersByStatus.WithLabelValues(oldStatus).Dec()
	}

	// Increment new status
	OrdersByStatus.WithLabelValues(newStatus).Inc()

	// If order is now paid, track paid revenue
	if newStatus == "paid" {
		OrderRevenueTotal.WithLabelValues("paid").Add(orderValue)
	}
}

// TrackOrderPrepTime: Track how long it took to prepare an order
func TrackOrderPrepTime(durationSeconds float64) {
	OrderPrepTimeSeconds.Observe(durationSeconds)
}
