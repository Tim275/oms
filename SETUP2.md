# SETUP Part 2: Service Discovery, RabbitMQ & Payments (Step-by-Step)

> **Production-Ready Features** - Von Hardcoded zu Dynamic Service Discovery & Event-Driven

---

## 📚 Was haben wir bis jetzt?

**Part 1 Resultat:**
- ✅ Orders Service (gRPC) - CreateOrder mit Items
- ✅ Gateway (HTTP → gRPC)
- ✅ Clean Architecture (4 Layers)

**Was fehlt noch?**
- ❌ Service Discovery (noch `localhost:9000` hardcoded)
- ❌ UpdateOrder/GetOrder (brauchen wir für Status Updates)
- ❌ RabbitMQ Events (Payments Service braucht Notification)
- ❌ Payments Integration (Stripe)

---

## 🎯 Part 2 Roadmap

1. **UpdateOrder/GetOrder hinzufügen** (Iteration)
2. **Service Discovery mit Consul** (Dynamic Service Location)
3. **RabbitMQ Minimal** (order.created Event)
4. **Payments Service** (Consumer + Stripe)
5. **Dead Letter Queues** (Production Best Practice)

---

## 📝 Phase 5: UpdateOrder & GetOrder (Iteration)

### Step 5.1: WARUM JETZT?

Bis jetzt: Nur CreateOrder
- ❌ Order Status kann nicht geändert werden
- ❌ Order kann nicht abgefragt werden
- ❌ Kitchen kann Order nicht auf "preparing" setzen

**Jetzt erweitern wir:**
- UpdateOrder - Status ändern
- GetOrder - Order abrufen

### Step 5.2: Protobuf erweitern

**Datei:** `common/api/oms.proto`

```proto
syntax = "proto3";

option go_package = "github.com/timour/order-microservices/common/api";

package api;

// Item - Product Info
message ItemWithQuantity {
    string item_id = 1;
    int32 quantity = 2;
}

// Order - Full Order Object (NEU!)
message Order {
    string id = 1;
    string customer_id = 2;
    string status = 3;  // "pending", "paid", "preparing", "ready"
    repeated ItemWithQuantity items = 4;
}

// CreateOrderRequest
message CreateOrderRequest {
    string customer_id = 1;
    repeated ItemWithQuantity items = 2;
}

// CreateOrderResponse
message CreateOrderResponse {
    string order_id = 1;
}

// UpdateOrderRequest (NEU!)
message UpdateOrderRequest {
    string order_id = 1;
    string status = 2;  // New status
}

// GetOrderRequest (NEU!)
message GetOrderRequest {
    string order_id = 1;
}

// OrderService
service OrderService {
    rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
    rpc UpdateOrder(UpdateOrderRequest) returns (Order);  // NEU!
    rpc GetOrder(GetOrderRequest) returns (Order);        // NEU!
}
```

**Code generieren:**
```bash
cd common
make gen
```

### Step 5.3: Store erweitern

**Datei:** `orders/store.go`

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/timour/order-microservices/common/api"
)

var ErrOrderNotFound = errors.New("order not found")

type Order struct {
	ID         string
	CustomerID string
	Status     string
	Items      []*api.ItemWithQuantity
}

type store struct {
	orders map[string]*Order
	mu     sync.RWMutex
}

func NewStore() *store {
	return &store{
		orders: make(map[string]*Order),
	}
}

// Create: Speichert Order MIT Items
func (s *store) Create(ctx context.Context, customerID string, items []*api.ItemWithQuantity) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	orderID := fmt.Sprintf("order_%d", len(s.orders)+1)

	s.orders[orderID] = &Order{
		ID:         orderID,
		CustomerID: customerID,
		Status:     "pending", // Initial Status
		Items:      items,
	}

	fmt.Printf("✅ Order created: %s (status: pending)\n", orderID)
	return orderID, nil
}

// Update: Status ändern (NEU!)
func (s *store) Update(ctx context.Context, orderID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[orderID]
	if !ok {
		return ErrOrderNotFound
	}

	order.Status = status
	fmt.Printf("✅ Order updated: %s (status: %s)\n", orderID, status)
	return nil
}

// Get: Order abrufen (NEU!)
func (s *store) Get(ctx context.Context, orderID string) (*Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, ok := s.orders[orderID]
	if !ok {
		return nil, ErrOrderNotFound
	}

	return order, nil
}
```

### Step 5.4: Types & Service aktualisieren

**Datei:** `orders/types.go`

```go
package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
)

type OrdersService interface {
	CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error)
	UpdateOrder(ctx context.Context, req *api.UpdateOrderRequest) (*api.Order, error)  // NEU!
	GetOrder(ctx context.Context, req *api.GetOrderRequest) (*api.Order, error)        // NEU!
}

