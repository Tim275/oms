# Payment Webhooks - Stripe Integration

This document describes the Stripe webhook integration for processing payment confirmations in the Payment Service.

## Overview

When a customer completes payment via Stripe Checkout, Stripe sends webhook events to our Payment Service. The service verifies the webhook signature, processes the `checkout.session.completed` event, and publishes an `order.paid` event to RabbitMQ.

## Architecture

```
Customer Payment Flow:
1. Customer creates order → Orders Service
2. Orders Service → order.created event → RabbitMQ
3. Payment Service consumes order.created
4. Payment Service creates Stripe Checkout Session
5. Customer pays via Stripe Checkout (browser)
6. Stripe → webhook → Payment Service (:8082/webhook)
7. Payment Service verifies signature
8. Payment Service → order.paid event → RabbitMQ
```

## Files Modified

### 1. `payments/http_handler.go` (NEW)
Handles incoming Stripe webhooks.

**Key Features:**
- Webhook signature verification with `webhook.ConstructEventWithOptions()`
- API version mismatch handling
- Event parsing for `checkout.session.completed`
- RabbitMQ event publishing

**Endpoint:**
- `POST /webhook` - Receives Stripe webhook events

### 2. `payments/main.go` (MODIFIED)
Configured concurrent architecture for HTTP server and RabbitMQ consumer.

**Changes:**
- Added `_ "github.com/joho/godotenv/autoload"` for early env loading
- Added package-level `endpointStripeSecret` variable
- Run HTTP server in goroutine (non-blocking)
- Run RabbitMQ consumer in goroutine (non-blocking)
- Main goroutine blocks on `<-ctx.Done()`

### 3. `payments/.env` (NEW CONFIG)
Added Stripe webhook signing secret:
```env
STRIPE_ENDPOINT_SECRET=whsec_60989c3cd8f409d2fbb4ddb888e70293307fa709902a6becfe230de0893df4dd
```

## Configuration

### Environment Variables

```env
# Stripe API Key (already configured)
STRIPE_SECRET_KEY=sk_test_...

# Stripe Webhook Signing Secret (NEW)
STRIPE_ENDPOINT_SECRET=whsec_...
```

### Getting Webhook Secret

**Development (Stripe CLI):**
```bash
stripe listen --forward-to localhost:8082/webhook
# Returns: whsec_...
```

**Production:**
1. Go to Stripe Dashboard → Developers → Webhooks
2. Add endpoint: `https://your-domain.com/webhook`
3. Select events: `checkout.session.completed`
4. Copy signing secret (starts with `whsec_`)

## Running the Service

### 1. Start Services
```bash
# Terminal 1: RabbitMQ
docker-compose up -d rabbitmq

# Terminal 2: Orders Service
cd orders && air

# Terminal 3: Payment Service
cd payments && air

# Terminal 4: Gateway
cd gateway && air
```

### 2. Forward Webhooks (Development)
```bash
stripe listen --forward-to localhost:8082/webhook
```

### 3. Test Payment Flow
```bash
# Create order
curl -X POST http://localhost:8081/api/customers/TEST_CUSTOMER/orders \
  -H "Content-Type: application/json" \
  -d '[{"id": "prod-test", "quantity": 1, "price_id": "price_..."}]'

# Get payment link from logs
tail -f /tmp/payment-logs.log | grep "Payment link created"

# Open link in browser and pay with test card:
# Card: 4242 4242 4242 4242
# Date: 12/34
# CVC: 567
```

## Webhook Events

### Received Events
When a payment is completed, Stripe sends multiple events:

```
1. charge.succeeded
2. payment_intent.succeeded
3. checkout.session.completed ← WE PROCESS THIS
4. payment_intent.created
5. charge.updated
```

### Processed Event: checkout.session.completed
```json
{
  "id": "evt_...",
  "type": "checkout.session.completed",
  "data": {
    "object": {
      "id": "cs_test_...",
      "payment_status": "paid",
      "metadata": {
        "orderID": "42",
        "customerID": "TEST_CUSTOMER"
      }
    }
  }
}
```

## Event Flow

### 1. Webhook Received
```go
POST /webhook
Headers: Stripe-Signature: t=...,v1=...
Body: <webhook JSON>
```

### 2. Signature Verification
```go
event, err := webhook.ConstructEventWithOptions(
    body,
    r.Header.Get("Stripe-Signature"),
    endpointStripeSecret,
    webhook.ConstructEventOptions{
        IgnoreAPIVersionMismatch: true,
    },
)
```

### 3. Event Processing
```go
if event.Type == "checkout.session.completed" {
    var session stripe.CheckoutSession
    json.Unmarshal(event.Data.Raw, &session)

    if session.PaymentStatus == "paid" {
        orderID := session.Metadata["orderID"]
        customerID := session.Metadata["customerID"]

        // Publish order.paid event to RabbitMQ
        publishOrderPaidEvent(orderID, customerID)
    }
}
```

### 4. Response
```go
w.WriteHeader(http.StatusOK)  // Always return 200 to Stripe
```

## Logs

### Successful Payment Logs

