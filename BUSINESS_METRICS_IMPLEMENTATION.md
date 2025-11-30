# Business Metrics Implementation Guide

## 📊 Overview

This guide shows you how to integrate the business metrics into your services.

**What you'll get:**
- 💰 Real-time revenue tracking
- 📊 Item sales analytics
- 📈 Order value distribution
- 🕐 Kitchen prep time tracking

## 🚀 Quick Start

### Step 1: Import the metrics package

```go
import (
    "github.com/timour/order-microservices/common/metrics"
)
```

### Step 2: Instrument your code

See examples below for each service.

---

## 📝 Orders Service Implementation

### File: `orders/grpc_handler.go`

#### CreateOrder - Track new orders

```go
func (h *GRPCHandler) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.Order, error) {
    // ... existing validation code ...

    // Create order (existing code)
    order, err := h.store.CreateOrder(ctx, &types.Order{
        CustomerID: req.CustomerId,
        Items:      orderItems,
        Status:     "pending",
        CreatedAt:  time.Now(),
    })
    if err != nil {
        return nil, err
    }

    // ⭐ NEW: Calculate order total
    var totalAmount float64
    itemsMap := make(map[string]int32)

    for _, item := range order.Items {
        totalAmount += float64(item.Price * int32(item.Quantity))
        itemsMap[item.Name] = item.Quantity
    }

    // ⭐ NEW: Track business metrics
    metrics.TrackOrderCreated(totalAmount, itemsMap)

    h.logger.Info("order created with business metrics",
        slog.String("order_id", order.ID),
        slog.Float64("total_amount", totalAmount),
        slog.Int("items_count", len(order.Items)),
    )

    return order, nil
}
```

#### UpdateOrder - Track status changes

```go
func (h *GRPCHandler) UpdateOrder(ctx context.Context, order *pb.Order) (*pb.Order, error) {
    // Get existing order
    existingOrder, err := h.store.GetOrder(ctx, order.Id, order.CustomerId)
    if err != nil {
        return nil, err
    }

    oldStatus := existingOrder.Status

    // Update order (existing code)
    updatedOrder, err := h.store.UpdateOrder(ctx, order)
    if err != nil {
        return nil, err
    }

    // ⭐ NEW: Track status change
    if oldStatus != order.Status {
        var totalAmount float64
        for _, item := range order.Items {
            totalAmount += float64(item.Price * int32(item.Quantity))
        }

        metrics.TrackOrderStatusChange(oldStatus, order.Status, totalAmount)

        h.logger.Info("order status changed",
            slog.String("order_id", order.Id),
            slog.String("old_status", oldStatus),
            slog.String("new_status", order.Status),
        )
    }

    return updatedOrder, nil
}
```

---

## 🍳 Kitchen Service Implementation

### File: `kitchen/http_handler.go`

#### Track prep time when marking order as ready