type OrdersStore interface {
	Create(ctx context.Context, customerID string, items []*api.ItemWithQuantity) (string, error)
	Update(ctx context.Context, orderID, status string) error  // NEU!
	Get(ctx context.Context, orderID string) (*Order, error)   // NEU!
}
```

**Datei:** `orders/service.go`

```go
package main

import (
	"context"
	"fmt"

	"github.com/timour/order-microservices/common/api"
)

type service struct {
	store OrdersStore
}

func NewService(store OrdersStore) *service {
	return &service{store: store}
}

func (s *service) CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error) {
	if req.CustomerId == "" {
		return nil, fmt.Errorf("customer_id required")
	}

	orderID, err := s.store.Create(ctx, req.CustomerId, req.Items)
	if err != nil {
		return nil, err
	}

	return &api.CreateOrderResponse{
		OrderId: orderID,
	}, nil
}

// UpdateOrder: Status ändern (NEU!)
func (s *service) UpdateOrder(ctx context.Context, req *api.UpdateOrderRequest) (*api.Order, error) {
	if req.OrderId == "" {
		return nil, fmt.Errorf("order_id required")
	}
	if req.Status == "" {
		return nil, fmt.Errorf("status required")
	}

	// Update Status
	if err := s.store.Update(ctx, req.OrderId, req.Status); err != nil {
		return nil, err
	}

	// Get updated Order
	order, err := s.store.Get(ctx, req.OrderId)
	if err != nil {
		return nil, err
	}

	// Convert to Protobuf
	return &api.Order{
		Id:         order.ID,
		CustomerId: order.CustomerID,
		Status:     order.Status,
		Items:      order.Items,
	}, nil
}

// GetOrder: Order abrufen (NEU!)
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

### Step 5.5: gRPC Handler erweitern

**Datei:** `orders/grpc_handler.go`

```go
package main

import (
	"context"
	"log"

	"github.com/timour/order-microservices/common/api"
	"google.golang.org/grpc"
)

type grpcHandler struct {
	api.UnimplementedOrderServiceServer
	service OrdersService
}

func NewGRPCHandler(grpcServer *grpc.Server, service OrdersService) {
	handler := &grpcHandler{
		service: service,
	}
	api.RegisterOrderServiceServer(grpcServer, handler)
	log.Println("✅ gRPC handler registered")
}

func (h *grpcHandler) CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error) {
	return h.service.CreateOrder(ctx, req)
}

// UpdateOrder: Status ändern (NEU!)
func (h *grpcHandler) UpdateOrder(ctx context.Context, req *api.UpdateOrderRequest) (*api.Order, error) {
	return h.service.UpdateOrder(ctx, req)
}

// GetOrder: Order abrufen (NEU!)
func (h *grpcHandler) GetOrder(ctx context.Context, req *api.GetOrderRequest) (*api.Order, error) {
	return h.service.GetOrder(ctx, req)
}
```

### ✅ Test Phase 5

**Test UpdateOrder:**
```bash
grpcurl -plaintext \
  -d '{"order_id": "order_1", "status": "paid"}' \
  localhost:9000 \
  api.OrderService/UpdateOrder
```

**Expected Response:**
```json
{
  "id": "order_1",
  "customerId": "cust_123",
  "status": "paid",
  "items": [...]
}
```

**Test GetOrder:**
```bash
grpcurl -plaintext \
  -d '{"order_id": "order_1"}' \
  localhost:9000 \
  api.OrderService/GetOrder
```

**🎯 CHECKPOINT:** UpdateOrder & GetOrder funktionieren!

---

## 🔍 Phase 6: Service Discovery mit Consul

### Step 6.1: WARUM Service Discovery?

**Problem (Aktuell):**
```go
ordersAddr := "localhost:9000"  // ❌ Hardcoded!
```

**Was wenn:**
- Orders Service läuft auf anderem Server?
- Orders Service skaliert → 3 Pods?
- Orders Service crashed → neuer Pod, neue IP?

**Lösung: Service Discovery**
```
Gateway fragt Consul: "Wo läuft Orders Service?"
Consul antwortet: ["10.0.1.5:9000", "10.0.1.6:9000", "10.0.1.7:9000"]
```

### Step 6.2: Consul starten

**Docker Compose:**
```yaml
# docker-compose.yml
services:
  consul:
    image: consul:1.15.4
    container_name: consul
    ports:
      - "8500:8500"
    command: "agent -dev -ui -client=0.0.0.0"
```

**Starten:**
```bash
docker-compose up -d consul
open http://localhost:8500  # UI öffnen
```

### Step 6.3: Discovery Package (Minimal)

**Datei:** `common/discovery/discovery.go`

