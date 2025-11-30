# SETUP Part 3: Stock Service, Stripe & Production Features (Step-by-Step)

> **Production-Ready System** - PostgreSQL, Redis, Stripe, Health Checks & Deployment

---

## 📚 Was haben wir bis jetzt?

**Part 1 & 2 Resultat:**
- ✅ Orders Service (CreateOrder, UpdateOrder, GetOrder)
- ✅ Gateway (HTTP → gRPC mit Service Discovery)
- ✅ Consul (Service Discovery)
- ✅ RabbitMQ (order.created Event)
- ✅ Payments Service (Consumer - ohne echte Payments)

**Was fehlt noch?**
- ❌ Stripe Integration (echte Payment Links!)
- ❌ Stock Service (Inventory Check)
- ❌ Kitchen Service (Kitchen Display)
- ❌ Production Features (Auto-Reconnect, Health Checks)
- ❌ Deployment (Docker Compose)

---

## 🎯 Part 3 Roadmap

1. **Stripe Integration** (Payment Links erstellen)
2. **Stock Service Minimal** (In-Memory Check)
3. **Stock Service PostgreSQL** (Real Database)
4. **Stock Service Redis Cache** (Performance)
5. **Kitchen Service** (order.paid Consumer)
6. **Production Features** (ResilientConnection, Health Checks)
7. **Docker Compose** (Full Stack Deployment)

---

## 💳 Phase 9: Stripe Integration (Payment Links)

### Step 9.1: WARUM Stripe?

Aktuell:
- Payments Service consumed "order.created"
- Logged nur "Processing payment"
- ❌ Kein echter Payment Link!

**Jetzt:**
- Stripe Checkout Session erstellen
- Payment Link zurück an Order speichern
- Customer kann bezahlen

### Step 9.2: Stripe Account Setup

1. **Account erstellen:** https://dashboard.stripe.com/register
2. **Test Keys holen:** https://dashboard.stripe.com/test/apikeys
   - Secret Key: `sk_test_...`
   - Publishable Key: `pk_test_...`

3. **Environment Variable setzen:**
```bash
export STRIPE_SECRET_KEY="sk_test_..."
```

### Step 9.3: Stripe Package installieren

```bash
cd payments
go get github.com/stripe/stripe-go/v76
```

### Step 9.4: Payments Service erweitern

**Datei:** `payments/types.go` (NEU!)

```go
package main

import "context"

type PaymentsService interface {
	CreatePaymentLink(ctx context.Context, orderID string, items []Item) (string, error)
}

type Item struct {
	ID       string
	Quantity int32
}
```

**Datei:** `payments/service.go` (NEU!)

```go
package main

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
)

type service struct {
	stripeKey string
}

func NewService(stripeKey string) *service {
	stripe.Key = stripeKey
	return &service{stripeKey: stripeKey}
}

// CreatePaymentLink: Stripe Checkout Session erstellen
func (s *service) CreatePaymentLink(ctx context.Context, orderID string, items []Item) (string, error) {
	// Line Items für Stripe
	var lineItems []*stripe.CheckoutSessionLineItemParams
	for _, item := range items {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String("eur"),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(fmt.Sprintf("Item %s", item.ID)),
				},
				UnitAmount: stripe.Int64(1000), // 10.00 EUR (in cents)
			},
			Quantity: stripe.Int64(int64(item.Quantity)),
		})
	}

	// Fallback: Wenn keine Items, 1 Default Item
	if len(lineItems) == 0 {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String("eur"),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String("Order " + orderID),
				},
				UnitAmount: stripe.Int64(1000),
			},
			Quantity: stripe.Int64(1),
		})
	}

	// Checkout Session erstellen
	params := &stripe.CheckoutSessionParams{
		Mode:      stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: lineItems,
		SuccessURL: stripe.String("http://localhost:3000/success?order_id=" + orderID),
		CancelURL:  stripe.String("http://localhost:3000/cancel"),
		Metadata: map[string]string{
			"order_id": orderID,
		},
	}

	sess, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("failed to create stripe session: %w", err)
	}

	return sess.URL, nil
}
```

### Step 9.5: Consumer aktualisieren

**Datei:** `payments/consumer.go`

```go
package main

import (
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	service PaymentsService
}

func NewConsumer(service PaymentsService) *Consumer {
	return &Consumer{service: service}
}

func (c *Consumer) Listen(ch *amqp.Channel) {
	q, err := ch.QueueDeclare(
		"payments_order_created",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	err = ch.QueueBind(
		q.Name,
		"order.created",
		"order.created",
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to bind queue: %v", err)
	}

	msgs, err := ch.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to consume: %v", err)
	}

	log.Println("🎧 Listening for order.created events...")

	for msg := range msgs {
		var event map[string]interface{}
		if err := json.Unmarshal(msg.Body, &event); err != nil {
			log.Printf("❌ Failed to unmarshal: %v", err)
			msg.Nack(false, false)
			continue
		}

		orderID, _ := event["order_id"].(string)

		// Parse Items
		var items []Item
		if itemsRaw, ok := event["items"].([]interface{}); ok {
			for _, itemRaw := range itemsRaw {
				if itemMap, ok := itemRaw.(map[string]interface{}); ok {
					items = append(items, Item{
						ID:       itemMap["item_id"].(string),
						Quantity: int32(itemMap["quantity"].(float64)),
					})
				}
			}
		}

		// Create Stripe Payment Link
		paymentLink, err := c.service.CreatePaymentLink(msg.Context, orderID, items)
		if err != nil {
			log.Printf("❌ Failed to create payment link: %v", err)
			msg.Nack(false, true) // Requeue
			continue
		}

		log.Printf("💳 Payment link created: %s", paymentLink)
		log.Printf("🔗 Order %s → %s", orderID, paymentLink)

		// TODO: Update Order in Orders Service mit payment_link

		msg.Ack(false)
	}
}
```

**Datei:** `payments/main.go` (Update)

```go
package main

import (
	"log"
	"os"

	"github.com/timour/order-microservices/common/broker"
)

func main() {
	// Stripe Key aus ENV
	stripeKey := os.Getenv("STRIPE_SECRET_KEY")
	if stripeKey == "" {
		log.Fatal("❌ STRIPE_SECRET_KEY not set")
	}

	// RabbitMQ Connection
	ch, closeRabbitMQ, err := broker.Connect("guest", "guest", "localhost", "5672")
	if err != nil {
		log.Fatalf("❌ Failed to connect: %v", err)
	}
	defer closeRabbitMQ()
	log.Println("✅ Connected to RabbitMQ")

	// Service
	svc := NewService(stripeKey)
	log.Println("✅ Stripe service initialized")

	// Consumer
	consumer := NewConsumer(svc)
	consumer.Listen(ch)
}
```

### ✅ Test Phase 9

**Payments Service starten:**
```bash
cd payments
STRIPE_SECRET_KEY=sk_test_... go run *.go
```

**Expected Output:**
```
✅ Connected to RabbitMQ
✅ Stripe service initialized
🎧 Listening for order.created events...
```

**Order erstellen:**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "cust_123",
    "items": [
      {"item_id": "burger", "quantity": 2}
    ]
  }'
```

**Payments Service Logs:**
```
💳 Payment link created: https://checkout.stripe.com/c/pay/cs_test_...
🔗 Order order_1 → https://checkout.stripe.com/c/pay/cs_test_...
```

**Test Payment Link:**
- Link im Browser öffnen
- Stripe Checkout Seite sollte erscheinen
- Test Card: `4242 4242 4242 4242`

**🎯 CHECKPOINT:** Stripe Payment Links funktionieren!

---

## 📦 Phase 10: Stock Service (Inventory Check)

### Step 10.1: WARUM Stock Service?

**Problem:**
- Orders Service erstellt Order OHNE zu prüfen ob genug Stock da ist
- ❌ Overselling möglich!
- ❌ Customer bestellt 100 Burger, aber nur 10 auf Lager

**Lösung:**
- Stock Service prüft Verfügbarkeit BEVOR Order erstellt wird
- Orders Service ruft Stock Service auf (gRPC)
- Nur wenn Stock verfügbar → Order erstellen

### Step 10.2: Stock Service Minimal (In-Memory)

**Warum In-Memory zuerst?**
- PostgreSQL Setup dauert
- Wir testen erst die Integration
- Später: 2 Zeilen ändern → PostgreSQL

**Datei:** `stock/types.go`

```go
package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
)

type StockService interface {
	CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error)
	GetItems(ctx context.Context) ([]*api.Item, error)
}

type StockStore interface {
	GetItems(ctx context.Context) ([]*api.Item, error)
	CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error)
}
```

**Datei:** `stock/store.go`

```go
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/timour/order-microservices/common/api"
)

type store struct {
	items map[string]*api.Item // item_id → Item
	mu    sync.RWMutex
}

func NewStore() *store {
	// Initial Inventory
	items := map[string]*api.Item{
		"burger": {
			Id:       "burger",
			Name:     "Burger",
			Quantity: 100,
			PriceId:  "price_burger",
		},
		"fries": {
			Id:       "fries",
			Name:     "Pommes",
			Quantity: 150,
			PriceId:  "price_fries",
		},
		"drink": {
			Id:       "drink",
			Name:     "Getränk",
			Quantity: 200,
			PriceId:  "price_drink",
		},
	}

	return &store{items: items}
}