```go
func (h *HTTPHandler) handleMarkReady(w http.ResponseWriter, r *http.Request) {
    var req struct {
        OrderID       string  `json:"order_id"`
        CustomerID    string  `json:"customer_id"`
        PrepStartTime string  `json:"prep_start_time,omitempty"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    // ⭐ NEW: Calculate prep time if provided
    if req.PrepStartTime != "" {
        startTime, err := time.Parse(time.RFC3339, req.PrepStartTime)
        if err == nil {
            prepDuration := time.Since(startTime).Seconds()
            metrics.TrackOrderPrepTime(prepDuration)

            h.logger.Info("order prep time tracked",
                slog.String("order_id", req.OrderID),
                slog.Float64("prep_time_seconds", prepDuration),
            )
        }
    }

    // ... rest of existing code to update order status ...
}
```

---

## 📈 Grafana Dashboard Queries

### Revenue Metrics

#### Total Revenue Today
```promql
sum(increase(order_revenue_euros_total{status="paid"}[24h]))
```

#### Revenue per Hour
```promql
sum(rate(order_revenue_euros_total{status="paid"}[1h])) * 3600
```

#### Revenue by Status
```promql
sum by (status) (order_revenue_euros_total)
```

### Sales Analytics

#### Top 5 Selling Items
```promql
topk(5, sum by (item_name) (rate(items_sold_total[1h])) * 3600)
```

#### Items Sold Today
```promql
sum by (item_name) (increase(items_sold_total[24h]))
```

### Order Metrics

#### Average Order Value (P50)
```promql
histogram_quantile(0.5, sum(rate(order_value_euros_bucket[5m])) by (le))
```

#### Average Order Value (P95)
```promql
histogram_quantile(0.95, sum(rate(order_value_euros_bucket[5m])) by (le))
```

#### Orders per Status
```promql
sum by (status) (orders_by_status_current)
```

#### Orders Created per Hour
```promql
rate(orders_total[1h]) * 3600
```

### Kitchen Metrics

#### Average Prep Time
```promql
histogram_quantile(0.5, sum(rate(order_prep_time_seconds_bucket[5m])) by (le))
```

#### 95th Percentile Prep Time
```promql
histogram_quantile(0.95, sum(rate(order_prep_time_seconds_bucket[5m])) by (le))
```

---

## 🎨 Grafana Dashboard JSON

### Create Dashboard: "Business Metrics"

```json
{
  "dashboard": {
    "title": "OMS - Business Metrics",
    "panels": [
      {
        "title": "💰 Revenue Today",
        "targets": [{
          "expr": "sum(increase(order_revenue_euros_total{status=\"paid\"}[24h]))"
        }],
        "type": "stat",
        "fieldConfig": {
          "defaults": {
            "unit": "currencyEUR"
          }
        }
      },
      {
        "title": "📊 Top Selling Items",
        "targets": [{
          "expr": "topk(5, sum by (item_name) (increase(items_sold_total[24h])))"
        }],
        "type": "bargauge"
      },
      {
        "title": "💵 Average Order Value",
        "targets": [{
          "expr": "histogram_quantile(0.5, sum(rate(order_value_euros_bucket[5m])) by (le))"
        }],
        "type": "stat",
        "fieldConfig": {
          "defaults": {
            "unit": "currencyEUR"
          }
        }
      },
      {
        "title": "📈 Orders per Hour",
        "targets": [{
          "expr": "rate(orders_total[1h]) * 3600"
        }],
        "type": "graph"
      }
    ]
  }
}
```

---

## 🧪 Testing

### 1. Create a test order

```bash
curl -X POST http://localhost:8080/api/customers/test-123/orders \
  -H 'Content-Type: application/json' \
  -d '[{"id":"1","quantity":2}]'
```

### 2. Check Prometheus metrics

```bash
# Open Prometheus
open http://localhost:9090

# Query metrics
order_revenue_euros_total
items_sold_total
order_value_euros
orders_by_status_current
```

### 3. View in Grafana

```bash
# Open Grafana
open http://localhost:3002

# Login: admin / admin123
# Navigate to: Dashboards → OMS - Business Metrics
```

---

## 🔄 Deployment Steps

### 1. Update dependencies

```bash
cd orders
go mod tidy
```

### 2. Rebuild images

```bash
docker compose -f docker-compose.prod.yml build orders kitchen
```

### 3. Restart services

```bash
docker compose -f docker-compose.prod.yml up -d
```

### 4. Verify metrics

```bash
# Check Orders service metrics
curl http://localhost:9001/metrics | grep business

# Should see:
# order_revenue_euros_total
# items_sold_total
# order_value_euros
# orders_by_status_current
# orders_total
```

---

## 📚 Additional Resources

- **Prometheus Documentation**: https://prometheus.io/docs/
- **Grafana Dashboards**: https://grafana.com/docs/grafana/latest/dashboards/
- **PromQL Guide**: https://prometheus.io/docs/prometheus/latest/querying/basics/

---

## ⚠️ Important Notes

1. **Counter vs Gauge**:
   - Use `Counter` for things that only increase (revenue, sales)
   - Use `Gauge` for things that go up and down (current orders)

2. **Label Cardinality**:
   - Don't use customer IDs as labels (too many unique values!)
   - Use item names, status, etc. (limited set of values)

3. **Performance**:
   - Metrics tracking adds ~1ms per operation
   - Negligible impact on overall performance

4. **Testing**:
   - Test locally first before production
   - Use E2E test script to verify metrics

---

**Ready to implement? Follow the examples above and integrate into your services!** 🚀