```go
package discovery

import "context"

// Registry: Interface für Service Discovery
type Registry interface {
	// Register: Service registrieren
	Register(ctx context.Context, instanceID, serviceName, hostPort string) error

	// Deregister: Service abmelden
	Deregister(ctx context.Context, instanceID, serviceName string) error

	// ServiceAddresses: Alle Adressen für einen Service
	ServiceAddresses(ctx context.Context, serviceName string) ([]string, error)

	// HealthCheck: Service als "healthy" markieren
	HealthCheck(instanceID, serviceName string) error
}
```

**Datei:** `common/discovery/consul/consul.go`

```go
package consul

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	consul "github.com/hashicorp/consul/api"
)

type Registry struct {
	client *consul.Client
}

func NewRegistry(addr string) (*Registry, error) {
	cfg := consul.DefaultConfig()
	cfg.Address = addr

	client, err := consul.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Registry{client: client}, nil
}

// Register: Service bei Consul registrieren
func (r *Registry) Register(ctx context.Context, instanceID, serviceName, hostPort string) error {
	parts := strings.Split(hostPort, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid hostPort: %s", hostPort)
	}

	host := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return err
	}

	return r.client.Agent().ServiceRegister(&consul.AgentServiceRegistration{
		ID:      instanceID,
		Name:    serviceName,
		Address: host,
		Port:    port,
		Check: &consul.AgentServiceCheck{
			CheckID:                        instanceID,
			TLSSkipVerify:                  true,
			TTL:                            "5s",
			DeregisterCriticalServiceAfter: "1m",
		},
	})
}

// Deregister: Service abmelden
func (r *Registry) Deregister(ctx context.Context, instanceID, serviceName string) error {
	return r.client.Agent().ServiceDeregister(instanceID)
}

// ServiceAddresses: Alle verfügbaren Adressen
func (r *Registry) ServiceAddresses(ctx context.Context, serviceName string) ([]string, error) {
	entries, _, err := r.client.Health().Service(serviceName, "", true, nil)
	if err != nil {
		return nil, err
	}

	var addrs []string
	for _, entry := range entries {
		addrs = append(addrs, fmt.Sprintf("%s:%d",
			entry.Service.Address,
			entry.Service.Port,
		))
	}

	return addrs, nil
}

// HealthCheck: Service als gesund markieren
func (r *Registry) HealthCheck(instanceID, serviceName string) error {
	return r.client.Agent().UpdateTTL(instanceID, "online", consul.HealthPassing)
}
```

### Step 6.4: Orders Service registrieren

**Datei:** `orders/main.go`

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/timour/order-microservices/common/discovery/consul"
	"google.golang.org/grpc"
)

func main() {
	ctx := context.Background()

	// 1. Consul Registry (OPTIONAL - nur wenn CONSUL_ADDR gesetzt)
	consulAddr := "localhost:8500" // Später: aus ENV
	var instanceID string

	if consulAddr != "" {
		registry, err := consul.NewRegistry(consulAddr)
		if err != nil {
			log.Fatalf("❌ Failed to connect to Consul: %v", err)
		}

		instanceID = fmt.Sprintf("orders-%d", time.Now().Unix())
		if err := registry.Register(ctx, instanceID, "orders", "localhost:9000"); err != nil {
			log.Fatalf("❌ Failed to register: %v", err)
		}
		log.Printf("✅ Registered at Consul: orders (%s)", instanceID)

		// Health Check Loop
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			for {
				<-ticker.C
				if err := registry.HealthCheck(instanceID, "orders"); err != nil {
					log.Printf("⚠️  Health check failed: %v", err)
				}
			}
		}()

		// Deregister beim Shutdown
		defer func() {
			log.Println("🛑 Deregistering from Consul...")
			registry.Deregister(ctx, instanceID, "orders")
		}()
	}

	// 2. Store, Service, gRPC (wie vorher)
	store := NewStore()
	log.Println("✅ Store initialized")

	svc := NewService(store)
	log.Println("✅ Service initialized")

	grpcServer := grpc.NewServer()
	NewGRPCHandler(grpcServer, svc)

	// 3. Server starten
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

### Step 6.5: Gateway Service Discovery nutzen