// GetItems: Alle Items zurückgeben
func (s *store) GetItems(ctx context.Context) ([]*api.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var items []*api.Item
	for _, item := range s.items {
		items = append(items, item)
	}

	return items, nil
}

// CheckIfItemsInStock: Prüft ob genug Stock vorhanden
func (s *store) CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*api.Item

	for _, reqItem := range items {
		stockItem, ok := s.items[reqItem.ItemId]
		if !ok {
			return nil, fmt.Errorf("item not found: %s", reqItem.ItemId)
		}

		if stockItem.Quantity < reqItem.Quantity {
			return nil, fmt.Errorf("insufficient stock for %s: requested %d, available %d",
				reqItem.ItemId, reqItem.Quantity, stockItem.Quantity)
		}

		// Return Item mit requested Quantity
		result = append(result, &api.Item{
			Id:       stockItem.Id,
			Name:     stockItem.Name,
			Quantity: reqItem.Quantity,
			PriceId:  stockItem.PriceId,
		})
	}

	return result, nil
}
```

**Datei:** `stock/service.go`

```go
package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
)

type service struct {
	store StockStore
}

func NewService(store StockStore) *service {
	return &service{store: store}
}

func (s *service) GetItems(ctx context.Context) ([]*api.Item, error) {
	return s.store.GetItems(ctx)
}

func (s *service) CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error) {
	return s.store.CheckIfItemsInStock(ctx, items)
}
```

### Step 10.3: Protobuf für Stock Service

**Datei:** `common/api/oms.proto` (erweitern)

```proto
// ... (existing messages) ...

// StockService - Inventory Management
service StockService {
    rpc GetItems(google.protobuf.Empty) returns (GetItemsResponse);
    rpc CheckIfItemsInStock(CheckStockRequest) returns (CheckStockResponse);
}

message GetItemsResponse {
    repeated Item items = 1;
}

message CheckStockRequest {
    repeated ItemWithQuantity items = 1;
}

message CheckStockResponse {
    repeated Item items = 1;
}

// Empty message (oder google.protobuf.Empty nutzen)
```

**Code generieren:**
```bash
cd common
make gen
```

### Step 10.4: Stock gRPC Handler

**Datei:** `stock/grpc_handler.go`

```go
package main

import (
	"context"
	"log"

	"github.com/timour/order-microservices/common/api"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type grpcHandler struct {
	api.UnimplementedStockServiceServer
	service StockService
}

func NewGRPCHandler(grpcServer *grpc.Server, service StockService) {
	handler := &grpcHandler{
		service: service,
	}
	api.RegisterStockServiceServer(grpcServer, handler)
	log.Println("✅ Stock gRPC handler registered")
}

func (h *grpcHandler) GetItems(ctx context.Context, req *emptypb.Empty) (*api.GetItemsResponse, error) {
	items, err := h.service.GetItems(ctx)
	if err != nil {
		return nil, err
	}

	return &api.GetItemsResponse{
		Items: items,
	}, nil
}

func (h *grpcHandler) CheckIfItemsInStock(ctx context.Context, req *api.CheckStockRequest) (*api.CheckStockResponse, error) {
	items, err := h.service.CheckIfItemsInStock(ctx, req.Items)
	if err != nil {
		return nil, err
	}

	return &api.CheckStockResponse{
		Items: items,
	}, nil
}
```

**Datei:** `stock/main.go`

```go
package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
)

func main() {
	// Store & Service
	store := NewStore()
	log.Println("✅ Stock store initialized")

	svc := NewService(store)
	log.Println("✅ Stock service initialized")

	// gRPC Server
	grpcServer := grpc.NewServer()
	NewGRPCHandler(grpcServer, svc)

	// Server starten
	lis, err := net.Listen("tcp", ":9003")
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Println("🚀 Stock Service listening on :9003")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("❌ Failed to serve: %v", err)
	}
}
```

### ✅ Test Phase 10

**Stock Service starten:**
```bash
cd stock
go mod init github.com/timour/order-microservices/stock
go mod tidy
go run *.go
```

**Test GetItems:**
```bash
grpcurl -plaintext localhost:9003 api.StockService/GetItems
```

**Expected Response:**
```json
{
  "items": [
    {"id": "burger", "name": "Burger", "quantity": 100, "priceId": "price_burger"},
    {"id": "fries", "name": "Pommes", "quantity": 150, "priceId": "price_fries"},
    {"id": "drink", "name": "Getränk", "quantity": 200, "priceId": "price_drink"}
  ]
}
```

**Test CheckStock:**
```bash
grpcurl -plaintext \
  -d '{"items": [{"item_id": "burger", "quantity": 2}]}' \
  localhost:9003 \
  api.StockService/CheckIfItemsInStock
```

**Expected Response:**
```json
{
  "items": [
    {"id": "burger", "name": "Burger", "quantity": 2, "priceId": "price_burger"}
  ]
}
```

**🎯 CHECKPOINT:** Stock Service funktioniert!

---

## 🔗 Phase 11: Orders Service Integration mit Stock

### Step 11.1: WARUM JETZT?

Bis jetzt:
- Stock Service läuft
- Orders Service erstellt Orders OHNE Stock Check
- ❌ Overselling möglich!

**Jetzt:**
- Orders Service ruft Stock Service auf
- Nur wenn Stock verfügbar → Order erstellen
- ✅ Kein Overselling mehr!

### Step 11.2: Orders Service Gateway

**Datei:** `orders/gateway/stock.go` (NEU!)

```go
package gateway

import (
	"context"

	"github.com/timour/order-microservices/common/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type StockGateway struct {
	addr string
}

func NewStockGateway(addr string) *StockGateway {
	return &StockGateway{addr: addr}
}

// CheckIfItemsInStock: gRPC Call zu Stock Service
func (g *StockGateway) CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error) {
	conn, err := grpc.Dial(g.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := api.NewStockServiceClient(conn)

	resp, err := client.CheckIfItemsInStock(ctx, &api.CheckStockRequest{
		Items: items,
	})
	if err != nil {
		return nil, err
	}

	return resp.Items, nil
}
```

### Step 11.3: Orders Service erweitern

**Datei:** `orders/types.go` (erweitern)

```go
package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
)

type OrdersService interface {
	CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error)
	UpdateOrder(ctx context.Context, req *api.UpdateOrderRequest) (*api.Order, error)
	GetOrder(ctx context.Context, req *api.GetOrderRequest) (*api.Order, error)
}

type OrdersStore interface {
	Create(ctx context.Context, customerID string, items []*api.ItemWithQuantity) (string, error)
	Update(ctx context.Context, orderID, status string) error
	Get(ctx context.Context, orderID string) (*Order, error)
}

// StockGateway: Kommunikation mit Stock Service (NEU!)
type StockGateway interface {
	CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error)
}
```

**Datei:** `orders/service.go` (Update CreateOrder)

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/timour/order-microservices/common/api"
)

type service struct {
	store   OrdersStore
	gateway StockGateway  // NEU!
}

func NewService(store OrdersStore, gateway StockGateway) *service {
	return &service{
		store:   store,
		gateway: gateway,
	}
}

func (s *service) CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error) {
	if req.CustomerId == "" {
		return nil, fmt.Errorf("customer_id required")
	}

	// Stock Check (NEU!)
	if len(req.Items) > 0 {
		items, err := s.gateway.CheckIfItemsInStock(ctx, req.Items)
		if err != nil {
			log.Printf("❌ Stock check failed: %v", err)
			return nil, fmt.Errorf("stock check failed: %w", err)
		}
		log.Printf("✅ Stock check passed: %d items available", len(items))
	}

	// Create Order
	orderID, err := s.store.Create(ctx, req.CustomerId, req.Items)
	if err != nil {
		return nil, err
	}

	return &api.CreateOrderResponse{
		OrderId: orderID,
	}, nil
}

// UpdateOrder, GetOrder (unverändert)
func (s *service) UpdateOrder(ctx context.Context, req *api.UpdateOrderRequest) (*api.Order, error) {
	if req.OrderId == "" {
		return nil, fmt.Errorf("order_id required")
	}
	if req.Status == "" {
		return nil, fmt.Errorf("status required")
	}

	if err := s.store.Update(ctx, req.OrderId, req.Status); err != nil {
		return nil, err
	}

	order, err := s.store.Get(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}

	return &api.Order{
		Id:         order.ID,
		CustomerId: order.CustomerID,
		Status:     order.Status,
		Items:      order.Items,
	}, nil
}

func (s *service) GetOrder(ctx context.Context, req *api.GetOrderRequest) (*api.Order, error) {
	if req.OrderId == "" {
		return nil, fmt.Errorf("order_id required")
	}

	order, err := s.store.Get(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}

	return &api.Order{
		Id:         order.ID,
		CustomerId: order.CustomerID,
		Status:     order.Status,
		Items:      order.Items,
	}, nil
}
```

**Datei:** `orders/main.go` (Update)

