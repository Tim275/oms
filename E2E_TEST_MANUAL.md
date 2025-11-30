# Manual End-to-End Test Guide

## 🎯 Overview

This guide walks you through a complete manual test of the Order Management System.

**What you'll test:**
- ✅ Customer App (Order placement)
- ✅ Gateway API (HTTP → gRPC)
- ✅ Orders Service (MongoDB)
- ✅ Stock Service (PostgreSQL + Redis)
- ✅ Payments Service (Stripe)
- ✅ Kitchen Service (RabbitMQ Consumer)
- ✅ Kitchen Display (Chef interface)
- ✅ Trace ID Correlation (OpenTelemetry)

**Duration:** ~10 minutes

---

## 📋 Prerequisites

1. All services running:
   ```bash
   docker compose -f docker-compose.prod.yml up -d
   ```

2. Check all services are UP:
   ```bash
   docker compose -f docker-compose.prod.yml ps
   ```

3. Verify Prometheus targets (7/7 UP):
   ```bash
   curl -s http://localhost:9090/api/v1/targets | jq -r '.data.activeTargets[] | "\(.labels.job): \(.health)"'
   ```

---

## 🧪 Test Steps

### Step 1: Open All Required Tabs

Open these URLs in separate browser tabs:

1. **Customer App**: http://localhost:3000
2. **Kitchen Display**: http://localhost:3001
3. **Grafana**: http://localhost:3002 (admin/admin123)
4. **Jaeger**: http://localhost:16686
5. **RabbitMQ**: http://localhost:15672 (guest/guest)
6. **Prometheus**: http://localhost:9090

---

### Step 2: Place an Order (Customer App)

**URL:** http://localhost:3000

1. You should see the menu with items:
   - Cheeseburger - €4.00
   - Pommes - €3.50
   - Cola - €2.50
   - etc.

2. **Select items:**
   - Click "+" next to Cheeseburger (2x)
   - Click "+" next to Pommes (1x)
   - Click "+" next to Cola (1x)

3. **Review cart:**
   - Should show: "Total: €14.00" (or similar)
   - Items count should be correct

4. **Place order:**
   - Click "Order Now" button
   - You should be redirected to Success page

5. **Note the Order Number:**
   - Success page shows: "Order #42" (example)
   - **Write this down!** You'll need it later

---

### Step 3: Check Stock Reservation (Logs)

**Terminal Command:**

```bash
# Check Stock service logs for reservation
docker logs stock-prod 2>&1 | grep -i "reserved" | tail -5
```

**Expected Output:**
```json
{"level":"info","msg":"items reserved","order_id":"...","items_count":3}
```

**What happened:**
- ✅ Stock Service reserved items in PostgreSQL
- ✅ `reserved_quantity` incremented for each item
- ✅ Ensures stock is available before payment

---

### Step 4: Simulate Payment (Stripe Test)

Since we don't have a real Stripe account configured, we'll simulate the payment webhook:

**Option A: Use Stripe Test Card (if configured)**

1. Click the payment link on Success page
2. Use Stripe test card:
   - Card: `4242 4242 4242 4242`
   - Expiry: Any future date (e.g., `12/34`)
   - CVC: Any 3 digits (e.g., `123`)
   - ZIP: Any 5 digits (e.g., `12345`)

**Option B: Simulate Webhook (Manual)**

```bash
# Get your Order ID from Success page (e.g., "69287b1f950ffc1cc341a8fb")
ORDER_ID="YOUR_ORDER_ID_HERE"
CUSTOMER_ID="test-customer-123"

# Send Stripe webhook
curl -X POST http://localhost:8082/webhook/stripe \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "checkout.session.completed",
    "data": {
      "object": {
        "id": "cs_test_'$ORDER_ID'",
        "payment_status": "paid",
        "metadata": {
          "orderID": "'$ORDER_ID'",
          "customerID": "'$CUSTOMER_ID'"
        }
      }
    }
  }'
```

**Expected Result:**
```
Order status updated successfully
```

---