**Datei:** `gateway/http_handler.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/discovery"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type handler struct {
	registry discovery.Registry  // ← NEU: Registry statt hardcoded address
}

func NewHandler(registry discovery.Registry) *handler {
	return &handler{registry: registry}
}

type CreateOrderRequest struct {
	CustomerID string `json:"customer_id"`
	Items      []struct {
		ItemID   string `json:"item_id"`
		Quantity int32  `json:"quantity"`
	} `json:"items"`
}

type CreateOrderResponse struct {
	OrderID string `json:"order_id"`
}

func (h *handler) HandleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Service Discovery: Wo läuft Orders Service? (NEU!)
	addrs, err := h.registry.ServiceAddresses(context.Background(), "orders")
	if err != nil {
		http.Error(w, "Orders service unavailable", http.StatusServiceUnavailable)
		return
	}
	if len(addrs) == 0 {
		http.Error(w, "No healthy orders instances", http.StatusServiceUnavailable)
		return
	}

	ordersAddr := addrs[0] // Load Balancing: Später Round-Robin

	// Connect to Orders Service
	conn, err := grpc.Dial(ordersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to connect: %v", err), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	client := api.NewOrderServiceClient(conn)

	// Convert Items
	var items []*api.ItemWithQuantity
	for _, item := range req.Items {
		items = append(items, &api.ItemWithQuantity{
			ItemId:   item.ItemID,
			Quantity: item.Quantity,
		})
	}

	grpcReq := &api.CreateOrderRequest{
		CustomerId: req.CustomerID,
		Items:      items,
	}

	grpcResp, err := client.CreateOrder(context.Background(), grpcReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateOrderResponse{
		OrderID: grpcResp.OrderId,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

**Datei:** `gateway/main.go`

```go
package main

import (
	"log"
	"net/http"

	"github.com/timour/order-microservices/common/discovery/consul"
)

func main() {
	consulAddr := "localhost:8500"
	httpAddr := ":8080"

	// Consul Registry
	registry, err := consul.NewRegistry(consulAddr)
	if err != nil {
		log.Fatalf("❌ Failed to connect to Consul: %v", err)
	}
	log.Println("✅ Connected to Consul")

	// Handler mit Registry
	handler := NewHandler(registry)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/orders/create", handler.HandleCreateOrder)

	log.Printf("🚀 Gateway starting on %s", httpAddr)
	log.Println("📡 Using Service Discovery (Consul)")

	if err := http.ListenAndServe(httpAddr, mux); err != nil {
		log.Fatalf("❌ Failed to start: %v", err)
	}
}
```

### ✅ Test Phase 6

**Consul UI Check:**
1. Open http://localhost:8500/ui
2. "Services" → "orders" sollte grün sein

**Test Gateway:**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "cust_123", "items": []}'
```

**Gateway Logs:**
```
📡 Service Discovery: orders → [localhost:9000]
```

**🎯 CHECKPOINT:** Service Discovery funktioniert! Kein hardcoded Address mehr!

---

## 📨 Phase 7: RabbitMQ Event-Driven (Minimal)

### Step 7.1: WARUM Events?

**Problem (Synchron):**
```
Gateway → Orders → Payments → Wartet... ⏳
```
- Langsam!
- Payment Service down? → Order erstellen failed!

**Lösung (Asynchron):**
```
Gateway → Orders → Order erstellen → ✅ FERTIG!
Orders → RabbitMQ: "order.created" Event
Payments (Consumer) → Liest Event → Payment Link erstellen
```

### Step 7.2: RabbitMQ starten

**Docker Compose:**
```yaml
rabbitmq:
  image: rabbitmq:3.13-management-alpine
  container_name: rabbitmq
  ports:
    - "5672:5672"
    - "15672:15672"
  environment:
    RABBITMQ_DEFAULT_USER: guest
    RABBITMQ_DEFAULT_PASS: guest
```

```bash
docker-compose up -d rabbitmq
open http://localhost:15672  # guest / guest
```

### Step 7.3: Broker Package (Production-Ready mit Auto-Reconnect!)

**Warum ResilientConnection?**
- ❌ Normal: Connection stirbt → Service muss neu starten
- ✅ Resilient: Connection stirbt → Auto-Reconnect mit Exponential Backoff!

**Datei:** `common/broker/resilient.go`