```go
package main

import (
	"context"
	"log"
	"net"

	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/orders/gateway"
	"google.golang.org/grpc"
)

func main() {
	ctx := context.Background()

	// ... Consul Registration (optional) ...

	// RabbitMQ
	ch, closeRabbitMQ, err := broker.Connect("guest", "guest", "localhost", "5672")
	if err != nil {
		log.Fatalf("❌ Failed to connect to RabbitMQ: %v", err)
	}
	defer closeRabbitMQ()
	log.Println("✅ Connected to RabbitMQ")

	// Stock Gateway (NEU!)
	stockGateway := gateway.NewStockGateway("localhost:9003")
	log.Println("✅ Stock gateway initialized")

	// Store & Service
	store := NewStore()
	svc := NewService(store, stockGateway)  // MIT Stock Gateway!

	// gRPC Handler
	grpcServer := grpc.NewServer()
	NewGRPCHandler(grpcServer, svc, ch)

	// Server starten
	lis, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Println("🚀 Orders Service listening on :9000")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("❌ Failed to serve: %v", err)
	}
}
```

### ✅ Test Phase 11

**Test: Sufficient Stock**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "cust_123",
    "items": [
      {"item_id": "burger", "quantity": 2}
    ]
  }'
```

**Orders Service Logs:**
```
✅ Stock check passed: 1 items available
✅ Order created: order_1 (status: pending)
📨 Event published: order.created
```

**Test: Insufficient Stock**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "cust_123",
    "items": [
      {"item_id": "burger", "quantity": 200}
    ]
  }'
```

**Orders Service Logs:**
```
❌ Stock check failed: insufficient stock for burger: requested 200, available 100
```

**HTTP Response:**
```
HTTP/1.1 500 Internal Server Error
stock check failed: insufficient stock for burger: requested 200, available 100
```

**🎯 CHECKPOINT:** Stock Integration funktioniert! Kein Overselling mehr!

---

## 🗄️ Phase 12: Stock Service mit PostgreSQL

### Step 12.1: WARUM PostgreSQL?

In-Memory Store:
- ❌ Daten verloren bei Restart
- ❌ Nicht skalierbar
- ❌ Keine Transaktionen

**PostgreSQL:**
- ✅ Persistent Storage
- ✅ ACID Transactions
- ✅ Stock Reservations möglich

### Step 12.2: PostgreSQL starten

**Docker Compose:**
```yaml
# docker-compose.yml
services:
  postgres:
    image: postgres:15-alpine
    container_name: postgres
    ports:
      - "5432:5432"
    environment:
      POSTGRES_USER: stock
      POSTGRES_PASSWORD: stock123
      POSTGRES_DB: stock
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./stock/migrations/init.sql:/docker-entrypoint-initdb.d/init.sql

volumes:
  postgres_data:
```

**Datei:** `stock/migrations/init.sql`

```sql
-- Menu Items Table
CREATE TABLE IF NOT EXISTS items (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    price_id VARCHAR(255) NOT NULL,
    quantity INT NOT NULL DEFAULT 0
);

-- Initial Data
INSERT INTO items (id, name, price_id, quantity) VALUES
('burger', 'Burger', 'price_burger', 100),
('fries', 'Pommes', 'price_fries', 150),
('drink', 'Getränk', 'price_drink', 200)
ON CONFLICT (id) DO NOTHING;
```

**Starten:**
```bash
docker-compose up -d postgres
```

### Step 12.3: PostgreSQL Store

**Datei:** `stock/postgres_store.go` (NEU!)

```go
package main

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/timour/order-microservices/common/api"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(connStr string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// GetItems: Alle Items aus PostgreSQL
func (s *PostgresStore) GetItems(ctx context.Context) ([]*api.Item, error) {
	query := `SELECT id, name, price_id, quantity FROM items`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*api.Item
	for rows.Next() {
		var item api.Item
		if err := rows.Scan(&item.Id, &item.Name, &item.PriceId, &item.Quantity); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}

	return items, nil
}

// CheckIfItemsInStock: Prüft Verfügbarkeit
func (s *PostgresStore) CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error) {
	var result []*api.Item

	for _, reqItem := range items {
		var dbItem api.Item

		query := `SELECT id, name, price_id, quantity FROM items WHERE id = $1`
		err := s.db.QueryRowContext(ctx, query, reqItem.ItemId).Scan(
			&dbItem.Id,
			&dbItem.Name,
			&dbItem.PriceId,
			&dbItem.Quantity,
		)

		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("item not found: %s", reqItem.ItemId)
		}
		if err != nil {
			return nil, err
		}

		if dbItem.Quantity < reqItem.Quantity {
			return nil, fmt.Errorf("insufficient stock for %s: requested %d, available %d",
				reqItem.ItemId, reqItem.Quantity, dbItem.Quantity)
		}

		// Return mit requested quantity
		result = append(result, &api.Item{
			Id:       dbItem.Id,
			Name:     dbItem.Name,
			Quantity: reqItem.Quantity,
			PriceId:  dbItem.PriceId,
		})
	}

	return result, nil
}
```

**Datei:** `stock/main.go` (Update)

```go
package main

import (
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
)

func main() {
	// PostgreSQL Connection
	connStr := "postgres://stock:stock123@localhost:5432/stock?sslmode=disable"

	store, err := NewPostgresStore(connStr)
	if err != nil {
		log.Fatalf("❌ Failed to connect to PostgreSQL: %v", err)
	}
	defer store.Close()
	log.Println("✅ Connected to PostgreSQL")

	// Service
	svc := NewService(store)
	log.Println("✅ Stock service initialized")

	// gRPC Server
	grpcServer := grpc.NewServer()
	NewGRPCHandler(grpcServer, svc)

	// Server starten
	lis, err := net.Listen("tcp", ":9003")
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Println("🚀 Stock Service listening on :9003")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("❌ Failed to serve: %v", err)
	}
}
```

### ✅ Test Phase 12

**Install Dependencies:**
```bash
cd stock
go get github.com/lib/pq
```

**Stock Service starten:**
```bash
go run *.go
```

**Expected Output:**
```
✅ Connected to PostgreSQL
✅ Stock service initialized
🚀 Stock Service listening on :9003
```

**Test:**
```bash
grpcurl -plaintext localhost:9003 api.StockService/GetItems
```

**Data sollte aus PostgreSQL kommen!**

**🎯 CHECKPOINT:** PostgreSQL Integration funktioniert!

---

## ⚡ Phase 13: Redis Cache (Performance Boost)

### Step 13.1: WARUM Redis Cache?

**Problem:**
- GetItems wird SEHR oft aufgerufen (Menu anzeigen)
- Jedes Mal PostgreSQL Query
- Langsam! (100ms+)

**Lösung: Cache-Aside Pattern**
```
GetItems:
  1. Check Redis Cache
  2. Cache HIT? → Return ⚡ (10ms)
  3. Cache MISS? → Query PostgreSQL (100ms)
  4. Populate Cache (TTL: 5min)
  5. Return
```

### Step 13.2: Redis starten

**Docker Compose:**
```yaml
redis:
  image: redis:7-alpine
  container_name: redis
  ports:
    - "6379:6379"
```

```bash
docker-compose up -d redis
```

### Step 13.3: Redis Cache

**Datei:** `stock/cache.go` (NEU!)

```go
package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/timour/order-microservices/common/api"
)

type ItemCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewItemCache(addr string, ttl time.Duration) (*ItemCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &ItemCache{
		client: client,
		ttl:    ttl,
	}, nil
}

func (c *ItemCache) Close() error {
	return c.client.Close()
}

// Get: Items aus Cache
func (c *ItemCache) Get(ctx context.Context) ([]*api.Item, error) {
	data, err := c.client.Get(ctx, "menu:items").Result()
	if err == redis.Nil {
		return nil, nil // Cache MISS
	}
	if err != nil {
		return nil, err
	}

	var items []*api.Item
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		return nil, err
	}

	return items, nil
}

// Set: Items in Cache speichern
func (c *ItemCache) Set(ctx context.Context, items []*api.Item) error {
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, "menu:items", data, c.ttl).Err()
}

// Invalidate: Cache löschen
func (c *ItemCache) Invalidate(ctx context.Context) error {
	return c.client.Del(ctx, "menu:items").Err()
}
```

### Step 13.4: Cached Store Wrapper

**Datei:** `stock/cached_store.go` (NEU!)

```go
package main

import (
	"context"
	"log"

	"github.com/timour/order-microservices/common/api"
)

type CachedStore struct {
	store *PostgresStore
	cache *ItemCache
}

func NewCachedStore(store *PostgresStore, cache *ItemCache) *CachedStore {
	return &CachedStore{
		store: store,
		cache: cache,
	}
}

// GetItems: Cache-Aside Pattern
func (cs *CachedStore) GetItems(ctx context.Context) ([]*api.Item, error) {
	// 1. Try Cache
	items, err := cs.cache.Get(ctx)
	if err == nil && items != nil {
		log.Println("✅ Cache HIT (Redis)")
		return items, nil
	}

	// 2. Cache MISS → Query DB
	log.Println("⚠️  Cache MISS → Query PostgreSQL")
	items, err = cs.store.GetItems(ctx)
	if err != nil {
		return nil, err
	}

	// 3. Populate Cache
	cs.cache.Set(ctx, items)
	log.Println("✅ Cache populated")

	return items, nil
}

// CheckIfItemsInStock: Delegate (Cache ist hier nicht sinnvoll)
func (cs *CachedStore) CheckIfItemsInStock(ctx context.Context, items []*api.ItemWithQuantity) ([]*api.Item, error) {
	return cs.store.CheckIfItemsInStock(ctx, items)
}
```