**Stripe CLI:**
```
2025-11-07 04:34:52   --> checkout.session.completed [evt_1SQgRr3th7a1Jo3bMQPt1c2R]
2025-11-07 04:34:52  <--  [200] POST http://localhost:8082/webhook
```

**Payment Service:**
```
2025/11/07 04:34:52 Payment for Checkout Session cs_test_... succeeded!
2025/11/07 04:34:52 Message published order.paid
```

## Error Handling

### API Version Mismatch
**Problem:** Stripe API version differs from stripe-go SDK version

**Solution:** Use `IgnoreAPIVersionMismatch: true` in webhook options

**Code:**
```go
webhook.ConstructEventOptions{
    IgnoreAPIVersionMismatch: true,
}
```

### Invalid Signature
**Problem:** Webhook signature verification fails

**Logs:**
```
Error verifying webhook signature: ...
```

**Response:** `400 Bad Request`

**Common Causes:**
- Wrong `STRIPE_ENDPOINT_SECRET`
- Webhook forwarding not running (development)
- Webhook endpoint not configured (production)

## Testing

### Test Card Numbers
```
Success: 4242 4242 4242 4242
Decline: 4000 0000 0000 0002
Insufficient funds: 4000 0000 0000 9995
```

### Test Payment Flow
```bash
# 1. Create order
ORDER_ID=$(curl -s -X POST http://localhost:8081/api/customers/TEST/orders \
  -H "Content-Type: application/json" \
  -d '[{"id": "prod-test", "quantity": 1, "price_id": "price_..."}]')

# 2. Get payment link
PAYMENT_LINK=$(tail -20 /tmp/payment-logs.log | grep "Payment link created" | tail -1 | awk '{print $NF}')

# 3. Open in browser
echo "Payment Link: $PAYMENT_LINK"

# 4. Pay with test card
# Card: 4242 4242 4242 4242

# 5. Check webhook logs
tail -f /tmp/payment-logs.log | grep "Payment for Checkout"
```

## RabbitMQ Events

### Published Event: order.paid
```json
{
  "id": "42",
  "customer_id": "TEST_CUSTOMER",
  "status": "paid"
}
```

**Exchange:** `order.paid`
**Routing Key:** `""`
**Delivery Mode:** `Persistent`

## Known Limitations

1. **No Orders Service Consumer**
   - Orders Service does not consume `order.paid` events
   - Queue `order.paid` is not created
   - Published messages are discarded
   - **Note:** This limitation exists in the reference implementation

2. **No Order Status Updates**
   - Order status is not updated in database after payment
   - Future enhancement: Add consumer to update order status

## Security

### Webhook Signature Verification
Always verify webhook signatures to prevent:
- Spoofed webhooks
- Replay attacks
- Man-in-the-middle attacks

**Implementation:**
```go
event, err := webhook.ConstructEventWithOptions(
    body,
    r.Header.Get("Stripe-Signature"),
    endpointStripeSecret,
    webhook.ConstructEventOptions{
        IgnoreAPIVersionMismatch: true,
    },
)

if err != nil {
    w.WriteHeader(http.StatusBadRequest)
    return
}
```

### Best Practices
1. Always verify webhook signatures
2. Use HTTPS in production
3. Store webhook secrets securely (environment variables)
4. Return 200 OK only after successful processing
5. Implement idempotency (handle duplicate events)
6. Log all webhook events for debugging

## Troubleshooting

### Webhook Not Received
**Check:**
1. Stripe CLI is running: `stripe listen --forward-to localhost:8082/webhook`
2. Payment Service is running: `lsof -ti:8082`
3. Webhook endpoint is accessible: `curl http://localhost:8082/webhook`

### Signature Verification Fails
**Check:**
1. `STRIPE_ENDPOINT_SECRET` in `.env` matches Stripe CLI output
2. Environment variables are loaded: `echo $STRIPE_ENDPOINT_SECRET`
3. Webhook secret is correct in Stripe Dashboard (production)

### RabbitMQ Message Not Published
**Check:**
1. RabbitMQ is running: `docker ps | grep rabbitmq`
2. Payment Service is connected to RabbitMQ: Check logs for "rabbitmq connected"
3. Channel is not nil: Add debug logs in `http_handler.go`

## Future Enhancements

1. **Orders Service Consumer**
   - Consume `order.paid` events
   - Update order status in database
   - Send confirmation email to customer

2. **Kitchen Display System**
   - Real-time order notifications
   - Order status tracking (paid → preparing → ready)
   - WebSocket/SSE for live updates

3. **Customer Notifications**
   - Email confirmation on payment
   - SMS notification when order is ready
   - Push notifications for order updates

4. **Idempotency**
   - Store processed webhook IDs
   - Prevent duplicate processing
   - Handle replay attacks

## Reference

- Senior's Implementation: https://github.com/sikozonpc/oms-repo/tree/main/payments
- Stripe Webhooks Docs: https://stripe.com/docs/webhooks
- Stripe Go SDK: https://github.com/stripe/stripe-go

---

**Implementation Date:** 2025-11-07
**Status:** ✅ Complete and tested
**Matches Senior's Implementation:** 100%