```go
package broker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Event Names (Constants)
const (
	OrderCreatedEvent   = "order.created"
	OrderPaidEvent      = "order.paid"
	OrderPreparingEvent = "order.preparing"
	OrderReadyEvent     = "order.ready"
	DLX                 = "dlx" // Dead Letter Exchange
)

// ResilientConnection: Production-Ready RabbitMQ Connection Manager
// Features:
// ✅ Auto-Reconnect with Exponential Backoff
// ✅ Connection Health Monitoring
// ✅ Thread-Safe Channel Access
// ✅ DLX/DLQ Setup
type ResilientConnection struct {
	url             string
	conn            *amqp.Connection
	channel         *amqp.Channel
	mu              sync.RWMutex
	closed          bool
	notifyConnClose chan *amqp.Error
	notifyChanClose chan *amqp.Error
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewResilientConnection: Creates auto-reconnecting RabbitMQ connection
func NewResilientConnection(user, pass, host, port string) (*ResilientConnection, error) {
	url := fmt.Sprintf("amqp://%s:%s@%s:%s/", user, pass, host, port)

	ctx, cancel := context.WithCancel(context.Background())

	rc := &ResilientConnection{
		url:    url,
		ctx:    ctx,
		cancel: cancel,
	}

	// Initial connection
	if err := rc.connect(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to establish initial connection: %w", err)
	}

	// Start connection monitor (auto-reconnect!)
	go rc.monitorConnection()

	log.Printf("✅ ResilientConnection established (auto-reconnect enabled)")
	return rc, nil
}

// connect: Internal method to establish connection + channel
func (rc *ResilientConnection) connect() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Close existing connection if any
	if rc.conn != nil {
		rc.conn.Close()
	}

	// Establish new connection
	conn, err := amqp.Dial(rc.url)
	if err != nil {
		return fmt.Errorf("failed to dial RabbitMQ: %w", err)
	}

	// Create channel
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create channel: %w", err)
	}

	// ⭐ Setup DLQ/DLX infrastructure (Production Best Practice!)
	if err := createDLQAndDLX(ch); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to setup DLQ: %w", err)
	}

	// Setup exchanges
	if err := createExchanges(ch); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to setup exchanges: %w", err)
	}

	// Update connection state
	rc.conn = conn
	rc.channel = ch
	rc.notifyConnClose = make(chan *amqp.Error, 1)
	rc.notifyChanClose = make(chan *amqp.Error, 1)

	// Monitor connection close events
	rc.conn.NotifyClose(rc.notifyConnClose)
	rc.channel.NotifyClose(rc.notifyChanClose)

	log.Printf("✅ RabbitMQ connection established")
	return nil
}

// monitorConnection: Background goroutine that watches for connection loss
func (rc *ResilientConnection) monitorConnection() {
	for {
		select {
		case <-rc.ctx.Done():
			log.Printf("🛑 RabbitMQ connection monitor stopped (graceful shutdown)")
			return

		case err := <-rc.notifyConnClose:
			if err != nil {
				log.Printf("⚠️  RabbitMQ connection lost: %v", err)
			}

			if rc.closed {
				return
			}

			// Start reconnection loop with exponential backoff
			rc.reconnectWithBackoff()

		case err := <-rc.notifyChanClose:
			if err != nil {
				log.Printf("⚠️  RabbitMQ channel closed: %v", err)
			}

			if rc.closed {
				return
			}

			log.Printf("🔄 Recreating channel (connection still alive)")
			if err := rc.recreateChannel(); err != nil {
				log.Printf("❌ Failed to recreate channel, triggering full reconnect: %v", err)
				rc.reconnectWithBackoff()
			} else {
				log.Printf("✅ Channel recreated successfully")
			}
		}
	}
}

// reconnectWithBackoff: Reconnect loop with exponential backoff
func (rc *ResilientConnection) reconnectWithBackoff() {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for attempt := 1; ; attempt++ {
		select {
		case <-rc.ctx.Done():
			return
		default:
		}

		log.Printf("🔄 Reconnection attempt #%d (backoff: %v)", attempt, backoff)

		err := rc.connect()
		if err == nil {
			log.Printf("✅ Reconnection successful after %d attempts", attempt)
			return
		}

		log.Printf("❌ Reconnection failed: %v (retry in %v)", err, backoff)
		time.Sleep(backoff)

		// Double backoff for next attempt (capped at maxBackoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// recreateChannel: Recreate only the channel (connection still alive)
func (rc *ResilientConnection) recreateChannel() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.channel != nil {
		rc.channel.Close()
	}

	ch, err := rc.conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to create new channel: %w", err)
	}

	if err := createDLQAndDLX(ch); err != nil {
		ch.Close()
		return fmt.Errorf("failed to setup DLQ: %w", err)
	}

	if err := createExchanges(ch); err != nil {
		ch.Close()
		return fmt.Errorf("failed to setup exchanges: %w", err)
	}

	rc.channel = ch
	rc.notifyChanClose = make(chan *amqp.Error, 1)
	rc.channel.NotifyClose(rc.notifyChanClose)

	return nil
}

// Channel: Thread-safe access to underlying channel
func (rc *ResilientConnection) Channel() (*amqp.Channel, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if rc.closed {
		return nil, fmt.Errorf("connection is closed")
	}

	if rc.channel == nil {
		return nil, fmt.Errorf("channel not available")
	}

	return rc.channel, nil
}

// Publish: Thread-safe publish with automatic retry on channel error
func (rc *ResilientConnection) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	ch, err := rc.Channel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}

	return ch.PublishWithContext(
		ctx,
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
		},
	)
}

// Close: Graceful shutdown
func (rc *ResilientConnection) Close() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.closed {
		return nil
	}

	rc.closed = true
	rc.cancel() // Stop monitor goroutine

	if rc.channel != nil {
		rc.channel.Close()
	}

	if rc.conn != nil {
		rc.conn.Close()
	}

	log.Printf("✅ ResilientConnection closed gracefully")
	return nil
}

// createDLQAndDLX: Setup Dead Letter infrastructure
func createDLQAndDLX(ch *amqp.Channel) error {
	// 1. Create DLX (Dead Letter Exchange)
	if err := ch.ExchangeDeclare(
		DLX,      // name
		"direct", // type
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	); err != nil {
		return err
	}
	log.Printf("DLX Exchange created: %s", DLX)

	// 2. Create DLQs for each event type
	events := []string{OrderCreatedEvent, OrderPaidEvent, OrderPreparingEvent, OrderReadyEvent}
	for _, event := range events {
		queueName := event + ".dlq"

		// Declare DLQ
		_, err := ch.QueueDeclare(
			queueName, // name
			true,      // durable
			false,     // auto-delete
			false,     // exclusive
			false,     // no-wait
			nil,       // arguments
		)
		if err != nil {
			return err
		}

		// Bind DLQ to DLX
		if err := ch.QueueBind(
			queueName, // queue name
			event,     // routing key
			DLX,       // exchange
			false,     // no-wait
			nil,       // arguments
		); err != nil {
			return err
		}

		log.Printf("DLQ created and bound: %s → %s (routing key: %s)", queueName, DLX, event)
	}

	return nil
}

// createExchanges: Setup all event exchanges
func createExchanges(ch *amqp.Channel) error {
	events := []string{OrderCreatedEvent, OrderPaidEvent, OrderPreparingEvent, OrderReadyEvent}
	for _, event := range events {
		if err := ch.ExchangeDeclare(
			event,   // name
			"fanout", // type
			true,    // durable
			false,   // auto-deleted
			false,   // internal
			false,   // no-wait
			nil,     // arguments
		); err != nil {
			return err
		}
	}

	log.Printf("Exchanges created: %s, %s, %s, %s",
		OrderCreatedEvent, OrderPaidEvent, OrderPreparingEvent, OrderReadyEvent)
	return nil
}
```