**Datei:** `stock/main.go` (Update)

```go
package main

import (
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
)

func main() {
	// PostgreSQL
	connStr := "postgres://stock:stock123@localhost:5432/stock?sslmode=disable"
	store, err := NewPostgresStore(connStr)
	if err != nil {
		log.Fatalf("❌ Failed to connect to PostgreSQL: %v", err)
	}
	defer store.Close()
	log.Println("✅ Connected to PostgreSQL")

	// Redis Cache (NEU!)
	cache, err := NewItemCache("localhost:6379", 5*time.Minute)
	if err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	defer cache.Close()
	log.Println("✅ Connected to Redis (TTL: 5min)")

	// Cached Store (NEU!)
	cachedStore := NewCachedStore(store, cache)

	// Service mit Cached Store
	svc := NewService(cachedStore)
	log.Println("✅ Stock service initialized with cache")

	// ... gRPC Server (wie vorher) ...
}
```

### ✅ Test Phase 13

**Install Dependencies:**
```bash
go get github.com/redis/go-redis/v9
```

**Stock Service starten:**
```bash
go run *.go
```

**Test GetItems (First Call):**
```bash
grpcurl -plaintext localhost:9003 api.StockService/GetItems
```

**Stock Service Logs:**
```
⚠️  Cache MISS → Query PostgreSQL
✅ Cache populated
```

**Test GetItems (Second Call):**
```bash
grpcurl -plaintext localhost:9003 api.StockService/GetItems
```

**Stock Service Logs:**
```
✅ Cache HIT (Redis)
```

**🎯 CHECKPOINT:** Redis Cache funktioniert! 10x schneller! ⚡

---

## 🍳 Phase 14: Kitchen Service (order.paid Consumer)

### Step 14.1: WARUM Kitchen Service?

**Flow:**
```
1. Order erstellt → "pending"
2. Payment completed → "paid"
3. Kitchen Service → Update Order: "preparing"
4. Kitchen Display → Zeigt Order
```

### Step 14.2: Kitchen Consumer (Minimal)

**Datei:** `kitchen/consumer.go`

```go
package main

import (
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
}

func NewConsumer() *Consumer {
	return &Consumer{}
}

func (c *Consumer) Listen(ch *amqp.Channel) {
	// Queue deklarieren
	q, err := ch.QueueDeclare(
		"kitchen_order_paid",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	// Bind to "order.paid" exchange
	err = ch.QueueBind(
		q.Name,
		"order.paid",
		"order.paid",
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to bind queue: %v", err)
	}

	// Consumer starten
	msgs, err := ch.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to consume: %v", err)
	}

	log.Println("🎧 Listening for order.paid events...")

	for msg := range msgs {
		var event map[string]interface{}
		if err := json.Unmarshal(msg.Body, &event); err != nil {
			log.Printf("❌ Failed to unmarshal: %v", err)
			msg.Nack(false, false)
			continue
		}

		orderID, _ := event["order_id"].(string)

		// TODO: Update Order Status: preparing
		// TODO: Show on Kitchen Display

		log.Printf("🍳 Order moved to kitchen: %s", orderID)

		msg.Ack(false)
	}
}
```

**Datei:** `kitchen/main.go`

```go
package main

import (
	"log"

	"github.com/timour/order-microservices/common/broker"
)

func main() {
	// RabbitMQ Connection
	ch, closeRabbitMQ, err := broker.Connect("guest", "guest", "localhost", "5672")
	if err != nil {
		log.Fatalf("❌ Failed to connect: %v", err)
	}
	defer closeRabbitMQ()
	log.Println("✅ Connected to RabbitMQ")

	// Consumer
	consumer := NewConsumer()
	consumer.Listen(ch)
}
```

**🎯 CHECKPOINT:** Kitchen Service basic funktioniert!

---

## 🔧 Phase 15: Production Best Practices

### Step 15.1: ResilientConnection (Auto-Reconnect)

**WARUM?**
- RabbitMQ Connection kann nach Stunden sterben
- Services crashen: "channel/connection is not open"
- ❌ Manual Restart nötig

**LÖSUNG:** Auto-Reconnect mit Exponential Backoff

**Details:** Siehe `ADVANCED_SETUP.md`

**Migration:**
- `broker.Connect()` → `broker.NewResilientConnection()`
- Auto-Reconnect bei Connection Loss
- Channel Recreation bei Channel Loss

### Step 15.2: Health Check Endpoints

**WARUM?**
- Kubernetes Readiness/Liveness Probes
- Load Balancer Health Checks
- Monitoring

**Implementation:**

**Gateway:**
```go
mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"healthy"}`))
})
```

**Orders, Payments, Stock:** Analog

**Health Endpoints:**
- Gateway: `http://localhost:8080/health`
- Orders: `http://localhost:9001/health`
- Payments: `http://localhost:8082/health`
- Stock: `http://localhost:8083/health`

### Step 15.3: ResilientConsumer Pattern (Auto-Restarting Consumers)

**WARUM?**
- RabbitMQ Channels können sterben (Connection Lost, Server Restart)
- Normale Consumer: Channel stirbt → Consumer stirbt PERMANENT
- ❌ Messages werden nicht mehr verarbeitet
- ❌ Manual Restart nötig

**PROBLEM Beispiel:**
```
Kitchen Consumer startet → Channel OK → verarbeitet Messages
  ↓ (Nach 2 Stunden)
RabbitMQ Channel stirbt
  ↓
Consumer sterben PERMANENT
  ↓
❌ Keine neuen Orders in Kitchen Display!
❌ Queue voll, aber 0 Consumer aktiv!
```

**LÖSUNG: ResilientConsumer Pattern**
```
1. Consumer erkennt Channel Closure
2. Holt fresh Channel von ResilientConnection
3. Startet Consumer neu automatisch
4. Continues processing Messages
✅ Zero Downtime!
```

---

#### Implementation 1: Kitchen ResilientConsumer

**Datei:** `kitchen/consumer_resilient.go` (NEU!)

