# SETUP Part 1: Foundation & Orders Service (Step-by-Step)

> **McDonald's-Style Order Management System** - Von Zero zum fertigen Microservice

---

## 📚 Einleitung

Du baust ein **Production-Ready Order Management System** wie bei McDonald's:
- **20-100 Filialen** regional, **1000+ weltweit**
- **Microservices** in Go mit gRPC
- **Kubernetes** für Skalierung

### Warum Microservices?

**Ohne Microservices (Monolith):**
```
1 Server crashed → KOMPLETTES System down
```

**Mit Microservices:**
```
Payment Service crashed → Orders + Kitchen laufen weiter
```

---

## 🏗️ System Architecture (Endresultat)

```
CUSTOMER APP (React)
     │ HTTP
     ↓
GATEWAY (Port 8080)
     │ gRPC
     ↓
ORDERS SERVICE (Port 9000)
     │
     ↓
MongoDB
```

**In Part 1 bauen wir:**
- Orders Service (gRPC)
- Gateway (HTTP → gRPC)
- Clean Architecture (4 Layers)

---

## 📦 Phase 0: Project Setup

### Directory Structure

```bash
order-microservices/
├── common/              # Shared code
│   └── api/            # Protobuf definitions
├── orders/             # Order Management
├── gateway/            # HTTP → gRPC Gateway
└── go.work             # Go Workspace
```

### Create Project

```bash
# Create root
mkdir order-microservices
cd order-microservices

# Create service directories
mkdir -p common/api orders gateway

# Initialize Go Workspace
go work init

# Common module (shared code)
cd common
go mod init github.com/timour/order-microservices/common
cd ..

# Orders Service
cd orders
go mod init github.com/timour/order-microservices/orders
cd ..

# Gateway Service
cd gateway
go mod init github.com/timour/order-microservices/gateway
cd ..

# Add to workspace
go work use ./common ./orders ./gateway
```

---

## 🏛️ Clean Architecture - Warum?

| Feature | Ohne Clean Arch | Mit Clean Arch |
|---------|-----------------|----------------|
| **Debugging** | 30 Min 😫 | 30 Sek ⚡ |
| **Testing** | Langsam (DB nötig) 🐌 | Schnell (Mocks) ⚡ |
| **DB Migration** | 1 Woche 😱 | 1 Tag ⚡ |

### 4-Layer Architecture

```
┌────────────────────────────────────────────┐
│ 1️⃣ PRESENTATION (grpc_handler.go)        │
│    → Empfängt Requests                    │
├────────────────────────────────────────────┤
│ 2️⃣ BUSINESS LOGIC (service.go)           │
│    → Validation, Business Rules           │
├────────────────────────────────────────────┤
│ 3️⃣ DATA ACCESS (store.go)                │
│    → Database Operations                  │
├────────────────────────────────────────────┤
│ 4️⃣ DOMAIN (types.go)                     │
│    → Interfaces (Contracts)               │
└────────────────────────────────────────────┘
```

**File Structure:**
```
orders/
├── types.go         # Interfaces (ZUERST!)
├── store.go         # Data Access Layer
├── service.go       # Business Logic Layer
├── grpc_handler.go  # Presentation Layer
└── main.go          # Entry Point
```

---

## 🎯 Phase 1: Minimal Orders Service (CreateOrder Only)

### Step 1.1: types.go - Interface Definition

**WARUM ZUERST?**
- Definiert Vertrag zwischen Schichten
- Ermöglicht Testing mit Mocks
- Clean Architecture Prinzip

**Datei:** `orders/types.go`

```go
package main

import "context"

// OrdersService: Business Logic Interface
// Nur CreateOrder - Rest kommt später!
type OrdersService interface {
	CreateOrder(ctx context.Context, customerID string) error
}

// OrdersStore: Data Access Interface
// Nur Create - Rest kommt später!
type OrdersStore interface {
	Create(ctx context.Context, customerID string) error
}
```

**💡 Warum nur CreateOrder?**
- Wir fangen minimal an!
- UpdateOrder, GetOrder brauchen wir erst später
- Ein Feature funktionsfähig ist besser als viele halbfertige

### Step 1.2: store.go - Data Access Layer (In-Memory)

**WARUM IN-MEMORY?**
- MongoDB Setup dauert zu lang
- Wir testen erstmal die Architektur
- Später: 2 Zeilen ändern → MongoDB

**Datei:** `orders/store.go`

```go
package main

import (
	"context"
	"fmt"
	"sync"
)

type store struct {
	orders map[string]string  // orderID → customerID
	mu     sync.RWMutex       // Thread-safe
}

func NewStore() *store {
	return &store{
		orders: make(map[string]string),
	}
}

// Create: Speichert Order (später: MongoDB InsertOne)
func (s *store) Create(ctx context.Context, customerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate simple order ID
	orderID := fmt.Sprintf("order_%d", len(s.orders)+1)
	s.orders[orderID] = customerID

	fmt.Printf("✅ Order created: %s (customer: %s)\n", orderID, customerID)
	return nil
}
```