### Step 7.4: Orders Service Event Publisher

**Datei:** `orders/grpc_handler.go` (Update CreateOrder)

```go
package main

import (
	"context"
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
	"google.golang.org/grpc"
)

type grpcHandler struct {
	api.UnimplementedOrderServiceServer
	service OrdersService
	channel *amqp.Channel  // ← NEU: RabbitMQ Channel
}

func NewGRPCHandler(grpcServer *grpc.Server, service OrdersService, channel *amqp.Channel) {
	handler := &grpcHandler{
		service: service,
		channel: channel,
	}
	api.RegisterOrderServiceServer(grpcServer, handler)
	log.Println("✅ gRPC handler registered")
}

func (h *grpcHandler) CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error) {
	resp, err := h.service.CreateOrder(ctx, req)
	if err != nil {
		return nil, err
	}

	// Publish Event: order.created (NEU!)
	event := map[string]interface{}{
		"order_id":    resp.OrderId,
		"customer_id": req.CustomerId,
		"items":       req.Items,
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Printf("❌ Failed to marshal event: %v", err)
		return resp, nil // Order erstellt, Event failed → OK!
	}

	if err := broker.Publish(ctx, h.channel, "order.created", "order.created", eventJSON); err != nil {
		log.Printf("❌ Failed to publish event: %v", err)
		return resp, nil
	}

	log.Printf("📨 Event published: order.created (order_id: %s)", resp.OrderId)
	return resp, nil
}

// UpdateOrder, GetOrder (unverändert)
func (h *grpcHandler) UpdateOrder(ctx context.Context, req *api.UpdateOrderRequest) (*api.Order, error) {
	return h.service.UpdateOrder(ctx, req)
}

func (h *grpcHandler) GetOrder(ctx context.Context, req *api.GetOrderRequest) (*api.Order, error) {
	return h.service.GetOrder(ctx, req)
}
```

**Datei:** `orders/main.go` (Update)

```go
func main() {
	ctx := context.Background()

	// ... Consul Registration (wie vorher) ...

	// RabbitMQ Connection (NEU!)
	ch, closeRabbitMQ, err := broker.Connect("guest", "guest", "localhost", "5672")
	if err != nil {
		log.Fatalf("❌ Failed to connect to RabbitMQ: %v", err)
	}
	defer closeRabbitMQ()
	log.Println("✅ Connected to RabbitMQ")

	// Store, Service (wie vorher)
	store := NewStore()
	svc := NewService(store)

	// gRPC Handler MIT RabbitMQ Channel (NEU!)
	grpcServer := grpc.NewServer()
	NewGRPCHandler(grpcServer, svc, ch)

	// ... Server starten (wie vorher) ...
}
```

### ✅ Test Phase 7