```go
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

// ResilientConsumer: Auto-restarting consumer that handles channel closures
// Warum?
// → Normale Consumer: Channel stirbt → Consumer stirbt
// → Resilient Consumer: Channel stirbt → Holt neuen Channel → Startet neu!
type ResilientConsumer struct {
	gateway       Gateway
	logger        *slog.Logger
	resilientConn *broker.ResilientConnection
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewResilientConsumer(
	gateway Gateway,
	logger *slog.Logger,
	resilientConn *broker.ResilientConnection,
) *ResilientConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ResilientConsumer{
		gateway:       gateway,
		logger:        logger,
		resilientConn: resilientConn,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start: Startet resilient consumer with auto-restart
// Warum?
// → Überwacht Channel Closures
// → Startet Consumer neu wenn Channel stirbt
// → Verhindert Message Loss!
func (rc *ResilientConsumer) Start() {
	go rc.consume()
}

func (rc *ResilientConsumer) consume() {
	for {
		select {
		case <-rc.ctx.Done():
			rc.logger.Info("consumer stopped")
			return
		default:
			// Hole frischen Channel von ResilientConnection
			ch, err := rc.resilientConn.Channel()
			if err != nil {
				rc.logger.Error("failed to get channel, retrying in 5s", slog.Any("error", err))
				time.Sleep(5 * time.Second)
				continue
			}

			// Starte Consumer auf diesem Channel
			rc.logger.Info("starting consumer on fresh channel")
			if err := rc.consumeOnChannel(ch); err != nil {
				rc.logger.Warn("consumer stopped, restarting...", slog.Any("error", err))
				// Channel geschlossen → Warte kurz → Hole neuen Channel
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}
}

func (rc *ResilientConsumer) consumeOnChannel(ch *amqp.Channel) error {
	// 1. Declare DLX (Dead Letter Exchange)
	err := ch.ExchangeDeclare(
		broker.DLX,   // name: "dlx"
		"fanout",     // type: fanout (broadcast to all bound queues)
		true,         // durable: Überlebt RabbitMQ Restart
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return err
	}

	// 2. Declare DLQ (Dead Letter Queue) für order.paid
	dlq, err := ch.QueueDeclare(
		"dlq.order.paid", // queue name
		true,             // durable: Überlebt RabbitMQ Restart
		false,            // delete when unused
		false,            // exclusive
		false,            // no-wait
		nil,              // arguments
	)
	if err != nil {
		return err
	}

	// 3. Bind DLQ to DLX
	err = ch.QueueBind(
		dlq.Name,    // queue: "dlq.order.paid"
		"",          // routing key: "" = matches all (fanout exchange)
		broker.DLX,  // exchange: "dlx"
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		return err
	}

	// 4. Queue deklarieren
	q, err := ch.QueueDeclare(
		broker.OrderPaidEvent, // queue name: "order.paid"
		true,                  // durable: Überlebt RabbitMQ Restart
		false,                 // delete when unused: NEIN
		false,                 // exclusive: Andere Consumer können auch lesen
		false,                 // no-wait
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX, // ⭐ DLX Integration! Failed messages → "dlx" exchange
		},
	)
	if err != nil {
		return err
	}

	rc.logger.Info("queue declared",
		slog.String("queue", broker.OrderPaidEvent),
	)

	// 5. Queue an Exchange binden
	err = ch.QueueBind(
		q.Name,                // queue name: "order.paid"
		"",                    // routing key: "" = matches all
		broker.OrderPaidEvent, // exchange name: "order.paid"
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		return err
	}

	rc.logger.Info("queue bound to exchange",
		slog.String("queue", broker.OrderPaidEvent),
		slog.String("exchange", broker.OrderPaidEvent),
	)

	// 6. Consumer registrieren
	msgs, err := ch.Consume(
		q.Name, // queue: "order.paid"
		"",     // consumer tag: "" = Auto-generiert
		false,  // auto-ack: FALSE! (Wichtig für DLQ!) → Manuelles Ack/Nack
		false,  // exclusive: Andere Consumer können auch lesen
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		return err
	}

	rc.logger.Info("order.paid consumer started (resilient)",
		slog.String("queue", broker.OrderPaidEvent),
	)

	// Monitor channel closures
	closeCh := make(chan *amqp.Error)
	ch.NotifyClose(closeCh)

	// Process messages
	for {
		select {
		case <-rc.ctx.Done():
			return nil

		case err := <-closeCh:
			// Channel wurde geschlossen!
			rc.logger.Warn("channel closed, will restart consumer", slog.Any("error", err))
			return err

		case d, ok := <-msgs:
			if !ok {
				// msgs channel closed
				rc.logger.Warn("msgs channel closed, will restart consumer")
				return nil
			}

			// Process message
			rc.processMessage(ch, d)
		}
	}
}

func (rc *ResilientConsumer) processMessage(ch *amqp.Channel, d amqp.Delivery) {
	// Extract trace context
	ctx := broker.ExtractTraceContext(context.Background(), d.Headers)

	// Start span for message processing
	tracer := otel.Tracer("kitchen")
	ctx, span := tracer.Start(ctx, "AMQP - consume - order.paid")
	defer span.End()

	rc.logger.Info("received message",
		slog.String("body", string(d.Body)),
	)

	// Unmarshal order
	o := &pb.Order{}
	if err := json.Unmarshal(d.Body, o); err != nil {
		rc.logger.Error("failed to unmarshal order", slog.Any("error", err))
		// ⭐ RETRY LOGIC with DLQ fallback
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false) // false, false = Don't requeue, goes to DLQ!
		return
	}

	// Update order status via gateway
	err := rc.gateway.UpdateOrderAfterPayment(ctx, o)
	if err != nil {
		rc.logger.Error("failed to update order", slog.Any("error", err))
		// ⭐ RETRY LOGIC with DLQ fallback
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false)
		return
	}

	// Success!
	d.Ack(false)

	rc.logger.Info("updating order",
		slog.String("order_id", o.Id),
		slog.String("status", o.Status),
	)
	rc.logger.Info("order updated successfully",
		slog.String("order_id", o.Id),
		slog.String("status", o.Status),
	)
}

func (rc *ResilientConsumer) Stop() {
	rc.cancel()
}
```

**Datei:** `kitchen/main.go` (Update)

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/joho/godotenv/autoload"
	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/common/config"
	"github.com/timour/order-microservices/common/discovery"
	"github.com/timour/order-microservices/common/discovery/consul"
	"github.com/timour/order-microservices/common/logger"
	"github.com/timour/order-microservices/common/tracing"
	"github.com/timour/order-microservices/kitchen/gateway"
)

func main() {
	// Configuration
	cfg := Config{
		ServiceName: config.GetEnv("SERVICE_NAME", "kitchen"),
		InstanceID:  config.GetEnv("INSTANCE_ID", "kitchen-1"),
		ConsulAddr:  config.GetEnv("CONSUL_ADDR", ""),
		AMQPUser:    config.GetEnv("AMQP_USER", "guest"),
		AMQPPass:    config.GetEnv("AMQP_PASS", "guest"),
		AMQPHost:    config.GetEnv("AMQP_HOST", "localhost"),
		AMQPPort:    config.GetEnv("AMQP_PORT", "5672"),
		OrdersAddr:  config.GetEnv("ORDERS_GRPC_ADDR", "localhost:9000"),
	}

	log := logger.NewLogger(cfg.ServiceName)
	log.Info("starting service",
		slog.String("instance_id", cfg.InstanceID),
	)

	// Initialize OpenTelemetry Tracing
	shutdown, err := tracing.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Error("failed to initialize tracer", slog.Any("error", err))
		os.Exit(1)
	}
	defer shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("received shutdown signal")
		cancel()
	}()

	// ✅ ResilientConnection mit Auto-Reconnect
	log.Info("connecting to rabbitmq with auto-reconnect",
		slog.String("host", cfg.AMQPHost),
		slog.String("port", cfg.AMQPPort),
	)
	resilientConn, err := broker.NewResilientConnection(cfg.AMQPUser, cfg.AMQPPass, cfg.AMQPHost, cfg.AMQPPort)
	if err != nil {
		log.Error("failed to connect to rabbitmq", slog.Any("error", err))
		os.Exit(1)
	}
	defer resilientConn.Close()
	log.Info("rabbitmq connected successfully with auto-reconnect enabled")

	// Gateway
	var registry discovery.Registry
	if cfg.ConsulAddr != "" {
		registry, err = consul.NewRegistry(cfg.ConsulAddr, cfg.ServiceName)
		if err != nil {
			log.Error("failed to create consul registry", slog.Any("error", err))
		}
	}

	ordersGateway := gateway.NewOrdersGateway(registry, cfg.OrdersAddr)
	log.Info("orders gateway initialized")

	// ✅ ResilientConsumer mit Auto-Restart (Production Best Practice)
	// → Überwacht Channel closures
	// → Startet automatisch neu wenn Channel stirbt
	// → Holt frischen Channel von ResilientConnection
	consumer := NewResilientConsumer(ordersGateway, log, resilientConn)
	log.Info("starting resilient consumer (auto-restart enabled)...")
	consumer.Start() // Non-blocking! Returns immediately

	// Block until shutdown signal
	<-ctx.Done()
	log.Info("shutting down")
	consumer.Stop()
}
```

---

#### Implementation 2: Stock ResilientConsumer

**Datei:** `stock/consumer_resilient.go` (NEU!)

```go
package main