**💡 Warum sync.RWMutex?**
- `Lock()`: Writes exklusiv
- Thread-safe für Production (auch wenn wir erst 1 Request haben)
- Best Practice von Anfang an!

### Step 1.3: service.go - Business Logic Layer

**WARUM SERVICE LAYER?**
- Business Rules zentral (später: Stock Check, Validation)
- Keine DB/HTTP Details
- Testbar ohne DB

**Datei:** `orders/service.go`

```go
package main

import "context"

type service struct {
	store OrdersStore  // Interface (für Testing: Mock Store)
}

func NewService(store OrdersStore) *service {
	return &service{store: store}
}

// CreateOrder: Business Logic für Order Creation
func (s *service) CreateOrder(ctx context.Context, customerID string) error {
	// TODO später: Validation, Stock Check
	return s.store.Create(ctx, customerID)
}
```

**💡 Warum so leer?**
- Jetzt: Nur Delegation zum Store
- Später: Validation, Stock Check, Business Rules
- Architektur steht, Logik kommt Schritt für Schritt!

### Step 1.4: main.go - Entry Point

**Datei:** `orders/main.go`

```go
package main

import (
	"context"
	"fmt"
)

func main() {
	// 1. Store erstellen
	store := NewStore()
	fmt.Println("✅ Store initialized")

	// 2. Service erstellen (Dependency Injection)
	svc := NewService(store)
	fmt.Println("✅ Service initialized")

	// 3. Test: CreateOrder
	ctx := context.Background()
	if err := svc.CreateOrder(ctx, "customer_123"); err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	fmt.Println("🎉 Orders Service working!")
}
```

### ✅ Test Phase 1.1

```bash
cd orders
go run *.go
```

**Expected Output:**
```
✅ Store initialized
✅ Service initialized
✅ Order created: order_1 (customer: customer_123)
🎉 Orders Service working!
```

**🎯 CHECKPOINT:** Wir haben Clean Architecture (4 Layers) aufgebaut!

---

## 📡 Phase 2: Protobuf & gRPC (Minimal)

### Step 2.1: Install Protobuf Tools

```bash
# Install Protoc Plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

### Step 2.2: Minimal Protobuf Definition

**WARUM MINIMAL?**
- Erst CreateOrder funktionsfähig machen
- Andere Endpoints später hinzufügen
- Jeder Step testbar!

**Datei:** `common/api/oms.proto`

```proto
syntax = "proto3";

option go_package = "github.com/timour/order-microservices/common/api";

package api;

// CreateOrderRequest - Nur customer_id!
message CreateOrderRequest {
    string customer_id = 1;
}

// CreateOrderResponse - Nur order_id!
message CreateOrderResponse {
    string order_id = 1;
}

// OrderService - Nur CreateOrder!
service OrderService {
    rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
}
```

**💡 Was fehlt noch?**
- Items (kommt in Step 2.6)
- UpdateOrder, GetOrder (kommt in Step 2.7)
- Erst mal: Ein Endpoint funktionsfähig!

### Step 2.3: Code Generation

**Datei:** `common/Makefile`

```makefile
.PHONY: gen
gen:
	@protoc --go_out=. --go_opt=paths=source_relative \
	        --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	        api/oms.proto
	@echo "✅ Protocol buffers generated"
```

**Execute:**

```bash
cd common
make gen
```

**Generated Files:**
- `common/api/oms.pb.go` - Message structs
- `common/api/oms_grpc.pb.go` - Service interfaces

### Step 2.4: Update types.go für gRPC

**WARUM UPDATE?**
- Vorher: `customerID string`
- Jetzt: Protobuf Messages nutzen
- Store bleibt gleich (nur Interface ändert sich)

**Datei:** `orders/types.go`

```go
package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
)

// OrdersService: Business Logic Interface
type OrdersService interface {
	CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error)
}

// OrdersStore: Data Access Interface (bleibt gleich!)
type OrdersStore interface {
	Create(ctx context.Context, customerID string) error
}
```

### Step 2.5: Update service.go

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

// CreateOrder: Business Logic mit Protobuf
func (s *service) CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error) {
	// Validate
	if req.CustomerId == "" {
		return nil, fmt.Errorf("customer_id required")
	}

	// Store (nutzt immer noch string!)
	if err := s.store.Create(ctx, req.CustomerId); err != nil {
		return nil, err
	}

	// Response
	return &api.CreateOrderResponse{
		OrderId: "order_1", // TODO: Get from store
	}, nil
}
```