**Order erstellen:**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "cust_123", "items": []}'
```

**Orders Service Logs:**
```
📨 Event published: order.created (order_id: order_1)
```

**RabbitMQ UI Check:**
- Open http://localhost:15672
- "Exchanges" → "order.created" sollte existieren
- "Messages" → 1 Message published

**🎯 CHECKPOINT:** Event Publishing funktioniert!

---

## 💳 Phase 8: Payments Service (Consumer - Minimal)

### Step 8.1: WARUM Payments Service?

- Orders Service published "order.created" Event
- Payments Service consumed Event
- Erstellt Stripe Checkout Link
- Speichert Payment Link in Order (später)

### Step 8.2: Payments Consumer (Minimal)

**Datei:** `payments/consumer.go`

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

// Listen: RabbitMQ Consumer
func (c *Consumer) Listen(ch *amqp.Channel) {
	// 1. Queue deklarieren
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

	// 2. Queue an Exchange binden
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

	// 3. Consumer starten
	msgs, err := ch.Consume(
		q.Name,
		"",
		false, // manual ack!
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to consume: %v", err)
	}

	log.Println("🎧 Listening for order.created events...")

	// 4. Messages verarbeiten
	for msg := range msgs {
		var event map[string]interface{}
		if err := json.Unmarshal(msg.Body, &event); err != nil {
			log.Printf("❌ Failed to unmarshal: %v", err)
			msg.Nack(false, false)
			continue
		}

		orderID, _ := event["order_id"].(string)

		// TODO: Create Stripe Checkout Link
		log.Printf("💳 Processing payment for order: %s", orderID)

		msg.Ack(false)
		log.Printf("✅ Payment processed for order: %s", orderID)
	}
}
```

**Datei:** `payments/main.go`

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
	consumer.Listen(ch) // Blocking!
}
```

### ✅ Test Phase 8

**Terminal 1: Payments Service starten**
```bash
cd payments
go mod init github.com/timour/order-microservices/payments
go mod tidy
go run *.go
```

**Expected Output:**
```
✅ Connected to RabbitMQ
🎧 Listening for order.created events...
```

**Terminal 2: Order erstellen**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "cust_123", "items": []}'
```

**Payments Service Logs:**
```
💳 Processing payment for order: order_1
✅ Payment processed for order: order_1
```

**🎯 CHECKPOINT:** Payments Consumer funktioniert!

---

## 📊 Part 2 Zusammenfassung

### Was haben wir gebaut?

```
Gateway (Port 8080)
     │
     ↓ Service Discovery (Consul)
Orders Service (Port 9000)
     │
     ├─> UpdateOrder, GetOrder (NEU!)
     │
     └─> RabbitMQ: order.created Event
             │
             ↓
Payments Service (Consumer)
     └─> Processing Payment
```

### Schritte die wir gemacht haben:

1. ✅ **UpdateOrder/GetOrder** - Iterativ erweitert
2. ✅ **Service Discovery** - Consul statt hardcoded
3. ✅ **RabbitMQ Minimal** - order.created Event
4. ✅ **Payments Consumer** - Event Processing

### Was fehlt noch?

- ❌ Stripe Integration (echte Payment Links)
- ❌ Dead Letter Queues (Failed Messages)
- ❌ Stock Service (Inventory Management)
- ❌ Kitchen Service (Kitchen Display)

**Weiter mit SETUP3.md!**

Dort fügen wir hinzu:
- Stripe Payment Links
- Dead Letter Queues
- Stock Service (PostgreSQL + Redis)
- Kitchen Service
- Production Features

---

---

## 🔄 Phase 9: Dead Letter Queues (DLQ) & Retry Logic (Production Best Practice!)

### Step 9.1: WARUM Dead Letter Queues?

**Problem ohne DLQ:**
```
Message Processing failed → Message lost forever! ❌
```

**Mit DLQ:**
```
Message Processing failed → Retry 3x → Still failing → Send to DLQ for investigation ✅
```

### Step 9.2: HandleRetry Function

**Datei:** `common/broker/retry.go`

```go
package broker

import (
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	MaxRetries     = 3
	RetryHeaderKey = "x-retry-count"
)

// HandleRetry: Smart retry logic with DLQ fallback
// How it works:
// 1. Check retry count in message headers
// 2. If < 3 retries: Increment count & requeue
// 3. If >= 3 retries: Send to DLQ (via Nack with requeue=false)
func HandleRetry(ch *amqp.Channel, d *amqp.Delivery) error {
	// Get current retry count
	retryCount := int64(0)
	if val, ok := d.Headers[RetryHeaderKey]; ok {
		if count, ok := val.(int64); ok {
			retryCount = count
		}
	}

	// Check if max retries reached
	if retryCount >= MaxRetries {
		// Max retries reached → Will go to DLQ via x-dead-letter-exchange
		return fmt.Errorf("max retries (%d) reached, sending to DLQ", MaxRetries)
	}

	// Increment retry count
	retryCount++

	// Republish with incremented retry count
	headers := make(amqp.Table)
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers[RetryHeaderKey] = retryCount

	// Wait before retry (exponential backoff)
	backoff := time.Duration(retryCount) * time.Second
	time.Sleep(backoff)

	// Republish to same queue
	return ch.PublishWithContext(
		ch.NotifyReturn(make(chan amqp.Return)),
		d.Exchange,
		d.RoutingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			Headers:         headers,
			ContentType:     d.ContentType,
			Body:            d.Body,
			DeliveryMode:    d.DeliveryMode,
			Priority:        d.Priority,
			CorrelationId:   d.CorrelationId,
			ReplyTo:         d.ReplyTo,
			Expiration:      d.Expiration,
			MessageId:       d.MessageId,
			Timestamp:       d.Timestamp,
			Type:            d.Type,
			UserId:          d.UserId,
			AppId:           d.AppId,
		},
	)
}
```