import (
	"context"
	"encoding/json"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

// ResilientConsumer for Stock Service
type ResilientConsumer struct {
	store         StockStore
	logger        *zap.Logger
	resilientConn *broker.ResilientConnection
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewResilientConsumer(
	store StockStore,
	logger *zap.Logger,
	resilientConn *broker.ResilientConnection,
) *ResilientConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ResilientConsumer{
		store:         store,
		logger:        logger,
		resilientConn: resilientConn,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (rc *ResilientConsumer) Start() {
	go rc.consume()
}

func (rc *ResilientConsumer) consume() {
	for {
		select {
		case <-rc.ctx.Done():
			rc.logger.Info("consumer stopped")
			return
		default:
			ch, err := rc.resilientConn.Channel()
			if err != nil {
				rc.logger.Error("failed to get channel, retrying in 5s", zap.Error(err))
				time.Sleep(5 * time.Second)
				continue
			}

			rc.logger.Info("starting consumer on fresh channel")
			if err := rc.consumeOnChannel(ch); err != nil {
				rc.logger.Warn("consumer stopped, restarting...", zap.Error(err))
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}
}

func (rc *ResilientConsumer) consumeOnChannel(ch *amqp.Channel) error {
	// Queue declaration
	q, err := ch.QueueDeclare(
		broker.OrderCreatedEvent,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX,
		},
	)
	if err != nil {
		return err
	}

	err = ch.QueueBind(
		q.Name,
		"",
		broker.OrderCreatedEvent,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	msgs, err := ch.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	rc.logger.Info("order.created consumer started (resilient)",
		zap.String("queue", broker.OrderCreatedEvent),
	)

	// Monitor channel closures
	closeCh := make(chan *amqp.Error)
	ch.NotifyClose(closeCh)

	// Process messages
	for {
		select {
		case <-rc.ctx.Done():
			return nil

		case err := <-closeCh:
			rc.logger.Warn("channel closed, will restart consumer", zap.Error(err))
			return err

		case d, ok := <-msgs:
			if !ok {
				rc.logger.Warn("msgs channel closed, will restart consumer")
				return nil
			}

			rc.processMessage(ch, d)
		}
	}
}

func (rc *ResilientConsumer) processMessage(ch *amqp.Channel, d amqp.Delivery) {
	ctx := broker.ExtractTraceContext(context.Background(), d.Headers)

	tracer := otel.Tracer("stock")
	ctx, span := tracer.Start(ctx, "AMQP - consume - order.created")
	defer span.End()

	// Unmarshal order
	o := &pb.Order{}
	if err := json.Unmarshal(d.Body, o); err != nil {
		rc.logger.Error("failed to unmarshal order", zap.Error(err))
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", zap.Error(err))
		}
		d.Nack(false, false)
		return
	}

	// Reserve stock
	err := rc.store.Reserve(ctx, o.Items)
	if err != nil {
		rc.logger.Error("failed to reserve stock", zap.Error(err))
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", zap.Error(err))
		}
		d.Nack(false, false)
		return
	}

	// Success!
	d.Ack(false)
	rc.logger.Info("stock reserved successfully", zap.String("order_id", o.Id))
}

func (rc *ResilientConsumer) Stop() {
	rc.cancel()
}
```

---

#### Implementation 3: Orders ResilientConsumer

**Datei:** `orders/consumer_resilient.go` (NEU!)

```go
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
)

type ResilientConsumer struct {
	store         OrdersStore
	logger        *slog.Logger
	resilientConn *broker.ResilientConnection
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewResilientConsumer(
	store OrdersStore,
	logger *slog.Logger,
	resilientConn *broker.ResilientConnection,
) *ResilientConsumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ResilientConsumer{
		store:         store,
		logger:        logger,
		resilientConn: resilientConn,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (rc *ResilientConsumer) Start() {
	go rc.consume()
}

func (rc *ResilientConsumer) consume() {
	for {
		select {
		case <-rc.ctx.Done():
			rc.logger.Info("consumer stopped")
			return
		default:
			ch, err := rc.resilientConn.Channel()
			if err != nil {
				rc.logger.Error("failed to get channel, retrying in 5s", slog.Any("error", err))
				time.Sleep(5 * time.Second)
				continue
			}

			rc.logger.Info("starting consumer on fresh channel")
			if err := rc.consumeOnChannel(ch); err != nil {
				rc.logger.Warn("consumer stopped, restarting...", slog.Any("error", err))
				time.Sleep(2 * time.Second)
				continue
			}
		}
	}
}

func (rc *ResilientConsumer) consumeOnChannel(ch *amqp.Channel) error {
	q, err := ch.QueueDeclare(
		broker.OrderPaidEvent,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX,
		},
	)
	if err != nil {
		return err
	}

	err = ch.QueueBind(
		q.Name,
		"",
		broker.OrderPaidEvent,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	msgs, err := ch.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	rc.logger.Info("order.paid consumer started (resilient)",
		slog.String("queue", broker.OrderPaidEvent),
	)

	closeCh := make(chan *amqp.Error)
	ch.NotifyClose(closeCh)

	for {
		select {
		case <-rc.ctx.Done():
			return nil

		case err := <-closeCh:
			rc.logger.Warn("channel closed, will restart consumer", slog.Any("error", err))
			return err

		case d, ok := <-msgs:
			if !ok {
				rc.logger.Warn("msgs channel closed, will restart consumer")
				return nil
			}

			rc.processMessage(ch, d)
		}
	}
}

func (rc *ResilientConsumer) processMessage(ch *amqp.Channel, d amqp.Delivery) {
	ctx := broker.ExtractTraceContext(context.Background(), d.Headers)

	tracer := otel.Tracer("orders")
	ctx, span := tracer.Start(ctx, "AMQP - consume - order.paid")
	defer span.End()

	rc.logger.Info("received message",
		slog.String("body", string(d.Body)),
	)

	// Unmarshal order
	o := &pb.Order{}
	if err := json.Unmarshal(d.Body, o); err != nil {
		rc.logger.Error("failed to unmarshal order", slog.Any("error", err))
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false)
		return
	}

	// Update order in store
	err := rc.store.Update(ctx, o.Id, o)
	if err != nil {
		rc.logger.Error("failed to update order", slog.Any("error", err))
		if err := broker.HandleRetry(ch, &d); err != nil {
			rc.logger.Error("error handling retry", slog.Any("error", err))
		}
		d.Nack(false, false)
		return
	}

	// Success!
	d.Ack(false)

	rc.logger.Info("updating order",
		slog.String("order_id", o.Id),
		slog.String("status", o.Status),
		slog.String("payment_link", o.PaymentLink),
	)
	rc.logger.Info("order updated successfully",
		slog.String("order_id", o.Id),
		slog.String("status", o.Status),
	)
}

func (rc *ResilientConsumer) Stop() {
	rc.cancel()
}
```

---

#### Testing ResilientConsumer

**Test 1: Normal Operation**

```bash
# Start all services with ResilientConsumer
docker-compose up -d

# Check logs - should see:
docker logs kitchen-prod 2>&1 | tail -20
```

**Expected Output:**
```
✅ Connected to RabbitMQ with auto-reconnect
✅ Starting resilient consumer (auto-restart enabled)...
✅ Starting consumer on fresh channel
✅ order.paid consumer started (resilient) queue=order.paid
```

**Test 2: Channel Closure Survival**

```bash
# Simulate RabbitMQ restart
docker restart rabbitmq

# Wait 10 seconds, then check consumer logs
docker logs kitchen-prod 2>&1 | tail -30
```

**Expected Output:**
```
⚠️  Channel closed, will restart consumer error="Exception (504) Reason: channel/connection is not open"
🔄 Reconnection attempt #1...
✅ Reconnection successful after 1 attempts
✅ Starting consumer on fresh channel
✅ order.paid consumer started (resilient) queue=order.paid
```

**Test 3: Message Processing After Restart**

```bash
# Create order after RabbitMQ restart
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "cust_123",
    "items": [{"item_id": "burger", "quantity": 2}]
  }'

# Pay the order (simulate Stripe webhook)
# Check kitchen logs
docker logs kitchen-prod 2>&1 | tail -10
```

**Expected Output:**
```
✅ received message body={"id":"...","status":"paid",...}
✅ updating order order_id=... status=paid
✅ order updated successfully order_id=... status=paid
```

---

#### Key Benefits

**1. Zero Downtime**
- Channel stirbt → Consumer startet neu in 2 Sekunden
- ✅ Keine Messages verloren
- ✅ Kein Manual Restart nötig

**2. Production Ready**
- Exponential Backoff bei Failures
- Thread-Safe Channel Access
- Graceful Shutdown Support

**3. Integration mit DLQ**
- Failed Messages → Dead Letter Queue
- Manual Investigation möglich
- Retry Logic mit HandleRetry()

**4. Monitoring Friendly**
- Logs zeigen Auto-Restart
- Metrics können Restart Count tracken
- Alerts bei häufigen Restarts

---

**🎯 CHECKPOINT:** Alle Consumer sind jetzt PRODUCTION-READY mit Auto-Restart! 🚀

---

## 🐳 Phase 16: Docker Compose (Full Stack)

### Step 16.1: Complete docker-compose.yml

**Datei:** `docker-compose.yml`

```yaml
version: '3.9'

services:
  # Infrastructure
  consul:
    image: consul:1.15.4
    ports:
      - "8500:8500"
    command: "agent -dev -ui -client=0.0.0.0"

  rabbitmq:
    image: rabbitmq:3.13-management-alpine
    ports:
      - "5672:5672"
      - "15672:15672"
    environment:
      RABBITMQ_DEFAULT_USER: guest
      RABBITMQ_DEFAULT_PASS: guest

  postgres:
    image: postgres:15-alpine
    ports:
      - "5432:5432"
    environment:
      POSTGRES_USER: stock
      POSTGRES_PASSWORD: stock123
      POSTGRES_DB: stock
    volumes:
      - ./stock/migrations/init.sql:/docker-entrypoint-initdb.d/init.sql

  mongodb:
    image: mongo:7
    ports:
      - "27017:27017"

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  # Backend Services
  gateway:
    build:
      context: .
      dockerfile: gateway/Dockerfile
    ports:
      - "8080:8080"
    environment:
      CONSUL_ADDR: consul:8500
    depends_on:
      - consul

  orders:
    build:
      context: .
      dockerfile: orders/Dockerfile
    environment:
      CONSUL_ADDR: consul:8500
      MONGODB_URI: mongodb://mongodb:27017/orders
      RABBITMQ_HOST: rabbitmq
      STOCK_GRPC_ADDR: stock:9003
    depends_on:
      - consul
      - mongodb
      - rabbitmq
      - stock

  payments:
    build:
      context: .
      dockerfile: payments/Dockerfile
    environment:
      RABBITMQ_HOST: rabbitmq
      STRIPE_SECRET_KEY: ${STRIPE_SECRET_KEY}
    depends_on:
      - rabbitmq

  stock:
    build:
      context: .
      dockerfile: stock/Dockerfile
    environment:
      POSTGRES_HOST: postgres
      REDIS_ADDR: redis:6379
      RABBITMQ_HOST: rabbitmq
    depends_on:
      - postgres
      - redis

  kitchen:
    build:
      context: .
      dockerfile: kitchen/Dockerfile
    environment:
      RABBITMQ_HOST: rabbitmq
    depends_on:
      - rabbitmq
```

**Starten:**
```bash
# Environment Variables setzen
export STRIPE_SECRET_KEY=sk_test_...

# Build & Start
docker-compose up --build
```

---

## 📊 Complete System Architecture

```
┌─────────────────────────────────────────────────────────┐
│ CUSTOMER APP (React)                                    │
└────────────┬────────────────────────────────────────────┘
             │ HTTP
             ↓
┌─────────────────────────────────────────────────────────┐
│ GATEWAY (Port 8080)                                     │
│  - Service Discovery (Consul)                           │
│  - HTTP → gRPC                                          │
│  - Health Checks                                        │
└────────────┬────────────────────────────────────────────┘
             │ gRPC (Service Discovery)
             ↓
┌─────────────────────────────────────────────────────────┐
│ ORDERS SERVICE (Port 9000)                              │
│  - Stock Check (gRPC → Stock Service)                   │
│  - MongoDB (Order Storage)                              │
│  - RabbitMQ Publisher (order.created)                   │
│  - Health Checks                                        │
└────────────┬────────────────────────────────────────────┘
             │
             ↓
┌─────────────────────────────────────────────────────────┐
│ RABBITMQ (Message Broker)                               │
│  - order.created → Payments Service                     │
│  - order.paid → Kitchen Service                         │
│  - Dead Letter Queues                                   │
│  - ResilientConnection (Auto-Reconnect)                 │
└────────┬────────────────────────────┬───────────────────┘
         │                            │
         ↓                            ↓
┌────────────────────┐   ┌───────────────────────────────┐
│ PAYMENTS SERVICE   │   │ KITCHEN SERVICE               │
│  - Stripe API      │   │  - Order Display              │
│  - Payment Links   │   │  - Status Updates             │
│  - Health Checks   │   │  - RabbitMQ Consumer          │
└────────────────────┘   └───────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ STOCK SERVICE (Port 9003)                               │
│  - PostgreSQL (Inventory + Reservations)                │
│  - Redis Cache (Menu Items, TTL: 5min)                  │
│  - gRPC Endpoints (GetItems, CheckStock)                │
│  - Health Checks                                        │
└─────────────────────────────────────────────────────────┘
```

---

## ✅ Complete Feature Checklist

### Architecture
- ✅ Clean Architecture (4 Layers)
- ✅ Microservices Pattern
- ✅ Event-Driven Architecture
- ✅ gRPC Communication
- ✅ Service Discovery (Consul)

### Services
- ✅ Gateway (HTTP → gRPC, Service Discovery)
- ✅ Orders (MongoDB, Stock Check, RabbitMQ Publisher)
- ✅ Payments (Stripe, RabbitMQ Consumer)
- ✅ Stock (PostgreSQL, Redis, Stock Check)
- ✅ Kitchen (RabbitMQ Consumer)

### Infrastructure
- ✅ RabbitMQ (Events, DLQ)
- ✅ PostgreSQL (Stock Data, ACID)
- ✅ MongoDB (Orders Storage)
- ✅ Redis (Cache-Aside Pattern)
- ✅ Consul (Service Registry)

### Production Features
- ✅ ResilientConnection (Auto-Reconnect)
- ✅ Health Check Endpoints
- ✅ Cache-Aside Pattern (Performance)
- ✅ Stock Validation (No Overselling)
- ✅ Stripe Payment Integration
- ✅ Docker Compose Deployment

---

## 🎯 Part 3 Zusammenfassung

### Was haben wir gebaut?

**Von Minimal zu Production:**
1. ✅ **Stripe Integration** - Echte Payment Links
2. ✅ **Stock Service** - In-Memory → PostgreSQL → Redis Cache
3. ✅ **Orders Integration** - Stock Check BEVOR Order erstellt
4. ✅ **Kitchen Service** - order.paid Consumer
5. ✅ **Production Features** - Auto-Reconnect, Health Checks
6. ✅ **Docker Compose** - Full Stack Deployment

### Schritte die wir gemacht haben:

1. ✅ **Iterativ erweitert** - Minimal → PostgreSQL → Redis
2. ✅ **Step-by-Step getestet** - Jeder Step funktionsfähig
3. ✅ **WARUM verstanden** - Begründung für jede Erweiterung
4. ✅ **Production-Ready** - Google SRE Best Practices

### Final Flow

```
1. Customer → Create Order
   ↓
2. Gateway → Service Discovery → Orders Service
   ↓
3. Orders → Stock Check (gRPC)
   ↓ (Stock OK)
4. Orders → Save Order (MongoDB)
   ↓
5. Orders → Publish: order.created (RabbitMQ)
   ↓
6. Payments → Create Payment Link (Stripe)
   ↓
7. Customer → Pays
   ↓
8. Stripe → Webhook → Payments Service
   ↓
9. Payments → Publish: order.paid (RabbitMQ)
   ↓
10. Kitchen → Update Order: preparing
    ↓
11. Kitchen Display → Shows Order
    ↓
12. Staff → Mark Ready
    ↓
13. Pickup Display → Shows Ready Orders
```

---

## 📊 Phase 15: Production Observability (Metrics, Logs, Traces)

### Step 15.1: Die 3 Säulen der Observability

**Was ist Observability?**
Die Fähigkeit, den internen Zustand eines Systems anhand seiner Ausgaben zu verstehen.

**Die 3 Pillars:**
1. **Metrics** - Was passiert? (Counters, Gauges, Histograms)
2. **Logs** - Warum passiert es? (Events mit Context)
3. **Traces** - Wo passiert es? (Request Flow durch Services)

### Step 15.2: AKTUELLER STAND ✅

**Du hast bereits:**

| Service | Metrics | Structured Logs | Distributed Tracing |
|---------|---------|----------------|---------------------|
| Gateway | ✅ `/metrics` | ✅ slog (JSON) | ✅ OpenTelemetry |
| Orders | ✅ `/metrics` | ✅ slog (JSON) | ✅ OpenTelemetry |
| Payments | ✅ `/metrics` | ✅ slog (JSON) | ✅ OpenTelemetry |
| Kitchen | ✅ `/metrics` | ✅ slog (JSON) | ✅ OpenTelemetry |
| Stock | ✅ `/metrics` | ✅ zap (JSON) | ✅ OpenTelemetry |

**🎉 Dein System ist bereits Production-Ready für Observability!**

### Step 15.3: Prometheus Metrics (Auto-Metrics)

**Was wird automatisch gemessen?**

Jeder Service exposed bereits `/metrics`:
```bash
curl http://localhost:8080/metrics

# Output:
# http_requests_total{code="200",method="GET",path="/api/menu"} 150
# http_request_duration_seconds_bucket{le="0.1"} 145
# go_goroutines 23
# process_cpu_seconds_total 2.45
```

**Metrics Categories:**

1. **HTTP Metrics** (automatisch via `promhttp.Handler()`)
   - Request Count: `http_requests_total`
   - Duration: `http_request_duration_seconds`
   - Aktive Requests: `http_requests_in_flight`

2. **Go Runtime Metrics** (automatisch)
   - Goroutines: `go_goroutines`
   - Memory: `go_memstats_alloc_bytes`
   - GC: `go_gc_duration_seconds`

3. **gRPC Metrics** (automatisch via `otelgrpc`)
   - RPC Duration: `rpc_server_duration`
   - RPC Count: `rpc_server_requests_total`

**Prometheus Configuration bereits vorhanden:**
```yaml
# observability/prometheus.yml
scrape_configs:
  - job_name: 'gateway'
    static_configs:
      - targets: ['gateway:8080']
    metrics_path: '/metrics'

  - job_name: 'orders'
    static_configs:
      - targets: ['orders:9001']

  - job_name: 'payments'
    static_configs:
      - targets: ['payments:8082']

  - job_name: 'stock'
    static_configs:
      - targets: ['stock:8083']

  - job_name: 'kitchen'
    static_configs:
      - targets: ['kitchen:8083']
```

**Prometheus Targets checken:**
```bash
curl http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | "\(.labels.job): \(.health)"'

# Output:
# gateway: up
# kitchen: up
# orders: up
# payments: up
# stock: up
# rabbitmq: up
```

### Step 15.4: Structured Logging (JSON)

**Warum strukturierte Logs?**

**ALT (Unstructured):**
```go
log.Printf("Order created: %s for customer %s", orderID, customerID)
// Output: "Order created: abc123 for customer cust456"
// ❌ Schwer zu parsen
// ❌ Keine Query nach customerID möglich
```

**NEU (Structured):**
```go
slog.Info("order created",
    slog.String("order_id", orderID),
    slog.String("customer_id", customerID),
    slog.String("trace_id", common.GetTraceID(ctx)),
)
// Output: {"level":"INFO","msg":"order created","order_id":"abc123","customer_id":"cust456","trace_id":"e7b7bf2c..."}
// ✅ Einfach zu parsen
// ✅ Query: customer_id="cust456"
// ✅ Trace ID für Correlation!
```

**Logs mit Trace IDs:**
```bash
docker logs gateway-prod | grep trace_id

# Output:
# {"level":"ERROR","msg":"failed to create order","customer_id":"123","error":"...","trace_id":"e7b7bf2c54ba510692a2ac19d7e447e9"}
```

**Diese Trace ID kannst du direkt in Jaeger suchen!** 🔥

### Step 15.5: Distributed Tracing (OpenTelemetry)

**Was ist ein Trace?**

Ein Request durch alle Services:
```
Customer → Gateway → Orders → Stock → PostgreSQL
                    ↓
                  Payment → Stripe
```

**Trace Beispiel:**
```
Trace ID: e7b7bf2c54ba510692a2ac19d7e447e9
└─ Gateway: POST /api/customers/123/orders (500ms)
   ├─ Orders gRPC: CreateOrder (200ms)
   │  ├─ Validate Items (10ms)
   │  ├─ Stock gRPC: CheckAndReserve (100ms)
   │  │  ├─ Redis Cache Check (5ms)
   │  │  └─ PostgreSQL Query (80ms)
   │  └─ MongoDB Insert (90ms)
   └─ RabbitMQ Publish (50ms)
```

**OpenTelemetry Configuration (bereits vorhanden):**

Jeder Service initialisiert OTEL:
```go
// common/tracer.go
func SetGlobalTracer(ctx context.Context, serviceName, exporterEndpoint string) error {
    client := otlptracehttp.NewClient(
        otlptracehttp.WithInsecure(),
        otlptracehttp.WithEndpoint(exporterEndpoint))

    exporter, err := otlptrace.New(ctx, client)
    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.ServiceNameKey.String(serviceName),
        )),
    )

    otel.SetTracerProvider(tp)
    return nil
}
```

**HTTP Auto-Tracing:**
```go
// gateway/app.go
mux.Handle("POST /api/customers/{customerID}/orders",
    otelhttp.NewHandler(http.HandlerFunc(h.handleCreateOrder),
        "POST /api/customers/{customerID}/orders"))