### Step 5: Verify Kitchen Display

**URL:** http://localhost:3001

1. **Refresh the page** (F5)

2. **You should see your order:**
   - Order Number: #42 (your number)
   - Items: Cheeseburger x2, Pommes x1, Cola x1
   - Status: "Preparing" or "New"
   - Timestamp

3. **Check Kitchen logs:**
   ```bash
   docker logs kitchen-prod 2>&1 | grep "received order.paid" | tail -3
   ```

**What happened:**
- ✅ Payments Service published `order.paid` event to RabbitMQ
- ✅ Kitchen Service consumed the event
- ✅ Order appears on Kitchen Display

---

### Step 6: Verify Stock Confirmation

**Terminal Command:**

```bash
# Check Stock service logs for confirmation
docker logs stock-prod 2>&1 | grep "order.paid" | tail -5
```

**Expected Output:**
```json
{"level":"info","msg":"received order.paid message","order_id":"...","trace_id":"..."}
{"level":"info","msg":"stock reservation confirmed","order_id":"..."}
```

**What happened:**
- ✅ Stock Service consumed `order.paid` event
- ✅ Confirmed reservation (decremented actual stock)
- ✅ Updated reservation status to "confirmed"

---

### Step 7: Mark Order as Ready (Kitchen Display)

**URL:** http://localhost:3001

1. **Find your order** in the list

2. **Click "Mark as Ready"** button

3. **Verify status changed:**
   - Order status should update to "Ready"
   - Order might move to a different section

4. **Check Orders service logs:**
   ```bash
   docker logs orders-prod 2>&1 | grep "status.*ready" | tail -3
   ```

**What happened:**
- ✅ Kitchen Display called Gateway API
- ✅ Gateway called Orders Service (gRPC)
- ✅ Order status updated in MongoDB

---

### Step 8: Verify Trace ID Correlation

**Goal:** Verify that all services logged the same Trace ID for this order.

**Terminal Commands:**

```bash
# 1. Get Trace ID from Gateway logs
TRACE_ID=$(docker logs gateway-prod 2>&1 | grep $ORDER_ID | grep trace_id | tail -1 | jq -r '.trace_id')

echo "Trace ID: $TRACE_ID"

# 2. Check if same Trace ID appears in Orders logs
docker logs orders-prod 2>&1 | grep $TRACE_ID

# 3. Check if same Trace ID appears in Stock logs
docker logs stock-prod 2>&1 | grep $TRACE_ID
```

**Expected Result:**
- ✅ Same Trace ID in Gateway, Orders, and Stock logs
- ✅ Proves distributed tracing is working!

---

### Step 9: View Trace in Jaeger

**URL:** http://localhost:16686

1. **Select Service:** "gateway" (dropdown)

2. **Click "Find Traces"**

3. **Find your order:**
   - Look for recent traces
   - Click on the trace with your Order ID in tags

4. **View complete trace:**
   - Should show spans from:
     - Gateway (HTTP)
     - Orders (gRPC)
     - Stock (gRPC)
   - All with the same Trace ID

5. **Verify timing:**
   - See how long each service took
   - Identify bottlenecks (if any)

**What you see:**
```
Gateway: POST /api/customers/.../orders [200ms]
  └─ Orders.CreateOrder [150ms]
      └─ Stock.ReserveItems [100ms]
          └─ PostgreSQL Query [50ms]
```

---

### Step 10: Check Metrics in Prometheus

**URL:** http://localhost:9090

1. **Query technical metrics:**

   ```promql
   # Total HTTP requests
   sum(http_requests_total)

   # Request rate per second
   rate(http_requests_total[5m])

   # Error rate
   sum(rate(http_requests_total{code=~"5.."}[5m]))
   ```

2. **Query business metrics** (if implemented):

   ```promql
   # Total revenue
   sum(order_revenue_euros_total)

   # Items sold
   sum(items_sold_total)

   # Orders created
   orders_total
   ```

---

### Step 11: View Dashboards in Grafana

**URL:** http://localhost:3002
**Login:** admin / admin123

