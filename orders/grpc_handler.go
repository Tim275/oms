package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/timour/order-microservices/common/api"
	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/common/discovery"
	"google.golang.org/grpc"
)

type grpcHandler struct {
	api.UnimplementedOrderServiceServer
	service  OrdersService
	store    OrdersStore
	channel  *amqp.Channel
	logger   *slog.Logger
	registry discovery.Registry
}

func NewGRPCHandler(grpcServer *grpc.Server, service OrdersService, store OrdersStore, channel *amqp.Channel, logger *slog.Logger, registry discovery.Registry) {
	handler := &grpcHandler{
		service:  service,
		store:    store,
		channel:  channel,
		logger:   logger,
		registry: registry,
	}
	api.RegisterOrderServiceServer(grpcServer, handler)
}

func (h *grpcHandler) CreateOrder(ctx context.Context, req *api.CreateOrderRequest) (*api.Order, error) {
	h.logger.Info("order received",
		slog.String("customer_id", req.CustomerId),
		slog.Int("items_count", len(req.Items)),
	)

	// ⭐ STEP 1: Stock Validation (NEU!)
	// Warum Stock Check ZUERST?
	// → Verhindert Orders für nicht verfügbare Items
	// → Payment läuft nur wenn Items in Stock sind
	// → Bessere User Experience: Sofortiges Feedback!
	conn, err := discovery.ServiceConnection(ctx, "stock", h.registry)
	if err != nil {
		h.logger.Error("failed to connect to stock service", slog.Any("error", err))
		return nil, fmt.Errorf("stock service unavailable: %w", err)
	}
	defer conn.Close()

	stockClient := api.NewStockServiceClient(conn)

	// Warum ItemsWithQuantity konvertieren?
	// → Stock Service erwartet []*api.ItemsWithQuantity
	// → Request hat das gleiche Format!
	stockCheckReq := &api.CheckIfItemIsInStockRequest{
		Items: req.Items,
	}

	h.logger.Info("checking stock availability",
		slog.Int("items_count", len(req.Items)),
	)

	// ⭐ gRPC Call zu Stock Service
	// → OpenTelemetry propagiert automatisch TraceID!
	// → discovery.ServiceConnection fügt otelgrpc Interceptor hinzu
	stockResp, err := stockClient.CheckIfItemIsInStock(ctx, stockCheckReq)
	if err != nil {
		h.logger.Error("stock check failed", slog.Any("error", err))
		return nil, fmt.Errorf("failed to check stock: %w", err)
	}

	// Warum InStock Check?
	// → Stock Service returned InStock=false wenn mindestens 1 Item nicht verfügbar ist
	// → Items Array enthält trotzdem die gefundenen Items (für Debugging)
	if !stockResp.InStock {
		h.logger.Warn("items not in stock",
			slog.Int("requested_items", len(req.Items)),
			slog.Int("available_items", len(stockResp.Items)),
		)
		return nil, fmt.Errorf("one or more items are not in stock")
	}

	h.logger.Info("stock check passed",
		slog.Int("items_count", len(stockResp.Items)),
	)

	// ⭐ Build Stock Item Lookup Map
	// Warum Map?
	// → Schnelles Lookup: O(1) statt O(n) pro Item
	// → Vermeidet HARDCODED "Product" Name!
	// → Stock Service hat die Source of Truth für Item Details
	stockItemMap := make(map[string]*api.Item)
	for _, item := range stockResp.Items {
		stockItemMap[item.ID] = item
	}

	// STEP 2: Aggregate request items and prepare for order creation
	// NOTE: We need to do this BEFORE reserving stock so we have the order ID
	err = h.service.CreateOrder(ctx)
	if err != nil {
		h.logger.Error("service create order failed", slog.Any("error", err))
		return nil, err
	}

	// Aggregate request items by ID FIRST
	itemQuantityMap := make(map[string]int32)
	for _, reqItem := range req.Items {
		itemQuantityMap[reqItem.ID] += reqItem.Quantity
	}

	// Map aggregated items to order items - Using ACTUAL Stock data!
	// Warum nicht mehr hardcoded?
	// → Stock Service returned die echten Item Details (Name, PriceID)
	// → Single Source of Truth: PostgreSQL in Stock Service
	items := make([]*api.Item, 0, len(itemQuantityMap))
	for itemId, quantity := range itemQuantityMap {
		stockItem := stockItemMap[itemId]  // ⭐ Get actual item from Stock service
		if stockItem == nil {
			h.logger.Error("item not found in stock response",
				slog.String("item_id", itemId),
			)
			return nil, fmt.Errorf("item %s not found in stock", itemId)
		}
		items = append(items, &api.Item{
			ID:       itemId,
			Name:     stockItem.Name,      // ✅ Real name: "Cheeseburger", "Pommes"
			Quantity: quantity,            // Aggregated quantity
			PriceID:  stockItem.PriceID,   // ✅ Real Stripe Price ID from database
		})
	}

	// Create order WITHOUT ID - MongoDB will generate unique _id
	orderToCreate := &api.Order{
		CustomerId: req.CustomerId,
		Status:     "pending",
		Items:      items,
	}

	// Store order and get MongoDB-generated _id
	objectID, err := h.store.Create(ctx, orderToCreate)
	if err != nil {
		h.logger.Error("failed to store order", slog.Any("error", err))
		return nil, err
	}

	// Use MongoDB's _id as Order ID (hex string)
	order := &api.Order{
		Id:         objectID.Hex(),  // ✅ Unique MongoDB ObjectID!
		CustomerId: req.CustomerId,
		Status:     "pending",
		Items:      items,
		CreatedAt:  objectID.Timestamp().Format("2006-01-02T15:04:05Z07:00"), // ISO 8601 timestamp from MongoDB ObjectID
	}

	// ⭐ STEP 3: Reserve Stock (NEW!)
	// Warum JETZT?
	// → Order existiert bereits in MongoDB mit status="pending"
	// → Falls Reservation fehlschlägt: Order bleibt "pending" (kein Payment Link)
	// → Falls Reservation erfolgreich: Stock ist reserviert für 15 Minuten!
	h.logger.Info("reserving stock for order",
		slog.String("order_id", order.Id),
		slog.Int("items_count", len(items)),
	)

	reserveReq := &api.ReserveStockRequest{
		OrderID: order.Id,
		Items:   items,
	}

	reserveResp, err := stockClient.ReserveStock(ctx, reserveReq)
	if err != nil {
		h.logger.Error("failed to reserve stock",
			slog.String("order_id", order.Id),
			slog.Any("error", err),
		)
		// Stock reservation failed → Order stays "pending" without payment link
		return nil, fmt.Errorf("failed to reserve stock: %w", err)
	}

	h.logger.Info("stock reserved successfully",
		slog.String("order_id", order.Id),
		slog.String("reservation_id", reserveResp.ReservationID),
	)

	// ⭐ STEP 4: Publish Event to RabbitMQ
	// Warum channel == nil Check?
	// → RabbitMQ ist OPTIONAL! Service funktioniert auch OHNE Events
	// → Bei Tests oder Entwicklung: Kein RabbitMQ → channel = nil
	if h.channel == nil {
		h.logger.Error("rabbitmq channel is nil, event not published")
		return order, nil  // Return order anyway! (Event Publishing ist nicht kritisch)
	}

	// Warum QueueDeclare?
	// → Erstellt Queue "order.created" falls sie NOCH NICHT existiert
	// → Idempotent: Mehrfaches Aufrufen = kein Problem!
	q, err := h.channel.QueueDeclare(
		broker.OrderCreatedEvent, // name: "order.created"
		true,  // durable: Queue überlebt RabbitMQ Restart!
		false, // auto-delete: Queue wird NICHT gelöscht wenn Consumer disconnected
		false, // exclusive: Andere Connections können auch zugreifen
		false, // no-wait: Warte auf Server Bestätigung
		amqp.Table{
			"x-dead-letter-exchange": broker.DLX, // DLX Integration! Failed messages → "dlx" exchange
		},
	)
	if err != nil {
		h.logger.Error("failed to declare queue",
			slog.String("queue", broker.OrderCreatedEvent),
			slog.Any("error", err),
		)
		return order, nil  // Event Publishing fehlgeschlagen, aber Order wurde gespeichert!
	}

	// Warum json.Marshal?
	// → Konvertiert Go struct (*api.Order) → JSON bytes
	// → RabbitMQ sendet nur []byte (keine Go structs!)
	// → Payment Service empfängt JSON und deserialisiert es zurück
	marshalledOrder, err := json.Marshal(order)
	if err != nil {
		h.logger.Error("failed to marshal order", slog.Any("error", err))
		return order, nil
	}

	// Warum PublishWithContext?
	// → Sendet Message an Queue "order.created"
	// → WithContext: Respektiert Timeouts/Cancellations!
	//
	// ⭐ OpenTelemetry Trace Propagation:
	// → broker.InjectTraceContext(ctx) extrahiert TraceID + SpanID aus context
	// → Injiziert in AMQP Headers (W3C Trace Context Standard!)
	// → Payment Service kann Trace fortsetzen!
	err = h.channel.PublishWithContext(
		ctx,
		"",      // exchange: "" = Default Exchange (Direct Routing)
		q.Name,  // routing key: Queue Name "order.created"
		false,   // mandatory: false = RabbitMQ wirft Message NICHT weg wenn Queue fehlt
		false,   // immediate: Deprecated, immer false
		amqp.Publishing{
			ContentType: "application/json",        // Warum? Payment Service weiß: Body ist JSON!
			Body:        marshalledOrder,           // Die eigentliche Order als JSON bytes
			Headers:     broker.InjectTraceContext(ctx), // ⭐ OpenTelemetry trace context!
		},
	)
	if err != nil {
		h.logger.Error("failed to publish event",
			slog.String("event", broker.OrderCreatedEvent),
			slog.String("order_id", order.Id),
			slog.Any("error", err),
		)
	} else {
		h.logger.Info("event published",
			slog.String("event", broker.OrderCreatedEvent),
			slog.String("order_id", order.Id),
			slog.String("customer_id", order.CustomerId),
		)
	}

	return order, nil
}

