# Advanced Setup - Production Best Practices

## 🚀 Production-Ready Architecture

This document covers advanced production patterns implemented in the Order Management System, following **Google SRE Best Practices**.

---

## 📡 RabbitMQ Resilient Connection Pattern

### Problem Statement

Traditional RabbitMQ connections fail silently when:
- Network timeouts occur
- Channels close due to inactivity
- RabbitMQ restarts or upgrades
- Container orchestration (K8s) reschedules pods

**Symptoms:**
```
ERROR: failed to publish event after retries
error: "channel/connection is not open"
```

### Solution: ResilientConnection

**Location:** `common/broker/resilient.go`

**Features:**
- ✅ Auto-Reconnect with Exponential Backoff (1s → 2s → 4s → 8s → max 30s)
- ✅ Connection Monitoring (detects connection loss)
- ✅ **Channel Monitoring** (detects channel closure independently)
- ✅ Thread-Safe Channel Access (RWMutex)
- ✅ Graceful Degradation
- ✅ Circuit Breaker Pattern

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│ ResilientConnection                                      │
├─────────────────────────────────────────────────────────┤
│  Connection ──┐                                          │
│               │                                          │
│  Channel   ───┼──> NotifyClose() ──> monitorConnection()│
│               │                           │              │
│               └───────────────────────────┘              │
│                                           │              │
│  On Connection Close ────> reconnectWithBackoff()       │
│  On Channel Close ────────> recreateChannel()           │
└─────────────────────────────────────────────────────────┘
```

### Key Innovation: Channel Recreation

**Problem:** Channels can die while connection is still alive!

**Traditional Approach:**
- Only monitor connection → Channel failures not detected
- Result: Publish fails with "channel not open"

**Our Approach:**
```go
// Monitor BOTH connection AND channel close events
rc.conn.NotifyClose(rc.notifyConnClose)
rc.channel.NotifyClose(rc.notifyChanClose)

// On channel close → recreate ONLY channel (fast!)
func (rc *ResilientConnection) recreateChannel() error {
    // Close old channel
    if rc.channel != nil {
        rc.channel.Close()
    }

    // Create fresh channel from existing connection
    ch, err := rc.conn.Channel()
    // ... setup DLQ/DLX/Exchanges ...

    return nil
}
```

**Impact:** Channel recreation in **milliseconds** vs full reconnect in **seconds**!

### Usage

#### Basic Usage

```go
// Initialize ResilientConnection
resilientConn, err := broker.NewResilientConnection(
    amqpUser,
    amqpPass,
    amqpHost,
    amqpPort,
)
if err != nil {
    log.Fatal("failed to connect to rabbitmq:", err)
}
defer resilientConn.Close()

// Get channel for publishing/consuming
ch, err := resilientConn.Channel()
if err != nil {
    return err
}

// Use channel as normal
consumer.Listen(ch)
```

#### Publishing with Auto-Retry

```go
// ResilientConnection handles retries automatically
err := resilientConn.Publish(
    ctx,
    "order.created",  // exchange
    "order.created",  // routing key
    eventJSON,        // body
)
```

### Exponential Backoff Strategy

```
Attempt 1: Wait 1 second   → Retry
Attempt 2: Wait 2 seconds  → Retry
Attempt 3: Wait 4 seconds  → Retry
Attempt 4: Wait 8 seconds  → Retry
Attempt 5+: Wait 30 seconds → Retry (capped)
```

**Why Exponential Backoff?**
- Gives RabbitMQ time to recover
- Prevents connection storms (DDoS on broker)
- Google SRE best practice
- Production standard for distributed systems

### Service Migration Status

| Service | Before | After | Status |
|---------|--------|-------|--------|
| **Orders** | ResilientConnection (no channel monitoring) | ResilientConnection + Channel Monitoring | ✅ Enhanced |
| **Payments** | broker.Connect() (no auto-reconnect) | ResilientConnection | ✅ Migrated |
| **Stock** | broker.Connect() (no auto-reconnect) | ResilientConnection | ✅ Migrated |
| **Kitchen** | broker.Connect() (no auto-reconnect) | ResilientConnection | ✅ Migrated |

### Verification

Check logs for successful connection:

```bash
docker logs orders-prod | grep ResilientConnection
# Output: ✅ ResilientConnection established (auto-reconnect enabled)

docker logs payments-prod | grep ResilientConnection
# Output: ✅ ResilientConnection established (auto-reconnect enabled)

docker logs stock-prod | grep ResilientConnection
# Output: ✅ ResilientConnection established (auto-reconnect enabled)

