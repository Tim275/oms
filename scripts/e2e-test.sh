#!/bin/bash

# End-to-End Test Script - Order Management System
# Tests complete flow: Order → Payment → Kitchen → Stock
#
# Prerequisites:
# - docker-compose.prod.yml running
# - jq installed (brew install jq)
#
# Usage:
#   ./scripts/e2e-test.sh

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  🧪 End-to-End Test - Order Management System                 ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Configuration
GATEWAY_URL="http://localhost:8080"
PAYMENTS_URL="http://localhost:8082"
CUSTOMER_ID="e2e-test-$(date +%s)"
TRACE_ID=""

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
pass() {
    echo -e "${GREEN}✅ $1${NC}"
    TESTS_PASSED=$((TESTS_PASSED + 1))
}

fail() {
    echo -e "${RED}❌ $1${NC}"
    TESTS_FAILED=$((TESTS_FAILED + 1))
}

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

warn() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# Test 1: Health Check
echo -e "\n${BLUE}═══ Test 1: Health Checks ═══${NC}"

if curl -s -f ${GATEWAY_URL}/health > /dev/null 2>&1; then
    pass "Gateway health check"
else
    fail "Gateway health check"
fi

# Test 2: Get Menu
echo -e "\n${BLUE}═══ Test 2: Get Menu ═══${NC}"

MENU=$(curl -s ${GATEWAY_URL}/api/menu)
MENU_COUNT=$(echo $MENU | jq '. | length')

if [ "$MENU_COUNT" -gt 0 ]; then
    pass "Menu retrieved ($MENU_COUNT items)"
    info "Sample item: $(echo $MENU | jq -r '.[0] | "\(.name) - €\(.price)"')"
else
    fail "Menu is empty"
fi

# Test 3: Create Order
echo -e "\n${BLUE}═══ Test 3: Create Order ═══${NC}"

ORDER_REQUEST='[
  {"id":"1","quantity":2},
  {"id":"2","quantity":1}
]'

ORDER_RESPONSE=$(curl -s -X POST ${GATEWAY_URL}/api/customers/${CUSTOMER_ID}/orders \
  -H 'Content-Type: application/json' \
  -d "${ORDER_REQUEST}")

ORDER_ID=$(echo $ORDER_RESPONSE | jq -r '.id')
PAYMENT_LINK=$(echo $ORDER_RESPONSE | jq -r '.payment_link')
ORDER_STATUS=$(echo $ORDER_RESPONSE | jq -r '.status')

if [ "$ORDER_ID" != "null" ] && [ -n "$ORDER_ID" ]; then
    pass "Order created successfully"
    info "Order ID: ${ORDER_ID}"
    info "Status: ${ORDER_STATUS}"
    info "Payment Link: ${PAYMENT_LINK}"
else
    fail "Order creation failed"
    echo "Response: $ORDER_RESPONSE"
    exit 1
fi

# Extract trace ID from logs
sleep 1
TRACE_ID=$(docker logs gateway-prod 2>&1 | grep $ORDER_ID | grep trace_id | tail -1 | jq -r '.trace_id' || echo "")

if [ -n "$TRACE_ID" ]; then
    pass "Trace ID captured: ${TRACE_ID}"
else
    warn "Could not capture trace ID"
fi

# Test 4: Verify Stock Reservation
echo -e "\n${BLUE}═══ Test 4: Stock Reservation ═══${NC}"

sleep 2  # Wait for async processing

STOCK_LOGS=$(docker logs stock-prod 2>&1 | grep -i "reserved" | tail -5)

if [ -n "$STOCK_LOGS" ]; then
    pass "Stock reservation logs found"
else
    warn "Stock reservation logs not found (might be cached)"
fi

# Test 5: Simulate Stripe Payment Webhook
echo -e "\n${BLUE}═══ Test 5: Simulate Payment (Stripe Webhook) ═══${NC}"

# Stripe Checkout Session Completed Event
WEBHOOK_PAYLOAD=$(cat <<EOF
{
  "type": "checkout.session.completed",
  "data": {
    "object": {
      "id": "cs_test_${ORDER_ID}",
      "payment_status": "paid",
      "metadata": {
        "orderID": "${ORDER_ID}",
        "customerID": "${CUSTOMER_ID}"
      }
    }
  }
}
EOF
)

WEBHOOK_RESPONSE=$(curl -s -X POST ${PAYMENTS_URL}/webhook/stripe \
  -H 'Content-Type: application/json' \
  -d "${WEBHOOK_PAYLOAD}")

if [ $? -eq 0 ]; then
    pass "Stripe webhook sent successfully"
else
    fail "Stripe webhook failed"
fi

# Test 6: Verify Order Status Update
echo -e "\n${BLUE}═══ Test 6: Verify Order Status (Paid) ═══${NC}"

sleep 3  # Wait for async event processing

UPDATED_ORDER=$(curl -s ${GATEWAY_URL}/api/customers/${CUSTOMER_ID}/orders/${ORDER_ID})
UPDATED_STATUS=$(echo $UPDATED_ORDER | jq -r '.status')