### Step 9.3: Nutzen der HandleRetry Function

**In Payments Consumer:**

```go
// In consumer.go bei Error Handling:

func (c *Consumer) Listen(ch *amqp.Channel) {
	// ... Queue setup ...

	for d := range msgs {
		var event map[string]interface{}
		if err := json.Unmarshal(d.Body, &event); err != nil {
			log.Printf("❌ Failed to unmarshal: %v", err)

			// Smart Retry: Retry 3x → Then DLQ
			if err := broker.HandleRetry(ch, &d); err != nil {
				log.Printf("⚠️  Retry handling failed: %v", err)
			}
			d.Nack(false, false) // Don't requeue (HandleRetry already did!)
			continue
		}

		// Process payment...
		if err := c.processPayment(event); err != nil {
			log.Printf("❌ Payment processing failed: %v", err)

			// Smart Retry
			if err := broker.HandleRetry(ch, &d); err != nil {
				log.Printf("⚠️  Max retries reached, message goes to DLQ")
			}
			d.Nack(false, false)
			continue
		}

		// Success!
		d.Ack(false)
	}
}
```

### Step 9.4: Queue mit DLX Setup

**In Consumer:**

```go
// Queue Declaration mit DLX
q, err := ch.QueueDeclare(
	"payments_order_created",
	true,  // durable
	false, // auto-delete
	false, // exclusive
	false, // no-wait
	amqp.Table{
		"x-dead-letter-exchange": broker.DLX, // ⭐ Failed messages → DLX
	},
)
```

### ✅ Test Dead Letter Queue

**Test Failed Message:**

```go
// In Payments Consumer - Add test case:
if orderID == "FAIL_TEST" {
	log.Printf("🧪 Deliberately failing for DLQ test")
	if err := broker.HandleRetry(ch, &d); err != nil {
		log.Printf("Max retries, sending to DLQ")
	}
	d.Nack(false, false)
	continue
}
```

**Create Test Order:**

```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "FAIL_TEST", "items": []}'
```

**Check RabbitMQ UI:**
1. Open http://localhost:15672
2. "Queues" → `order.created.dlq` should have 1 message
3. Message was retried 3x, then moved to DLQ

**Logs Output:**
```
🧪 Deliberately failing for DLQ test (retry 1/3)
🧪 Deliberately failing for DLQ test (retry 2/3)
🧪 Deliberately failing for DLQ test (retry 3/3)
⚠️  Max retries reached, message goes to DLQ
```

---

## 📊 Part 2 Final Summary

### Production-Ready Features We Implemented:

```
✅ ResilientConnection (Auto-Reconnect with Exponential Backoff)
✅ Dead Letter Exchange (DLX)
✅ Dead Letter Queues (DLQ) for each event type
✅ Smart Retry Logic (3 attempts with exponential backoff)
✅ Thread-Safe Channel Access
✅ Graceful Shutdown
✅ Connection Health Monitoring
```

### Architecture (Complete):

```
Gateway (Port 8080)
     │
     ↓ Service Discovery (Consul)
Orders Service (Port 9000)
     │
     ├─> UpdateOrder, GetOrder
     │
     └─> RabbitMQ (ResilientConnection):
             │
             └─> Exchange: order.created
                     │
                     ↓
              Payments Consumer
                     │
                     ├─> Success → Ack
                     ├─> Fail → Retry 3x
                     └─> Still Fail → DLQ
```

### What's Next (SETUP3.md):

1. **ResilientConsumer Pattern** - Auto-restart on channel death
2. **Stripe Integration** - Real payment links
3. **Stock Service** - PostgreSQL + Redis
4. **Kitchen Service** - Kitchen Display
5. **Docker Compose** - Full deployment

**🎯 Part 2 Complete!** Du hast:
- ✅ Iterativ erweitert (UpdateOrder/GetOrder)
- ✅ Service Discovery verstanden (Consul)
- ✅ Event-Driven Architecture implementiert (RabbitMQ)
- ✅ Production-Ready Features: Auto-Reconnect, DLQ, Retry Logic
- ✅ Jeden Step getestet und WARUM verstanden