```

**gRPC Auto-Tracing:**
```go
// orders/main.go
grpcServer := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()))
```

**Jaeger UI öffnen:**
```bash
open http://localhost:16686

# Suche nach:
# - Service: gateway
# - Operation: POST /api/customers/{customerID}/orders
# - Trace ID: e7b7bf2c54ba510692a2ac19d7e447e9
```

### Step 15.6: Grafana Dashboards

**Grafana öffnen:**
```bash
open http://localhost:3002
# Login: admin / admin123
```

**Vorkonfigurierte Dashboards:**

1. **OMS - Business Metrics** (`grafana/dashboards/business-metrics.json`)
   - Orders per Minute
   - Success Rate %
   - Avg Response Time (P95)
   - Errors per Minute
   - Request Rate by Status Code
   - RabbitMQ Queue Depth
   - Service Request Rates

**PromQL Beispiele:**
```promql
# Orders pro Minute
sum(rate(http_requests_total{job="gateway",code=~"2..",path="/api/customers/.*/orders"}[5m])) * 60

# Success Rate
sum(rate(http_requests_total{job="gateway",code=~"2.."}[5m]))
/
sum(rate(http_requests_total{job="gateway"}[5m])) * 100

# P95 Response Time
histogram_quantile(0.95,
  sum(rate(http_request_duration_seconds_bucket{job="gateway"}[5m])) by (le))