docker logs kitchen-prod | grep ResilientConnection
# Output: ✅ ResilientConnection established (auto-reconnect enabled)
```

---

## 🔄 ResilientConsumer Pattern

### Problem Statement

**Even with ResilientConnection, consumers can get stuck!**

Traditional consumer pattern:
```go
// Get channel ONCE
ch, err := resilientConn.Channel()

// Use channel forever
consumer.Listen(ch)  // ← Channel dies → Consumer dies!
```

**What happens:**
1. ResilientConnection creates channel
2. Channel passed to consumer
3. **Channel dies** (timeout, error, RabbitMQ restart)
4. Consumer goroutine exits (`for d := range msgs` breaks)
5. **No auto-restart!** Messages pile up in queue ❌

**Symptoms:**
```bash
# RabbitMQ queue has messages
$ rabbitmqctl list_queues
order.created  3  3  0
               ↑  ↑  ↑
             total ready unack

# But consumer not consuming!
$ docker logs payments-prod | tail
# No recent "received message" logs
```

### Solution: ResilientConsumer

**Location:** `payments/consumer_resilient.go`

**Features:**
- ✅ **Channel Monitoring**: Detects channel closure via `NotifyClose`
- ✅ **Auto-Restart Loop**: Gets fresh channel when old one dies
- ✅ **Retry Logic**: 5s backoff on errors, 2s between restarts
- ✅ **Context Support**: Graceful shutdown with `context.Context`
- ✅ **Same Features**: DLQ, Retry, Tracing preserved

### Architecture

```
┌──────────────────────────────────────────────────────┐
│ ResilientConsumer                                     │
├──────────────────────────────────────────────────────┤
│                                                       │
│  Loop Forever:                                        │
│    1. Get fresh channel from ResilientConnection     │
│    2. Start consuming on channel                      │
│    3. Monitor channel closure (NotifyClose)           │
│    4. Channel dies? → Back to step 1!                 │
│                                                       │
│  ┌─────────────────────────────────────┐             │
│  │ consumeOnChannel(ch)                 │             │
│  ├─────────────────────────────────────┤             │
│  │  - Declare queue                     │             │
│  │  - Register consumer                 │             │
│  │  - Monitor: ch.NotifyClose(closeCh)  │             │
│  │  - Process messages                  │             │
│  │  - On close → return (loop restarts) │             │
│  └─────────────────────────────────────┘             │
│                                                       │
└──────────────────────────────────────────────────────┘
```

### Implementation

```go
// consumer_resilient.go
type ResilientConsumer struct {
    service        PaymentService
    logger         *slog.Logger
    resilientConn  *broker.ResilientConnection
    ctx            context.Context
    cancel         context.CancelFunc
}

func (rc *ResilientConsumer) consume() {
    for {
        select {
        case <-rc.ctx.Done():
            return
        default:
            // 1. Get fresh channel from ResilientConnection
            ch, err := rc.resilientConn.Channel()
            if err != nil {
                rc.logger.Error("failed to get channel, retrying in 5s")
                time.Sleep(5 * time.Second)
                continue
            }

            // 2. Start consuming (blocks until channel closes)
            if err := rc.consumeOnChannel(ch); err != nil {
                rc.logger.Warn("consumer stopped, restarting...", err)
                time.Sleep(2 * time.Second)
                continue
            }
        }
    }
}

func (rc *ResilientConsumer) consumeOnChannel(ch *amqp.Channel) error {
    // Declare queue, register consumer
    msgs, err := ch.Consume(...)

    // Monitor channel closure
    closeCh := make(chan *amqp.Error)
    ch.NotifyClose(closeCh)

    // Process messages
    for {
        select {
        case <-rc.ctx.Done():
            return nil
        case err := <-closeCh:
            // Channel closed! Return to restart
            rc.logger.Warn("channel closed, will restart consumer", err)
            return err
        case d := <-msgs:
            // Process message...
        }
    }
}
```

### Usage

**Before (Vulnerable to channel death):**
```go
// app.go - OLD
ch, err := resilientConn.Channel()
consumer := NewConsumer(svc, logger)
consumer.Listen(ch)  // ❌ Dies when channel dies
```

**After (Auto-restart on channel death):**
```go
// app.go - NEW
consumer := NewResilientConsumer(svc, logger, resilientConn)
consumer.Start()  // ✅ Auto-restarts when channel dies!

// Block until shutdown
<-ctx.Done()
```

### Testing Auto-Restart

```bash
# 1. Create order (consumer processes)
curl -X POST http://localhost:8080/api/orders/create \
  -d '{"customer_id":"test","items":[...]}'