func (h *grpcHandler) UpdateOrder(ctx context.Context, req *api.Order) (*api.Order, error) {
	h.logger.Info("updating order",
		slog.String("order_id", req.Id),
		slog.String("status", req.Status),
		slog.String("payment_link", req.PaymentLink),
	)

	// Get previous order state to detect status changes
	previousOrder, err := h.store.Get(ctx, req.Id)
	if err != nil {
		h.logger.Error("failed to get previous order", slog.Any("error", err))
		return nil, fmt.Errorf("order not found: %w", err)
	}

	// Update the order
	updatedOrder, err := h.service.UpdateOrder(ctx, req)
	if err != nil {
		h.logger.Error("failed to update order", slog.Any("error", err))
		return nil, err
	}

	h.logger.Info("order updated successfully",
		slog.String("order_id", updatedOrder.Id),
		slog.String("status", updatedOrder.Status),
		slog.String("previous_status", previousOrder.Status),
	)

	// Publish event if status changed
	if previousOrder.Status != updatedOrder.Status && h.channel != nil {
		var eventName string
		switch updatedOrder.Status {
		case "paid":
			eventName = broker.OrderPaidEvent
		case "preparing":
			eventName = broker.OrderPreparingEvent
		case "ready":
			eventName = broker.OrderReadyEvent
		default:
			// No event for other status changes (e.g., payment_link updates)
			h.logger.Info("no event to publish for status",
				slog.String("status", updatedOrder.Status),
			)
			return updatedOrder, nil
		}

		// Declare queue for this event
		q, err := h.channel.QueueDeclare(
			eventName, // name: "order.paid", "order.preparing", or "order.ready"
			true,      // durable
			false,     // auto-delete
			false,     // exclusive
			false,     // no-wait
			nil,       // arguments
		)
		if err != nil {
			h.logger.Error("failed to declare queue",
				slog.String("queue", eventName),
				slog.Any("error", err),
			)
			return updatedOrder, nil // Return order anyway (event publishing is non-critical)
		}

		// Marshal order to JSON
		marshalledOrder, err := json.Marshal(updatedOrder)
		if err != nil {
			h.logger.Error("failed to marshal order", slog.Any("error", err))
			return updatedOrder, nil
		}

		// Publish event with trace context
		err = h.channel.PublishWithContext(
			ctx,
			"",     // exchange: default
			q.Name, // routing key: queue name
			false,  // mandatory
			false,  // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        marshalledOrder,
				Headers:     broker.InjectTraceContext(ctx),
			},
		)
		if err != nil {
			h.logger.Error("failed to publish event",
				slog.String("event", eventName),
				slog.String("order_id", updatedOrder.Id),
				slog.Any("error", err),
			)
		} else {
			h.logger.Info("event published",
				slog.String("event", eventName),
				slog.String("order_id", updatedOrder.Id),
				slog.String("status", updatedOrder.Status),
			)
		}
	}

	return updatedOrder, nil
}

func (h *grpcHandler) GetOrder(ctx context.Context, req *api.GetOrderRequest) (*api.Order, error) {
	h.logger.Info("getting order",
		slog.String("order_id", req.OrderId),
		slog.String("customer_id", req.CustomerId),
	)

	order, err := h.service.GetOrder(ctx, req.OrderId)
	if err != nil {
		h.logger.Error("failed to get order", slog.Any("error", err))
		return nil, err
	}

	return order, nil
}

func (h *grpcHandler) GetOrdersByStatus(ctx context.Context, req *api.GetOrdersByStatusRequest) (*api.GetOrdersByStatusResponse, error) {
	h.logger.Info("getting orders by status",
		slog.String("status", req.Status),
	)

	orders, err := h.store.GetByStatus(ctx, req.Status)
	if err != nil {
		h.logger.Error("failed to get orders by status",
			slog.String("status", req.Status),
			slog.Any("error", err),
		)
		return nil, err
	}

	h.logger.Info("orders retrieved successfully",
		slog.String("status", req.Status),
		slog.Int("count", len(orders)),
	)

	return &api.GetOrdersByStatusResponse{Orders: orders}, nil
}