# Errors pro Minute
sum(rate(http_requests_total{job="gateway",code=~"5.."}[5m])) * 60
```

### Step 15.7: Cloud-Native Compatibility 🌥️

**🎉 Dein System funktioniert mit ALLEN Cloud-Native Tools!**

**Warum? Standard-Formate:**

| Data Type | Format | Compatible With |
|-----------|--------|----------------|
| **Metrics** | Prometheus `/metrics` | Datadog, Prometheus, New Relic, Grafana Cloud |
| **Traces** | OpenTelemetry (OTLP) | Datadog APM, Jaeger, Tempo, Honeycomb |
| **Logs** | Structured JSON | Datadog Logs, Loki, Elasticsearch, CloudWatch |

### Step 15.8: Switching to Datadog (Beispiel)

**Aktuell (Jaeger):**
```yaml
# docker-compose.prod.yml
environment:
  - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
```

**Datadog:**
```yaml
environment:
  - OTEL_EXPORTER_OTLP_ENDPOINT=https://api.datadoghq.com:4317
  - DD_API_KEY=${DATADOG_API_KEY}
  - DD_SERVICE=gateway
  - DD_ENV=production
```

**Das war's! Keine Code-Änderungen!** 🚀

### Step 15.9: Switching to Loki (Logs)

**Promtail hinzufügen:**
```yaml
# docker-compose.prod.yml
services:
  promtail:
    image: grafana/promtail:latest
    volumes:
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - ./observability/promtail-config.yaml:/etc/promtail/config.yml
    command: -config.file=/etc/promtail/config.yml
```

**Promtail Config:**
```yaml
# observability/promtail-config.yaml
server:
  http_listen_port: 9080

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  - job_name: docker
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
    relabel_configs:
      - source_labels: ['__meta_docker_container_name']
        target_label: 'container'
      - source_labels: ['__meta_docker_container_log_stream']
        target_label: 'stream'
```

**Deine JSON Logs werden automatisch geschickt!** ✅

### Step 15.10: Trace ID Correlation (Log → Trace)

**Das Killer-Feature:**

1. **Error in Logs finden:**
```bash
docker logs gateway-prod | grep ERROR
# {"level":"ERROR","msg":"failed to create order","trace_id":"e7b7bf2c54ba510692a2ac19d7e447e9"}
```

2. **Trace ID kopieren:** `e7b7bf2c54ba510692a2ac19d7e447e9`

3. **In Jaeger suchen:** http://localhost:16686
   - Paste Trace ID
   - Siehst KOMPLETTEN Request Flow!

4. **Root Cause finden:**
```
Gateway → Orders (200ms) ✅
  └─ Stock gRPC (timeout after 5s) ❌
     └─ PostgreSQL slow query (4.8s) 🐢
```

**Problem gefunden: PostgreSQL Query optimieren!**

### Step 15.11: Testing Your Observability Stack

**1. Alle Prometheus Targets prüfen:**
```bash
curl -s http://localhost:9090/api/v1/targets | jq -r '.data.activeTargets[] | "\(.labels.job): \(.health)"'

# Expected: All "up"
# gateway: up
# kitchen: up
# orders: up
# payments: up
# stock: up
# rabbitmq: up
```

**2. Metrics abrufen:**
```bash
# Gateway Metrics
curl http://localhost:8080/metrics

# Orders Metrics
curl http://localhost:9001/metrics

# Stock Metrics
curl http://localhost:8083/metrics
```

**3. Logs mit Trace IDs:**
```bash
# API Request machen
curl -X POST http://localhost:8080/api/customers/test123/orders \
  -H "Content-Type: application/json" \
  -d '[{"id":"burger","quantity":2,"price_id":"price_burger"}]'

# Logs checken
docker logs gateway-prod --tail 20 | grep trace_id

# Trace ID kopieren und in Jaeger suchen
open http://localhost:16686
```

**4. Grafana Dashboard:**
```bash
open http://localhost:3002/d/oms-business
# Login: admin / admin123

# Du siehst:
# - Orders per Minute
# - Success Rate %
# - Response Time P95
# - Error Rate
```

### Step 15.12: Observability Best Practices

**✅ DO:**
- Strukturierte Logs (JSON) mit Context (customer_id, order_id, trace_id)
- Standard Formate (Prometheus, OTLP, JSON)
- Trace IDs in Logs für Correlation
- Health Checks für alle Services
- Metrics für Business Logic (später: revenue, items sold)

**❌ DON'T:**
- Unstrukturierte Logs: `log.Printf("Something happened")`
- Secrets in Logs: `log.Printf("API Key: %s", apiKey)`
- Vendor Lock-in: Proprietary Formats
- Zu viele Metrics: Overhead ohne Value

### Step 15.13: Next Level: Business Metrics (Optional)

**Aktuell: Nur technische Metrics**
- HTTP Request Count
- Response Time
- CPU/Memory

**Next: Business Metrics**
```go
// payments/processor/stripe.go
var (
    revenueCounter = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "payment_revenue_cents_total",
            Help: "Total revenue in cents",
        },
        []string{"currency"},
    )

    ordersProcessed = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "orders_processed_total",
            Help: "Total orders processed",
        },
    )
)

func (s *StripeProcessor) CreatePaymentIntent(amount int, orderID string) {
    // ... Stripe call ...

    // Track revenue
    revenueCounter.WithLabelValues("usd").Add(float64(amount))
    ordersProcessed.Inc()
}
```

**Dann in Grafana:**
```promql
# Revenue per Hour
sum(rate(payment_revenue_cents_total[1h])) * 3600 / 100

# Orders per Hour
sum(rate(orders_processed_total[1h])) * 3600
```

---

## 🚀 Next Steps

**Kubernetes Deployment:**
- Siehe `CLAUDE.md` für Kubernetes Migration
- k3d für lokales Testing
- Homelab Deployment mit Talos Linux

**Advanced Features:**
- Stock Reservations (Prevent Overselling während Payment)
- Order History & Analytics
- Customer Notifications
- Kitchen Timer & Alerts
- Business Metrics (Revenue, Popular Items)

**Production Hardening:**
- Rate Limiting (prevent abuse)
- Circuit Breakers (fail fast)
- Retry Policies (resilience)
- Alert Manager (notifications)

---

**🎉 CONGRATULATIONS!** Du hast ein **Production-Ready Order Management System** gebaut!

**Du kannst jetzt:**
- ✅ Microservices in Go entwickeln
- ✅ Clean Architecture anwenden
- ✅ Event-Driven Systems bauen
- ✅ PostgreSQL + Redis nutzen
- ✅ Stripe Payments integrieren
- ✅ Production Best Practices implementieren
- ✅ Docker Compose & Kubernetes deployen

**🎯 Du bist jetzt ein Microservices Engineer!** 🚀