# 2. Kill all RabbitMQ connections (simulates channel death)
docker exec rabbitmq-prod rabbitmqctl close_all_connections "Testing"

# 3. Check logs - should see auto-restart
docker logs payments-prod --tail=20
# Expected output:
# ⚠️  "channel closed, will restart consumer"
# ✅ "starting consumer on fresh channel"

# 4. Create another order (consumer still works!)
curl -X POST http://localhost:8080/api/orders/create \
  -d '{"customer_id":"after_restart","items":[...]}'

# 5. Verify processing
docker logs payments-prod | grep "payment link created"
# Should see BOTH orders processed ✅
```

### Service Migration Status

| Service | Before | After | Status |
|---------|--------|-------|--------|
| **Payments** | Regular Consumer (no auto-restart) | ResilientConsumer ✅ | ✅ Migrated |
| **Kitchen** | Regular Consumer | ResilientConsumer (TODO) | ⏳ Pending |
| **Stock** | N/A (no consumer) | N/A | - |
| **Orders** | N/A (only publishes) | N/A | - |

### Benefits

**Before Fix:**
- Channel dies → Messages pile up ❌
- Manual restart required: `docker restart payments-prod` ❌
- Downtime: Minutes until someone notices ❌

**After Fix:**
- Channel dies → Auto-restart in 2 seconds ✅
- No manual intervention ✅
- Downtime: ~2 seconds (minimal!) ✅

### Verification

```bash
# Check consumer started with resilient mode
docker logs payments-prod | grep "resilient consumer"
# Expected: "starting resilient consumer (auto-restart enabled)..."

# Check consumer is running
docker logs payments-prod | grep "payment consumer started"
# Expected: "payment consumer started (resilient)"

# Simulate failure and verify recovery
docker exec rabbitmq-prod rabbitmqctl close_all_connections "Test"
sleep 3
docker logs payments-prod --tail=10
# Expected:
# "channel closed, will restart consumer"
# "starting consumer on fresh channel"
```

---

## 🏥 Kubernetes Health Check Endpoints

### Why Health Checks?

Kubernetes uses health checks for:
1. **Liveness Probes** - Restart unhealthy pods
2. **Readiness Probes** - Route traffic only to ready pods
3. **Graceful Deployments** - Zero-downtime rolling updates

### Implementation

All services expose `/health` endpoint returning:

```json
{"status":"healthy"}
```

### Service Health Endpoints

| Service | Port | Endpoint | Protocol |
|---------|------|----------|----------|
| **Gateway** | 8080 | `GET /health` | HTTP |
| **Orders** | 9001 | `GET /health` | HTTP (metrics server) |
| **Payments** | 8082 | `GET /health` | HTTP |
| **Stock** | 8083 | `GET /health` | HTTP |

### Testing Health Endpoints

```bash
# Gateway
curl http://localhost:8080/health
# Response: {"status":"healthy"}

# Orders
curl http://localhost:9001/health
# Response: {"status":"healthy"}

# Payments
curl http://localhost:8082/health
# Response: {"status":"healthy"}

# Stock
curl http://localhost:8083/health
# Response: {"status":"healthy"}
```

### Kubernetes Configuration

**Liveness Probe Example:**

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3
```

**Readiness Probe Example:**

```yaml
readinessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 5
  timeoutSeconds: 3
  successThreshold: 1
  failureThreshold: 3
```

**What happens:**
- Pod starts → Wait 10s → Check `/health`
- Healthy? → Pod gets traffic
- Unhealthy 3 times? → Pod removed from service (no traffic)
- Still unhealthy after 30s? → Pod restarted (liveness)

---

## 🔧 Production Deployment Checklist

### Before Deploying to Production

- [ ] **RabbitMQ:** All services use ResilientConnection
- [ ] **Health Checks:** All services expose `/health` endpoint
- [ ] **Secrets:** Use Kubernetes Secrets or SealedSecrets (never hardcode!)
- [ ] **Resource Limits:** Set CPU/Memory limits for all pods
- [ ] **Persistent Volumes:** Configure for databases (PostgreSQL, MongoDB, Redis)
- [ ] **Monitoring:** Setup Prometheus scraping `/metrics` endpoints
- [ ] **Observability:** OTEL Collector + Jaeger for distributed tracing
- [ ] **TLS:** Enable TLS for external traffic (Cloudflare Tunnel or cert-manager)
- [ ] **Horizontal Pod Autoscaling:** Configure HPA for high-traffic services