1. **Go to Dashboards**

2. **Open "OMS - Business Metrics"**

3. **Verify panels show data:**
   - Orders per Minute
   - Success Rate
   - Average Response Time
   - Request Rate by Service

4. **Refresh dashboard** to see latest data

---

### Step 12: Verify RabbitMQ Message Flow

**URL:** http://localhost:15672
**Login:** guest / guest

1. **Click "Queues" tab**

2. **Check exchanges:**
   - `order.created` - Should show message flow
   - `order.paid` - Should show message flow

3. **Verify consumers:**
   - Kitchen consumer should be connected
   - Stock consumer should be connected

4. **Check message rates:**
   - Publish rate should show activity
   - Delivery rate should show consumption

---

## ✅ Success Criteria

Your E2E test is successful if:

- ✅ Order was created in Customer App
- ✅ Stock was reserved (logs show "reserved")
- ✅ Payment webhook was processed
- ✅ Order appeared on Kitchen Display
- ✅ Stock was confirmed (logs show "confirmed")
- ✅ Order was marked as ready
- ✅ Trace ID appeared in multiple service logs
- ✅ Jaeger shows complete trace
- ✅ Prometheus metrics are being collected
- ✅ Grafana dashboards show data
- ✅ RabbitMQ shows message flow

---

## 🐛 Troubleshooting

### Order doesn't appear on Kitchen Display

**Check:**
1. RabbitMQ is running: `docker ps | grep rabbitmq`
2. Kitchen service is running: `docker ps | grep kitchen`
3. Kitchen logs: `docker logs kitchen-prod 2>&1 | tail -50`
4. RabbitMQ queues: http://localhost:15672 → Queues

**Solution:**
- Restart Kitchen service: `docker compose -f docker-compose.prod.yml restart kitchen`

---

### Stock not reserved

**Check:**
1. Stock service logs: `docker logs stock-prod 2>&1 | tail -50`
2. PostgreSQL connection: `docker logs stock-prod 2>&1 | grep -i "database"`
3. Redis connection: `docker logs stock-prod 2>&1 | grep -i "redis"`

**Solution:**
- Check PostgreSQL: `docker exec -it postgres-prod psql -U stock -d stock -c "SELECT * FROM items LIMIT 5;"`

---

### Trace ID not appearing

**Check:**
1. OpenTelemetry initialized: `docker logs gateway-prod 2>&1 | grep "tracer"`
2. OTEL Collector running: `docker ps | grep otel`
3. Jaeger running: `docker ps | grep jaeger`

**Solution:**
- Restart services: `docker compose -f docker-compose.prod.yml restart gateway orders stock`

---

### Metrics not in Prometheus

**Check:**
1. Prometheus targets: http://localhost:9090/targets
2. Service `/metrics` endpoint: `curl http://localhost:9001/metrics`
3. Prometheus config: `docker logs prometheus-prod 2>&1 | grep ERROR`

**Solution:**
- Verify scrape config in `observability/prometheus.yml`
- Restart Prometheus: `docker compose -f docker-compose.prod.yml restart prometheus`

---

## 📊 Performance Benchmarks

**Expected timings:**

| Operation | Expected Time | Acceptable Max |
|-----------|---------------|----------------|
| Create Order | 100-300ms | 500ms |
| Stock Reservation | 50-150ms | 300ms |
| Payment Webhook | 50-100ms | 200ms |
| Kitchen Display Update | 1-3s | 5s |
| Order Status Update | 50-150ms | 300ms |

---

## 🎉 Congratulations!

If all steps passed, your Order Management System is working perfectly! 🚀

**Next Steps:**
1. Run automated E2E test: `./scripts/e2e-test.sh`
2. Implement Business Metrics (see `BUSINESS_METRICS_IMPLEMENTATION.md`)
3. Set up CI/CD pipeline
4. Deploy to Kubernetes

---

**Questions or Issues?**

Check the logs:
```bash
docker compose -f docker-compose.prod.yml logs -f
```

Or open issues in the repository.