### Step 2.6: gRPC Handler - Presentation Layer

**WARUM GRPC HANDLER?**
- Empfängt gRPC Requests (Protobuf)
- Validiert Input
- Ruft Service Layer auf
- Returned gRPC Response

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

// CreateOrder: gRPC Endpoint
func (h *grpcHandler) CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error) {
	return h.service.CreateOrder(ctx, req)
}
```

### Step 2.7: Update main.go für gRPC Server

**Datei:** `orders/main.go`

```go
package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
)

func main() {
	// 1. Store erstellen
	store := NewStore()
	log.Println("✅ Store initialized")

	// 2. Service erstellen
	svc := NewService(store)
	log.Println("✅ Service initialized")

	// 3. gRPC Server erstellen
	grpcServer := grpc.NewServer()
	log.Println("✅ gRPC server created")

	// 4. gRPC Handler registrieren
	NewGRPCHandler(grpcServer, svc)

	// 5. Server starten
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

### ✅ Test Phase 2

```bash
cd orders
go mod tidy
go run *.go
```

**Expected Output:**
```
✅ Store initialized
✅ Service initialized
✅ gRPC server created
✅ gRPC handler registered
🚀 Orders Service listening on :9000
```

**Test with grpcurl:**

```bash
# Install grpcurl
brew install grpcurl

# Test CreateOrder
grpcurl -plaintext \
  -d '{"customer_id": "cust_123"}' \
  localhost:9000 \
  api.OrderService/CreateOrder
```

**Expected Response:**
```json
{
  "orderId": "order_1"
}
```

**🎯 CHECKPOINT:** Orders Service empfängt gRPC Requests!

---

## 🌐 Phase 3: Gateway Service (HTTP → gRPC)

### Step 3.1: Warum Gateway?

**Problem:**
```
Customer App (JavaScript) → gRPC? ❌ Browser unterstützt kein gRPC!
```

**Lösung:**
```
Customer App → HTTP POST → Gateway → gRPC → Orders Service
```

### Step 3.2: HTTP Handler (Minimal)

**Datei:** `gateway/http_handler.go`

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/timour/order-microservices/common/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type handler struct {
	ordersAddr string // "localhost:9000"
}

func NewHandler(ordersAddr string) *handler {
	return &handler{ordersAddr: ordersAddr}
}

// CreateOrderRequest: HTTP Request Body
type CreateOrderRequest struct {
	CustomerID string `json:"customer_id"`
}

// CreateOrderResponse: HTTP Response Body
type CreateOrderResponse struct {
	OrderID string `json:"order_id"`
}

// POST /api/orders/create
func (h *handler) HandleCreateOrder(w http.ResponseWriter, r *http.Request) {
	// 1. Parse HTTP Request Body (JSON)
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// 2. Connect to Orders Service (gRPC)
	conn, err := grpc.Dial(h.ordersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	// 3. Create gRPC Client
	client := api.NewOrderServiceClient(conn)

	// 4. Call gRPC CreateOrder
	grpcReq := &api.CreateOrderRequest{
		CustomerId: req.CustomerID,
	}

	grpcResp, err := client.CreateOrder(context.Background(), grpcReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 5. Return HTTP Response (JSON)
	resp := CreateOrderResponse{
		OrderID: grpcResp.OrderId,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

**💡 HTTP → gRPC Translation:**
1. HTTP Request Body (JSON) → Protobuf Struct
2. gRPC Call
3. Protobuf Response → HTTP Response (JSON)

### Step 3.3: Main (HTTP Server)

**Datei:** `gateway/main.go`

```go
package main

import (
	"log"
	"net/http"
)

func main() {
	ordersAddr := "localhost:9000" // Orders Service gRPC Address
	httpAddr := ":8080"            // Gateway HTTP Address

	handler := NewHandler(ordersAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/orders/create", handler.HandleCreateOrder)

	log.Printf("🚀 Gateway starting on %s", httpAddr)
	log.Printf("📡 Connecting to Orders Service at %s", ordersAddr)

	if err := http.ListenAndServe(httpAddr, mux); err != nil {
		log.Fatalf("❌ Failed to start: %v", err)
	}
}
```

### ✅ Test Phase 3

**Terminal 1: Orders Service starten**
```bash
cd orders
go run *.go
```

**Terminal 2: Gateway starten**
```bash
cd gateway
go mod tidy
go run *.go
```

**Terminal 3: HTTP Request senden**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "cust_123"}'
```

**Expected Response:**
```json
{
  "order_id": "order_1"
}
```

**🎯 CHECKPOINT:** HTTP → Gateway → gRPC funktioniert!

---

## 🔧 Phase 4: Items hinzufügen (Iteration)

### Step 4.1: WARUM JETZT ITEMS?

Bis jetzt:
- ✅ CreateOrder funktioniert
- ✅ HTTP → gRPC funktioniert
- ❌ Aber: Keine Items! (Burger, Pommes)

**Jetzt erweitern wir:**
- Protobuf: Items hinzufügen
- Store: Items speichern
- Alles andere bleibt kompatibel!

### Step 4.2: Protobuf erweitern

**Datei:** `common/api/oms.proto`

```proto
syntax = "proto3";

option go_package = "github.com/timour/order-microservices/common/api";

package api;

// Item - Product Info
message ItemWithQuantity {
    string item_id = 1;   // "burger"
    int32 quantity = 2;   // 2
}

// CreateOrderRequest - MIT Items!
message CreateOrderRequest {
    string customer_id = 1;
    repeated ItemWithQuantity items = 2;  // NEU!
}

// CreateOrderResponse
message CreateOrderResponse {
    string order_id = 1;
}

// OrderService
service OrderService {
    rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
}
```

**Code neu generieren:**
```bash
cd common
make gen
```

### Step 4.3: Store erweitern

**Datei:** `orders/store.go`

```go
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/timour/order-microservices/common/api"
)

type Order struct {
	ID         string
	CustomerID string
	Items      []*api.ItemWithQuantity
}

type store struct {
	orders map[string]*Order // orderID → Order
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
		Items:      items,
	}

	fmt.Printf("✅ Order created: %s (customer: %s, items: %d)\n",
		orderID, customerID, len(items))
	return orderID, nil
}
```

### Step 4.4: Types & Service aktualisieren

**Datei:** `orders/types.go`

```go
package main

import (
	"context"

	"github.com/timour/order-microservices/common/api"
)

type OrdersService interface {
	CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.CreateOrderResponse, error)
}

type OrdersStore interface {
	Create(ctx context.Context, customerID string, items []*api.ItemWithQuantity) (string, error)
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

	// Items optional (können leer sein)
	orderID, err := s.store.Create(ctx, req.CustomerId, req.Items)
	if err != nil {
		return nil, err
	}

	return &api.CreateOrderResponse{
		OrderId: orderID,
	}, nil
}
```

### Step 4.5: Gateway aktualisieren

**Datei:** `gateway/http_handler.go`

```go
type CreateOrderRequest struct {
	CustomerID string `json:"customer_id"`
	Items      []struct {
		ItemID   string `json:"item_id"`
		Quantity int32  `json:"quantity"`
	} `json:"items"`
}

func (h *handler) HandleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	conn, err := grpc.Dial(h.ordersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	client := api.NewOrderServiceClient(conn)

	// Convert HTTP Items → Protobuf Items
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

### ✅ Test Phase 4

**Test with Items:**
```bash
curl -X POST http://localhost:8080/api/orders/create \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "cust_123",
    "items": [
      {"item_id": "burger", "quantity": 2},
      {"item_id": "fries", "quantity": 1}
    ]
  }'
```

**Expected Output (Orders Service):**
```
✅ Order created: order_1 (customer: cust_123, items: 2)
```

**🎯 CHECKPOINT:** Items funktionieren! Wir haben iterativ erweitert!

---

## 📊 Part 1 Zusammenfassung

### Was haben wir gebaut?

```
HTTP Request
     │
     ↓
GATEWAY (Port 8080)
  - Parse JSON
  - Convert to Protobuf
     │ gRPC
     ↓
ORDERS SERVICE (Port 9000)
  ┌─────────────────────┐
  │ grpc_handler.go     │ → Presentation
  │ service.go          │ → Business Logic
  │ store.go            │ → Data Access
  │ types.go            │ → Interfaces
  └─────────────────────┘
```

### Schritte die wir gemacht haben:

1. ✅ **Clean Architecture** - 4 Layers mit Interfaces
2. ✅ **Minimal Start** - Nur CreateOrder, nur customerID
3. ✅ **Protobuf & gRPC** - Binary Protocol statt JSON
4. ✅ **Gateway** - HTTP → gRPC Translation
5. ✅ **Iterative Erweiterung** - Items hinzugefügt

### Was fehlt noch?

- ❌ UpdateOrder, GetOrder (brauchen wir für Status Updates)
- ❌ MongoDB (noch In-Memory)
- ❌ Service Discovery (noch hardcoded localhost:9000)
- ❌ RabbitMQ (noch keine Events)

**Weiter mit SETUP2.md!**

Dort fügen wir hinzu:
- UpdateOrder/GetOrder
- Service Discovery (Consul)
- RabbitMQ (Events)
- Payments Service

---

**🎯 Part 1 Complete!** Du hast:
- ✅ Clean Architecture verstanden
- ✅ Minimal angefangen und iterativ erweitert
- ✅ Jeden Step getestet
- ✅ WARUM jede Erweiterung gemacht wurde