### Testing RabbitMQ Resilience

**Scenario 1: RabbitMQ Restart**

```bash
# Restart RabbitMQ
docker restart rabbitmq-prod

# Check logs - should auto-reconnect
docker logs orders-prod --tail 20
# Expected:
# ⚠️  RabbitMQ connection lost: ...
# 🔄 Reconnection attempt #1 (backoff: 1s)
# ✅ Reconnection successful after 1 attempts
```

**Scenario 2: Network Interruption**

```bash
# Simulate network failure
docker network disconnect <network> rabbitmq-prod

# Wait 10 seconds

# Reconnect
docker network connect <network> rabbitmq-prod

# Services should automatically reconnect with backoff
```

**Scenario 3: Channel Closure**

Channels auto-recreate on:
- Timeout
- Error during publish
- RabbitMQ policy enforcement

No manual intervention needed!

---

## 📊 Monitoring & Observability

### Prometheus Metrics

All services expose `/metrics` for Prometheus scraping:

- **Gateway:** `http://localhost:8080/metrics`
- **Orders:** `http://localhost:9001/metrics`
- **Payments:** `http://localhost:8082/metrics`
- **Stock:** `http://localhost:8083/metrics`

**Key Metrics to Monitor:**

```
# RabbitMQ
rabbitmq_connection_total
rabbitmq_reconnection_attempts
rabbitmq_channel_recreations

# HTTP Requests
http_requests_total
http_request_duration_seconds

# gRPC
grpc_server_handled_total
grpc_server_handling_seconds

# Business Metrics
orders_created_total
orders_paid_total
stock_reservations_total
```

### Distributed Tracing (Jaeger)

Access Jaeger UI:
```bash
# Docker Compose
http://localhost:16686

# Kubernetes (port forward)
kubectl port-forward -n observability svc/jaeger 16686:16686
```

**Trace Flow Example:**

```
HTTP /api/orders/create (Gateway)
  └─> gRPC CreateOrder (Orders Service)
      └─> RabbitMQ Publish order.created
          └─> Consumer (Payments Service)
              └─> Stripe API
              └─> RabbitMQ Publish order.paid
```

---

## 🏗️ Architecture Patterns Implemented

### 1. Circuit Breaker Pattern
**ResilientConnection** implements circuit breaker:
- Connection fails → Open circuit
- Exponential backoff → Half-open (testing)
- Success → Closed circuit (normal operation)

### 2. Dead Letter Queue (DLQ)
Failed messages after 3 retries → DLQ:
- `order.created.dlq`
- `order.paid.dlq`
- `order.preparing.dlq`
- `order.ready.dlq`

### 3. Cache-Aside Pattern (Stock Service)
```
GetItems:
  1. Check Redis cache
  2. Cache miss? → Query PostgreSQL
  3. Populate cache (TTL: 5 minutes)

DecrementQuantity:
  1. Update PostgreSQL
  2. Invalidate Redis cache
```

### 4. Saga Pattern (Order Flow)
```
1. Create Order (Orders Service)
2. Reserve Stock (Stock Service)
3. Create Payment Link (Payments Service)
4. Wait for Payment (Stripe Webhook)
5. Commit Stock (Stock Service)
6. Notify Kitchen (Kitchen Service)
```

---

## 🎯 Production Readiness Score

| Category | Status | Notes |
|----------|--------|-------|
| **Resilient Messaging** | ✅ Production Ready | ResilientConnection with channel monitoring |
| **Health Checks** | ✅ Production Ready | All services expose `/health` |
| **Observability** | ✅ Production Ready | Prometheus + Jaeger + OTEL |
| **Secrets Management** | ⚠️ Needs K8s Secrets | Currently: `.env` file (dev only) |
| **Resource Limits** | ⚠️ Needs Configuration | Set in K8s manifests |
| **Auto-Scaling** | ⚠️ Needs HPA | Configure HPA for Gateway/Orders |
| **TLS/HTTPS** | ✅ Production Ready | Cloudflare Tunnel configured |
| **Persistent Volumes** | ⚠️ Needs Configuration | Configure PVCs for databases |

---

## 📚 References

- [Google SRE Book - Handling Overload](https://sre.google/sre-book/handling-overload/)
- [RabbitMQ Production Checklist](https://www.rabbitmq.com/production-checklist.html)
- [Kubernetes Health Checks Best Practices](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
- [Exponential Backoff And Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)

---

**Last Updated:** November 17, 2025
**Status:** Production Ready (with K8s Secrets needed)
**Maintained by:** Timour + Claude