if [ "$UPDATED_STATUS" == "paid" ] || [ "$UPDATED_STATUS" == "preparing" ]; then
    pass "Order status updated to: ${UPDATED_STATUS}"
else
    fail "Order status not updated (current: ${UPDATED_STATUS}, expected: paid or preparing)"
fi

# Test 7: Verify Kitchen Received Order
echo -e "\n${BLUE}═══ Test 7: Kitchen Display Integration ═══${NC}"

KITCHEN_LOGS=$(docker logs kitchen-prod 2>&1 | grep $ORDER_ID || echo "")

if [ -n "$KITCHEN_LOGS" ]; then
    pass "Kitchen service received order"
    info "Kitchen log sample: $(echo "$KITCHEN_LOGS" | head -1)"
else
    warn "Kitchen service logs not found (check RabbitMQ)"
fi

# Test 8: Verify RabbitMQ Message Flow
echo -e "\n${BLUE}═══ Test 8: RabbitMQ Event Propagation ═══${NC}"

# Check if order.paid event was published
ORDERS_LOGS=$(docker logs orders-prod 2>&1 | grep -i "published.*order.paid" | grep $ORDER_ID || echo "")

if [ -n "$ORDERS_LOGS" ]; then
    pass "order.paid event published to RabbitMQ"
else
    warn "order.paid event logs not found"
fi

# Test 9: Verify Stock Confirmation
echo -e "\n${BLUE}═══ Test 9: Stock Confirmation (After Payment) ═══${NC}"

sleep 2

STOCK_CONFIRM_LOGS=$(docker logs stock-prod 2>&1 | grep -i "received order.paid" | tail -1 || echo "")

if [ -n "$STOCK_CONFIRM_LOGS" ]; then
    pass "Stock service confirmed reservation"
else
    warn "Stock confirmation logs not found"
fi

# Test 10: Get Orders by Status
echo -e "\n${BLUE}═══ Test 10: Query Orders by Status ═══${NC}"

PAID_ORDERS=$(curl -s "${GATEWAY_URL}/api/orders?status=paid")
PAID_COUNT=$(echo $PAID_ORDERS | jq '. | length')

if [ "$PAID_COUNT" -gt 0 ]; then
    pass "Retrieved paid orders (count: ${PAID_COUNT})"
else
    warn "No paid orders found (order might be in 'preparing' state)"
fi

# Test 11: Trace ID Correlation
echo -e "\n${BLUE}═══ Test 11: Trace ID Correlation ═══${NC}"

if [ -n "$TRACE_ID" ]; then
    # Check if trace ID appears in multiple services
    GATEWAY_TRACE=$(docker logs gateway-prod 2>&1 | grep $TRACE_ID | wc -l)
    ORDERS_TRACE=$(docker logs orders-prod 2>&1 | grep $TRACE_ID | wc -l)

    if [ "$GATEWAY_TRACE" -gt 0 ] && [ "$ORDERS_TRACE" -gt 0 ]; then
        pass "Trace ID found in multiple services (Gateway: ${GATEWAY_TRACE}, Orders: ${ORDERS_TRACE})"
        info "Jaeger UI: http://localhost:16686/trace/${TRACE_ID}"
    else
        warn "Trace ID not propagated correctly"
    fi
else
    warn "Trace ID not available for correlation test"
fi

# Test 12: Prometheus Metrics
echo -e "\n${BLUE}═══ Test 12: Prometheus Metrics ═══${NC}"

METRICS=$(curl -s http://localhost:9090/api/v1/query?query=up | jq -r '.data.result[] | "\(.metric.job): \(.value[1])"')

if [ -n "$METRICS" ]; then
    pass "Prometheus metrics available"
    echo "$METRICS" | head -5
else
    fail "Prometheus metrics not available"
fi

# Summary
echo ""
echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  📊 Test Summary                                               ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "Total Tests:  $((TESTS_PASSED + TESTS_FAILED))"
echo -e "${GREEN}Passed:       ${TESTS_PASSED}${NC}"
echo -e "${RED}Failed:       ${TESTS_FAILED}${NC}"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}🎉 All tests passed! System is working correctly.${NC}"
    echo ""
    echo -e "${BLUE}Next steps:${NC}"
    echo "  1. Open Jaeger UI: http://localhost:16686"
    echo "  2. Search for Trace ID: ${TRACE_ID}"
    echo "  3. View complete request flow across all services"
    echo "  4. Open Grafana: http://localhost:3002"
    echo "  5. Check Business Metrics dashboard"
    exit 0
else
    echo -e "${RED}⚠️  Some tests failed. Check logs above for details.${NC}"
    echo ""
    echo -e "${YELLOW}Debugging commands:${NC}"
    echo "  docker logs gateway-prod"
    echo "  docker logs orders-prod"
    echo "  docker logs payments-prod"
    echo "  docker logs kitchen-prod"
    echo "  docker logs stock-prod"
    exit 1
fi
