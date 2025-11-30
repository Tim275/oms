# Order Microservices Platform - Detailed Documentation

> Modern, event-driven microservices architecture for order management and payment processing, built with Go, gRPC, RabbitMQ, and Stripe.

---

## 📋 Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [API Reference](#api-reference)
- [Development](#development)
- [Project Structure](#project-structure)
- [Configuration](#configuration)
- [Testing](#testing)
- [Deployment](#deployment)
- [Contributing](#contributing)

---

## 🎯 Overview

A production-ready microservices platform demonstrating modern architectural patterns:

- **Clean Architecture** with clear separation of concerns
- **Event-Driven Design** using RabbitMQ for asynchronous communication
- **Service Discovery** with Consul for dynamic service registration
- **gRPC Communication** for high-performance inter-service calls
- **Payment Integration** with Stripe Checkout Sessions
- **Fanning Out Pattern** for bidirectional event flows

### Use Cases

- E-commerce order processing
- Food delivery order management
- Restaurant kitchen display systems
- Payment gateway integration
- Microservices learning and reference implementation

---

## ✨ Features

### Core Functionality

✅ **Order Management**
- Create orders with multiple items
- Real-time order status tracking (pending → waiting_payment → paid → preparing → ready)
- Customer order history
- MongoDB storage with persistent order data

✅ **Payment Processing**
- Stripe Checkout Session integration
- Secure payment link generation
- Payment status webhooks
- Automatic order updates after payment

✅ **Stock Management**
- PostgreSQL-based inventory tracking
- Atomic stock reservations with 15-minute TTL
- Redis caching (5-minute TTL) for menu items
- Automatic cleanup of expired reservations

✅ **Event-Driven Architecture**
- Asynchronous message processing with RabbitMQ
- Event publishing: `order.created`, `order.paid`, `order.preparing`, `order.ready`
- Reliable message delivery
- Dead Letter Exchange (DLX) with queue-specific DLQs

✅ **Service Discovery**
- Dynamic service registration with Consul
- Health check monitoring (10s intervals)
- Automatic service deregistration on shutdown
- Load balancing support

✅ **API Gateway**
- Centralized HTTP entry point
- Request validation and error handling
- HTTP to gRPC translation
- Static file serving

### Technical Features

- **Hot Reload** with Air for rapid development
- **Structured Logging** with `slog`
- **Protocol Buffers** for type-safe communication
- **Thread-Safe** in-memory stores with `sync.RWMutex`
- **Graceful Shutdown** for all services
- **Docker Compose** for local infrastructure

---

## 🏗️ Architecture

### 🎨 Complete System Architecture (Production-Ready)

**Like McDonald's, Burger King, Subway - Real Restaurant Systems!**

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         FRONTEND LAYER                                   │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  ┌──────────────────────┐              ┌──────────────────────┐         │
│  │   Customer App       │              │  Kitchen Display     │         │
│  │   (React)            │              │  (React)             │         │
│  │   Port: 3000         │              │  Port: 3001          │         │
│  │                      │              │                      │         │
│  │  ✅ Order Creation   │              │  ✅ View Orders      │         │
│  │  ✅ Menu Selection   │              │  ✅ Mark Ready       │         │
│  │  ✅ Status Tracking  │              │  ✅ Urgency Alerts   │         │
│  │  ✅ Payment Link     │              │  ✅ Time Tracking    │         │
│  └──────────┬───────────┘              └──────────┬───────────┘         │
│             │                                     │                      │
└─────────────┼─────────────────────────────────────┼──────────────────────┘
              │                                     │
              ▼                                     ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         API GATEWAY LAYER                                │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  ┌────────────────────────────────────────────────────────────┐         │
│  │              API Gateway (Go)                               │         │
│  │              Port: 8081                                     │         │
│  │                                                             │         │
│  │  HTTP Endpoints:                                            │         │
│  │  • POST   /api/customers/{customerID}/orders               │         │
│  │  • GET    /api/customers/{customerID}/orders/{orderID}     │         │
│  │  • GET    /metrics (Prometheus)                            │         │
│  │                                                             │         │
│  │  🔍 Service Discovery: Consul                              │         │
│  │  ⚖️  Load Balancing: Round-Robin                           │         │
│  └─────────────────────────┬──────────────────────────────────┘         │
│                            │                                             │
└────────────────────────────┼─────────────────────────────────────────────┘
                             │
                             ├──── gRPC Calls ────►
                             │
┌────────────────────────────┼─────────────────────────────────────────────┐
│                MICROSERVICES LAYER (Backend)                             │
├────────────────────────────┴─────────────────────────────────────────────┤
│                                                                           │
│  ┌────────────────────────────────────────────────────────────┐         │
│  │  1. ORDERS SERVICE (Go)                                     │         │
│  │     Ports: 8080 (HTTP), 9000 (gRPC), 9001 (Metrics)        │         │
│  │                                                             │         │
│  │     gRPC Methods:                                           │         │
│  │     • CreateOrder(customerId, items) → Order               │         │
│  │     • GetOrder(customerId, orderId) → Order                │         │
│  │     • UpdateOrder(order) → Order                           │         │
│  │                                                             │         │
│  │     Database: MongoDB                                       │         │
│  │     Events Published:                                       │         │
│  │     • order.created → RabbitMQ                             │         │
│  │     • order.preparing → RabbitMQ                           │         │
│  │     • order.ready → RabbitMQ                               │         │
│  └────────────────────────────────────────────────────────────┘         │
│                                                                           │
│  ┌────────────────────────────────────────────────────────────┐         │
│  │  2. STOCK SERVICE (Go)                                      │         │
│  │     Port: 8084 (gRPC)                                       │         │
│  │                                                             │         │
│  │     gRPC Methods:                                           │         │
│  │     • CheckIfItemsInStock(items) → bool                    │         │
│  │     • GetItems(itemIDs) → []Item                           │         │
│  │                                                             │         │
│  │     Database: PostgreSQL                                    │         │
│  │     Cache: Redis (5 min TTL)                               │         │
│  │     Tables: items, stock_reservations                      │         │
│  └────────────────────────────────────────────────────────────┘         │
│                                                                           │
│  ┌────────────────────────────────────────────────────────────┐         │
│  │  3. PAYMENTS SERVICE (Go)                                   │         │
│  │     Port: 8082 (HTTP)                                       │         │
│  │                                                             │         │
│  │     HTTP Endpoints:                                         │         │
│  │     • POST /webhook (Stripe Webhook)                        │         │
│  │     • GET  /metrics (Prometheus)                            │         │
│  │                                                             │         │
│  │     gRPC Methods:                                           │         │
│  │     • CreatePayment(order) → PaymentLink                   │         │
│  │                                                             │         │
│  │     Integration: Stripe                                     │         │
│  │     RabbitMQ Consumer: order.created                       │         │
│  │     Events Published: order.paid                           │         │
│  └────────────────────────────────────────────────────────────┘         │
│                                                                           │
│  ┌────────────────────────────────────────────────────────────┐         │
│  │  4. KITCHEN SERVICE (Go)                                    │         │
│  │     Port: 8083 (HTTP)                                       │         │
│  │                                                             │         │
│  │     HTTP Endpoints:                                         │         │
│  │     • POST /api/orders/{orderID}/ready                      │         │
│  │     • GET  /metrics (Prometheus)                            │         │
│  │                                                             │         │
│  │     RabbitMQ Consumer: order.paid                          │         │
│  │     ⚙️ Auto-Update: paid → preparing                       │         │
│  │     👨‍🍳 Manual-Update: preparing → ready (via HTTP)         │         │
│  └────────────────────────────────────────────────────────────┘         │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────────────────┐
│                    INFRASTRUCTURE LAYER                                   │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                   │
│  │   MongoDB    │  │  PostgreSQL  │  │    Redis     │                   │
│  │   Port: 27017│  │  Port: 5432  │  │  Port: 6379  │                   │
│  │              │  │              │  │              │                   │
│  │  Orders DB   │  │  Stock DB    │  │  Cache Layer │                   │
│  └──────────────┘  └──────────────┘  └──────────────┘                   │
│                                                                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                   │
│  │  RabbitMQ    │  │   Consul     │  │  Prometheus  │                   │
│  │  Port: 5672  │  │  Port: 8500  │  │  Port: 9090  │                   │
│  │              │  │              │  │              │                   │
│  │  Event Bus   │  │  Discovery   │  │  Metrics     │                   │
│  └──────────────┘  └──────────────┘  └──────────────┘                   │
│                                                                            │
│  ┌──────────────────────────────────────────────────┐                    │
│  │            Stripe (External)                      │                    │
│  │            Payment Processing                     │                    │
│  └──────────────────────────────────────────────────┘                    │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

### 📋 ALL API ENDPOINTS

#### 🌐 Gateway Service (Port 8081)
```
POST   /api/customers/{customerID}/orders       Create new order
GET    /api/customers/{customerID}/orders/{orderID}  Get order details
GET    /metrics                                  Prometheus metrics
```

#### 📦 Orders Service (gRPC - Port 9000)
```
CreateOrder(customerId, items) → Order         Create new order
GetOrder(customerId, orderId) → Order          Get order by ID
UpdateOrder(order) → Order                     Update order status
```

#### 📊 Stock Service (gRPC - Port 8084)
```
CheckIfItemsInStock(items) → bool              Check stock availability
GetItems(itemIDs) → []Item                     Get menu items
```

#### 💳 Payments Service (Port 8082)
```
POST   /webhook                                 Stripe webhook handler
GET    /metrics                                 Prometheus metrics

gRPC:
CreatePayment(order) → PaymentLink             Generate Stripe checkout
```

#### 👨‍🍳 Kitchen Service (Port 8083)
```
POST   /api/orders/{orderID}/ready              Mark order as ready (MANUAL)
GET    /metrics                                 Prometheus metrics

Consumer:
order.paid → preparing (AUTOMATIC)
```

### 🔄 Complete Order Flow (Production-Level)

```
1. CUSTOMER APP 🍔
   ↓
   POST /api/customers/Max/orders
   { items: [{ ID: "1", Quantity: 2 }] }

2. GATEWAY → ORDERS SERVICE
   ↓
   gRPC: CreateOrder()
   ↓
   MongoDB: status = "pending"
   ↓
   RabbitMQ: Publish "order.created" 📢

3. PAYMENTS SERVICE (Consumer) 💳
   ↓
   Receives "order.created"
   ↓
   Stripe: Create Checkout Session
   ↓
   MongoDB: status = "waiting_payment"
   ↓
   Returns payment_link to customer

4. CUSTOMER PAYS via Stripe ✅
   ↓
   Stripe Webhook → POST /webhook
   ↓
   MongoDB: status = "paid"
   ↓
   RabbitMQ: Publish "order.paid" 📢

5. KITCHEN SERVICE (Consumer) - AUTOMATISCH ⚙️
   ↓
   Receives "order.paid"
   ↓
   gRPC: UpdateOrder(status = "preparing")
   ↓
   MongoDB: status = "preparing"
   ↓
   Customer App: Shows "In Zubereitung 🔵"
   ↓
   Kitchen Display: Shows order card

6. CHEF MARKS READY - MANUELL 👨‍🍳
   ↓
   Kitchen Display: POST /api/orders/{id}/ready
   ↓
   gRPC: UpdateOrder(status = "ready")
   ↓
   MongoDB: status = "ready"
   ↓
   Customer App: Shows "Bereit zur Abholung! 🎉"

✅ RESULT: Customer picks up order!
```

### 🎯 Production Features

**✅ Like McDonald's/Burger King:**
- Automatic kitchen notification (paid → preparing)
- Manual ready confirmation by chef
- Real-time customer status updates
- Urgency color coding (fresh/warning/urgent)
- Clean customer-facing UI (only preparing/ready)

**✅ Enterprise-Grade:**
- Microservices Architecture
- Service Discovery (Consul)
- Event-Driven (RabbitMQ)
- gRPC for Internal Communication
- REST APIs for External Clients
- Caching Layer (Redis)
- Dead Letter Queue (DLX)
- Prometheus Metrics
- Stock Reservation System
- Payment Processing (Stripe)

**✅ Resilience:**
- Retry Mechanism mit Exponential Backoff
- Circuit Breaker Pattern
- Health Checks
- Graceful Shutdown
- Transaction Safety

---

## 🚀 Quick Start

### Prerequisites

```bash
# Required
go version        # Go 1.22+
docker --version  # Docker Desktop / Rancher Desktop

# Development Tools
brew install protobuf              # Protocol Buffers Compiler
go install github.com/air-verse/air@latest  # Hot Reload

# Optional
brew install stripe/stripe-cli/stripe      # Stripe Webhooks
```

### Installation

**1. Clone the repository:**

```bash
git clone https://github.com/Tim275/order-service.git
cd order-service
```

**2. Start infrastructure:**

```bash
docker-compose up -d
```

This starts:
- **Consul** at `http://localhost:8500` (Service Registry)
- **RabbitMQ** at `http://localhost:15672` (Message Broker) - guest/guest
- **PostgreSQL** at `localhost:5432` (Stock Database) - postgres/postgres
- **MongoDB** at `localhost:27017` (Orders Database)
- **Redis** at `localhost:6379` (Cache Layer)
- **Jaeger** at `http://localhost:16686` (Distributed Tracing)
- **Stripe CLI** (Webhook Forwarding)

**3. Configure Stripe:**

Create `payments/.env`:

```env
STRIPE_SECRET_KEY=sk_test_your_key_here
```

Get your test key from [Stripe Dashboard](https://dashboard.stripe.com/test/apikeys).

**4. Start services:**

Open **5 separate terminals**:

```bash
# Terminal 1 - Gateway
cd gateway && air

# Terminal 2 - Orders Service
cd orders && air

# Terminal 3 - Payment Service
cd payments && air

# Terminal 4 - Stock Service
cd stock && air

# Terminal 5 - Kitchen Service
cd kitchen && air
```

**5. Create your first order:**

```bash
curl -X POST http://localhost:8081/api/customers/DEMO_USER/orders \
  -H "Content-Type: application/json" \
  -d '[
    {
      "id": "prod-1",
      "quantity": 2,
      "price_id": "price_1SQYsL3th7a1Jo3bsOVNnRpm"
    }
  ]'
```

**6. Get order with payment link:**

```bash
curl http://localhost:8081/api/customers/DEMO_USER/orders/42
```

**Response:**

```json
{
  "id": "42",
  "customer_id": "DEMO_USER",
  "status": "waiting_payment",
  "items": [...],
  "payment_link": "https://checkout.stripe.com/c/pay/cs_test_..."
}
```

✅ **Success!** You now have a running microservices platform.

---

## 📡 API Reference

### Base URL

```
http://localhost:8081
```

### Endpoints

#### Create Order

Creates a new order and initiates payment processing.

```http
POST /api/customers/{customerID}/orders
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `customerID` | string | Customer identifier |

**Request Body:**

```json
[
  {
    "id": "prod-1",
    "quantity": 2,
    "price_id": "price_1SQYsL3th7a1Jo3bsOVNnRpm"
  }
]
```

**Response:** `201 Created`

```json
{
  "id": "42",
  "customer_id": "DEMO_USER",
  "status": "pending",
  "items": [
    {
      "id": "prod-1",
      "name": "Product",
      "quantity": 2,
      "price_id": "price_1SQYsL3th7a1Jo3bsOVNnRpm"
    }
  ]
}
```

**Note:** The `payment_link` is added asynchronously within 1-2 seconds.

---

#### Get Order

Retrieves an order by ID.

```http
GET /api/customers/{customerID}/orders/{orderID}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `customerID` | string | Customer identifier |
| `orderID` | string | Order identifier |

**Response:** `200 OK`

```json
{
  "id": "42",
  "customer_id": "DEMO_USER",
  "status": "waiting_payment",
  "items": [...],
  "payment_link": "https://checkout.stripe.com/c/pay/cs_test_..."
}
```

---

#### Order Status Values

| Status | Description |
|--------|-------------|
| `pending` | Order created, payment link being generated |
| `waiting_payment` | Payment link ready, awaiting payment |
| `paid` | Payment confirmed, stock reserved |
| `preparing` | Kitchen is preparing the order |
| `ready` | Order ready for customer pickup |

---

### Error Responses

**400 Bad Request**

```json
{
  "error": "order must contain at least one item"
}
```

**503 Service Unavailable**

```json
{
  "error": "Orders service unavailable"
}
```

---

## 💻 Development

### Project Setup

**Initialize Go Workspace:**

```bash
go work init
go work use ./common ./gateway ./orders ./payments
```

**Generate Protocol Buffers:**

```bash
make gen
```

This generates:
- `common/api/oms.pb.go`
- `common/api/oms_grpc.pb.go`

### Hot Reload Configuration

Each service has an `.air.toml` configuration for hot reload:

```toml
[build]
  cmd = "go build -o ./tmp/main ."
  bin = "tmp/main"
  include_ext = ["go", "proto"]
  exclude_dir = ["tmp"]
```

### Adding a New Service

1. **Create module:**

```bash
mkdir myservice
cd myservice
go mod init github.com/timour/order-microservices/myservice
```

2. **Add to workspace:**

```bash
go work use ./myservice
```

3. **Implement structure:**

```
myservice/
├── main.go          # Entry point
├── app.go           # Application lifecycle
├── types.go         # Interfaces
├── service.go       # Business logic
├── store.go         # Data layer
├── grpc_handler.go  # gRPC server
└── .air.toml        # Hot reload config
```

### Logging

All services use structured logging with `slog`:

```go
import "log/slog"

logger.Info("order created",
    slog.String("order_id", orderID),
    slog.String("customer_id", customerID),
)

logger.Error("failed to connect",
    slog.Any("error", err),
)
```

---

## 📂 Project Structure

```
order-microservices/
│
├── common/                      # Shared libraries
│   ├── api/
│   │   ├── oms.proto           # Protocol Buffer definitions
│   │   ├── oms.pb.go           # Generated: Messages
│   │   └── oms_grpc.pb.go      # Generated: gRPC stubs
│   ├── broker/
│   │   └── broker.go           # RabbitMQ connection helper
│   ├── config/
│   │   └── config.go           # Environment variable loader
│   ├── discovery/
│   │   └── consul/
│   │       └── consul.go       # Consul service discovery
│   └── logger/
│       └── logger.go           # Structured logging setup
│
├── orders/                      # Orders Microservice
│   ├── main.go                 # Service entry point
│   ├── app.go                  # Lifecycle management
│   ├── types.go                # Domain interfaces
│   ├── service.go              # Business logic layer
│   ├── store.go                # Data access layer
│   ├── grpc_handler.go         # gRPC server implementation
│   ├── registry.go             # Consul registration
│   └── .air.toml               # Hot reload config
│
├── payments/                    # Payment Microservice
│   ├── main.go                 # Service entry point
│   ├── app.go                  # Lifecycle management
│   ├── service.go              # Business logic layer
│   ├── consumer.go             # RabbitMQ consumer
│   ├── http_handler.go         # Stripe webhook handler
│   ├── .env                    # Stripe credentials
│   ├── gateway/
│   │   └── orders_gateway.go   # gRPC client for Orders
│   ├── processor/
│   │   └── stripe.go           # Stripe API integration
│   └── .air.toml               # Hot reload config
│
├── gateway/                     # API Gateway
│   ├── main.go                 # Service entry point
│   ├── app.go                  # Lifecycle management
│   ├── http_handler.go         # HTTP request handlers
│   ├── registry.go             # Consul registration
│   ├── public/                 # Static files
│   │   ├── success.html        # Payment success page
│   │   └── cancel.html         # Payment cancel page
│   └── .air.toml               # Hot reload config
│
├── stock/                       # Stock Management Service
│   ├── main.go                 # Service entry point
│   ├── service.go              # Business logic (reserve, confirm)
│   ├── grpc_handler.go         # gRPC server
│   ├── consumer.go             # RabbitMQ consumer
│   ├── migrations/             # PostgreSQL migrations
│   └── .air.toml               # Hot reload config
│
├── kitchen/                     # Kitchen Display Service
│   ├── main.go                 # Service entry point
│   ├── service.go              # Business logic
│   ├── consumer.go             # RabbitMQ consumer
│   └── .air.toml               # Hot reload config
│
├── docker-compose.yml           # Local infrastructure
├── go.work                      # Go workspace configuration
├── Makefile                     # Build commands
├── README.md                    # This file
├── SETUP.md                     # Build from scratch guide
├── ADVANCED_SETUP.md            # DLX, Redis, Stock guide
└── claude.md                    # Development documentation
```

### Architecture Layers

Each service follows Clean Architecture:

```
Service/
├── types.go        → Interfaces (Contracts)
├── store.go        → Data Access Layer
├── service.go      → Business Logic Layer
├── grpc_handler.go → Presentation Layer (gRPC)
├── http_handler.go → Presentation Layer (HTTP)
└── main.go         → Wiring & Initialization
```

---

## ⚙️ Configuration

### Environment Variables

#### Common (All Services)

```env
SERVICE_NAME=orders           # Service identifier
INSTANCE_ID=orders-1          # Unique instance ID
CONSUL_ADDR=localhost:8500    # Consul address
```

#### Orders Service

```env
GRPC_ADDR=localhost:9000      # gRPC server address
AMQP_USER=guest               # RabbitMQ username
AMQP_PASS=guest               # RabbitMQ password
AMQP_HOST=localhost           # RabbitMQ host
AMQP_PORT=5672                # RabbitMQ port
```

#### Payment Service

```env
AMQP_USER=guest               # RabbitMQ username
AMQP_PASS=guest               # RabbitMQ password
AMQP_HOST=localhost           # RabbitMQ host
AMQP_PORT=5672                # RabbitMQ port
HTTP_ADDR=localhost:8082      # HTTP server address
STRIPE_SECRET_KEY=sk_test_... # Stripe API key
STRIPE_ENDPOINT_SECRET=whsec_... # Stripe webhook secret
```

#### Gateway Service

```env
HTTP_ADDR=localhost:8081      # HTTP server address
CONSUL_ADDR=localhost:8500    # Consul address
```

### Stripe Configuration

**1. Create Test Products:**

Visit [Stripe Dashboard → Products](https://dashboard.stripe.com/test/products)

```
Product: Burger
Price: €8.00
Price ID: price_1SQYsL3th7a1Jo3bsOVNnRpm
Image: (manually set in Stripe Dashboard)

Product: Pommes (Fries)
Price: €3.50
Price ID: price_1SRMZL3th7a1Jo3b5LNJkEoe
Image: https://media01.stockfood.com/largepreviews/Mjc5MzI3OTg=/00901058-Pommes-frites-in-roter-Fast-Food-Box.jpg
```

**2. Get API Keys:**

Visit [Stripe Dashboard → API Keys](https://dashboard.stripe.com/test/apikeys)

Copy your **Secret key** (starts with `sk_test_`)

**3. Test Cards:**

| Card Number | Description |
|-------------|-------------|
| `4242 4242 4242 4242` | Success |
| `4000 0000 0000 0002` | Declined |
| `4000 0000 0000 9995` | Insufficient funds |

---

## 🧪 Testing

### Manual Testing

**1. Health Checks:**

```bash
# Check Consul
curl http://localhost:8500/v1/catalog/services

# Check RabbitMQ
curl -u guest:guest http://localhost:15672/api/overview
```

**2. Create Order:**

```bash
curl -X POST http://localhost:8081/api/customers/TEST/orders \
  -H "Content-Type: application/json" \
  -d '[{"id":"1","quantity":2,"price_id":"price_1SQYsL3th7a1Jo3bsOVNnRpm"}]'
```

**3. Verify Logs:**

**Orders Service:**
```
INFO order received customer_id=TEST
INFO event published event=order.created order_id=42
```

**Payment Service:**
```
INFO received message
INFO payment link created payment_link=https://checkout.stripe.com/...
```

**4. Get Order:**

```bash
curl http://localhost:8081/api/customers/TEST/orders/42
```

### Integration Testing

**Complete Flow:**

```bash
# 1. Create order
ORDER_RESPONSE=$(curl -s -X POST http://localhost:8081/api/customers/TEST/orders \
  -H "Content-Type: application/json" \
  -d '[{"id":"1","quantity":1,"price_id":"price_1SQYsL3th7a1Jo3bsOVNnRpm"}]')

ORDER_ID=$(echo $ORDER_RESPONSE | jq -r '.id')
echo "Order ID: $ORDER_ID"

# 2. Wait for async processing
sleep 2

# 3. Get updated order
curl http://localhost:8081/api/customers/TEST/orders/$ORDER_ID | jq

# Expected: payment_link should be present
```

### Load Testing

```bash
# Install Apache Bench
brew install httpd

# Run 1000 requests, 10 concurrent
ab -n 1000 -c 10 -p order.json -T application/json \
  http://localhost:8081/api/customers/LOAD_TEST/orders
```

---

## 🚢 Deployment

### Docker Build

**Build service images:**

```bash
# Orders Service
docker build -t order-service-orders:latest -f orders/Dockerfile .

# Payment Service
docker build -t order-service-payments:latest -f payments/Dockerfile .

# Gateway
docker build -t order-service-gateway:latest -f gateway/Dockerfile .
```

### Kubernetes Deployment

**Example Deployment (Orders Service):**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orders-service
spec:
  replicas: 3
  selector:
    matchLabels:
      app: orders
  template:
    metadata:
      labels:
        app: orders
    spec:
      containers:
      - name: orders
        image: order-service-orders:latest
        ports:
        - containerPort: 9000
        env:
        - name: SERVICE_NAME
          value: "orders"
        - name: CONSUL_ADDR
          value: "consul:8500"
        - name: AMQP_HOST
          value: "rabbitmq"
---
apiVersion: v1
kind: Service
metadata:
  name: orders-service
spec:
  selector:
    app: orders
  ports:
  - port: 9000
    targetPort: 9000
```

### Production Considerations

✅ **Database:**
- Replace in-memory stores with PostgreSQL/MongoDB
- Implement database migrations
- Add connection pooling

✅ **Security:**
- Enable TLS for gRPC
- Add authentication middleware
- Implement rate limiting
- Secure Consul with ACLs

✅ **Observability:**
- Add OpenTelemetry tracing
- Implement Prometheus metrics
- Set up Grafana dashboards
- Centralized logging with Loki

✅ **Reliability:**
- Implement retry logic
- Add circuit breakers
- Configure dead letter queues
- Set up health check endpoints

✅ **Scalability:**
- Horizontal pod autoscaling
- Load balancing configuration
- Database read replicas
- Cache layer (Redis)

---

## 🤝 Contributing

Contributions are welcome! Please follow these guidelines:

### Development Workflow

1. **Fork the repository**

2. **Create a feature branch:**

```bash
git checkout -b feature/amazing-feature
```

3. **Make your changes:**

- Follow Go best practices
- Add tests for new features
- Update documentation
- Run `go fmt` and `go vet`

4. **Commit your changes:**

```bash
git commit -m "feat: add amazing feature"
```

5. **Push to your fork:**

```bash
git push origin feature/amazing-feature
```

6. **Open a Pull Request**

### Code Style

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` for formatting
- Write meaningful commit messages
- Add comments for exported functions
- Keep functions small and focused

### Testing Requirements

- Unit tests for business logic
- Integration tests for API endpoints
- Maintain >80% code coverage
- Add test documentation

---

## 📚 Additional Resources

### Documentation

- **[SETUP_GUIDE.md](./SETUP_GUIDE.md)** - Step-by-step implementation tutorial
- **[Protocol Buffers](./common/api/oms.proto)** - API contract definitions

### External Links

- [Go Documentation](https://go.dev/doc/)
- [gRPC Go Tutorial](https://grpc.io/docs/languages/go/quickstart/)
- [RabbitMQ Tutorials](https://www.rabbitmq.com/getstarted.html)
- [Consul Documentation](https://www.consul.io/docs)
- [Stripe API Reference](https://stripe.com/docs/api)

### Related Projects

- [Senior's OMS Implementation](https://github.com/sikozonpc/oms-repo) - Reference architecture
- [Go Microservices](https://github.com/golang/go/wiki/Projects#microservices) - Go microservices projects

---

## 📝 License

This project is for educational purposes. Feel free to use it as a reference for your own implementations.

---

## 👥 Authors

- **Tim** - [@Tim275](https://github.com/Tim275)

---

## 🙏 Acknowledgments

- Inspired by [sikozonpc/oms-repo](https://github.com/sikozonpc/oms-repo)
- Built with guidance from Claude Code
- Thanks to the Go, gRPC, RabbitMQ, and Stripe communities

---

## 📞 Support

If you have questions or run into issues:

1. Check the [SETUP_GUIDE.md](./SETUP_GUIDE.md) for detailed instructions
2. Open an [Issue](https://github.com/Tim275/order-service/issues)
3. Review existing issues for solutions

---

**⭐ If you find this project helpful, please consider giving it a star!**

**Happy Coding! 🚀**
