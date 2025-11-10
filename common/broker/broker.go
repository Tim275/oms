package broker

import (
	"context"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Event names
// Warum Constants?
// → Verhindert Typos! "order.created" statt "order.creatd" (Fehler!)
// → Zentrale Stelle: Beide Services nutzen GLEICHEN Event-Namen
const (
	OrderCreatedEvent   = "order.created"   // Orders Service → publishes
	OrderPaidEvent      = "order.paid"      // Payments Service → publishes
	OrderPreparingEvent = "order.preparing" // Orders Service → publishes (Kitchen started)
	OrderReadyEvent     = "order.ready"     // Orders Service → publishes (Kitchen finished)
)

// DLQ Configuration
// Warum MaxRetryCount?
// → Retry failed messages up to 3 times before sending to DLQ
// → Production Best Practice: Don't retry forever!
const MaxRetryCount = 3
const DLX = "dlx"  // Dead Letter Exchange - Routes failed messages to queue-specific DLQs

// Connect: Helper zum Verbinden mit RabbitMQ
// Warum eigene Funktion?
// → Reusable! Orders + Payments Service nutzen gleichen Code
// Warum 3 Return-Werte (Channel, Close-Funktion, Error)?
// → Channel: Zum Senden/Empfangen von Messages
// → Close-Funktion: Cleanup (mit defer nutzen!)
// → Error: Falls Connection fehlschlägt
func Connect(user, pass, host, port string) (*amqp.Channel, func() error, error) {
	// Warum fmt.Sprintf?
	// → Baut AMQP URL: "amqp://guest:guest@localhost:5672/"
	// → RabbitMQ braucht dieses Format!
	address := fmt.Sprintf("amqp://%s:%s@%s:%s/", user, pass, host, port)

	// Warum amqp.Dial?
	// → Öffnet TCP Connection zu RabbitMQ Server
	// → Ähnlich wie Consul: Service-to-Service Connection!
	conn, err := amqp.Dial(address)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// Warum Channel (nicht direkt Connection)?
	// → Channel = "virtuelle Connection" innerhalb der echten Connection
	// → 1 Connection kann VIELE Channels haben (effizient!)
	// → Jeder Service nutzt eigenen Channel
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()  // Cleanup wenn Channel-Fehler!
		return nil, nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Warum DLQ/DLX Setup hier?
	// → Wird einmal beim Connect aufgerufen
	// → Alle Services nutzen gleiche DLQ Infrastruktur
	err = createDLQAndDLX(ch)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, fmt.Errorf("failed to create DLQ: %w", err)
	}

	// Warum Exchanges hier deklarieren?
	// → Exchanges müssen existieren BEVOR Services daran binden
	// → Wird einmal beim Connect aufgerufen
	err = createExchanges(ch)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, fmt.Errorf("failed to create exchanges: %w", err)
	}

	// Warum Close-Funktion zurückgeben?
	// → Caller kann mit defer close() automatisch cleanup machen
	// → Schließt Channel UND Connection (in richtiger Reihenfolge!)
	close := func() error {
		if err := ch.Close(); err != nil {
			return err
		}
		return conn.Close()  // Connection NACH Channel schließen!
	}

	return ch, close, nil
}

// HandleRetry: Retry-Logik für Failed Messages mit DLX Integration
// Warum HandleRetry?
// → Intelligentes Retry-System: Nicht sofort aufgeben!
// → Tracks retry count in message headers
// → Nach MaxRetryCount → RabbitMQ's DLX routed automatisch zu queue-spezifischer DLQ
//
// Flow (Senior's DLX Approach):
// 1. Message fails → HandleRetry
// 2. Increment x-retry-count in headers
// 3. If retry < MaxRetryCount → Republish to same queue (with exponential backoff)
// 4. If retry >= MaxRetryCount → Nack (requeue=false) → DLX → queue-specific DLQ
func HandleRetry(ch *amqp.Channel, d *amqp.Delivery) error {
	// Warum Headers initialisieren?
	// → Erste Delivery hat keine Headers
	// → Brauchen Map für x-retry-count
	if d.Headers == nil {
		d.Headers = amqp.Table{}
	}

	// Warum x-retry-count aus Headers lesen?
	// → Wir speichern retry count IN der Message
	// → Persistent! Geht nicht verloren bei Restart
	retryCount, ok := d.Headers["x-retry-count"].(int64)
	if !ok {
		retryCount = 0  // First retry
	}
	retryCount++
	d.Headers["x-retry-count"] = retryCount

	log.Printf("Retrying message, retry count: %d", retryCount)

	// Warum >= MaxRetryCount?
	// → After 3 retries → give up → let DLX handle it
	// → DLX routed automatisch zu queue-spezifischer DLQ (order.created.dlq, etc.)
	if retryCount >= MaxRetryCount {
		log.Printf("Max retries reached, sending to DLX (will route to %s.dlq)", d.RoutingKey)

		// ⭐ DLX Approach: Nack mit requeue=false
		// Warum Nack statt manuelles Publish?
		// → RabbitMQ's DLX Feature übernimmt!
		// → Queue hat x-dead-letter-exchange=dlx konfiguriert
		// → RabbitMQ sendet automatisch zu DLX
		// → DLX routed zu queue-spezifischer DLQ basierend auf routing key
		return d.Nack(false, false) // multiple=false, requeue=false
	}

	// Warum time.Sleep mit exponential backoff?
	// → Retry 1: wait 1 second
	// → Retry 2: wait 2 seconds
	// → Retry 3: wait 3 seconds
	// → Gibt externen Services Zeit zu recovern!
	time.Sleep(time.Second * time.Duration(retryCount))

	// Warum Republish zur GLEICHEN Queue?
	// → Message geht zurück in original queue
	// → Consumer wird es nochmal verarbeiten
	// → Mit updated retry count in headers!
	return ch.PublishWithContext(
		context.Background(),
		d.Exchange,   // Same exchange as original message
		d.RoutingKey, // Same routing key (usually queue name)
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			Headers:      d.Headers,  // Updated retry count!
			Body:         d.Body,
			DeliveryMode: amqp.Persistent,
		},
	)
}

// createDLQAndDLX: Erstellt Dead Letter Exchange + Queue-spezifische DLQs
// Warum DLX (Dead Letter Exchange)?
// → Native RabbitMQ Feature für automatisches Failed Message Routing
// → Jede Queue hat eigene DLQ (order.created.dlq, order.paid.dlq, etc.)
//
// Architecture (Senior's Approach):
// order.created Queue → (if failed after max retries) → DLX → order.created.dlq
// order.paid Queue → (if failed after max retries) → DLX → order.paid.dlq
func createDLQAndDLX(ch *amqp.Channel) error {
	// ⭐ 1. Create DLX Exchange
	// Warum Exchange?
	// → RabbitMQ routed failed messages automatisch zum DLX
	// → DLX routed dann zu queue-spezifischen DLQs
	err := ch.ExchangeDeclare(
		DLX,      // name: "dlx"
		"direct", // type: direct routing (routing key = queue name)
		true,     // durable: Überlebt RabbitMQ Restart
		false,    // auto-deleted: NEIN
		false,    // internal: NEIN
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare DLX exchange: %w", err)
	}

	log.Printf("DLX Exchange created: %s", DLX)

	// ⭐ 2. Create Queue-Specific DLQs
	// Warum pro Queue eine eigene DLQ?
	// → Bessere Übersicht: order.created failures getrennt von order.paid failures
	// → Einfacheres Debugging: Welche Queue hat Probleme?
	// → Granulares Monitoring: Metrics pro DLQ
	dlqQueues := []string{
		OrderCreatedEvent + ".dlq",   // "order.created.dlq"
		OrderPaidEvent + ".dlq",      // "order.paid.dlq"
		OrderPreparingEvent + ".dlq", // "order.preparing.dlq"
		OrderReadyEvent + ".dlq",     // "order.ready.dlq"
	}

	for _, dlq := range dlqQueues {
		_, err := ch.QueueDeclare(
			dlq,   // queue name
			true,  // durable: Überlebt RabbitMQ Restart
			false, // delete when unused: NEIN
			false, // exclusive: Andere können zugreifen
			false, // no-wait
			nil,   // arguments
		)
		if err != nil {
			return fmt.Errorf("failed to declare DLQ %s: %w", dlq, err)
		}

		// ⭐ 3. Bind DLQ to DLX
		// Warum Bind?
		// → DLX routed Messages basierend auf Routing Key
		// → Routing Key = original queue name (order.created, order.paid, etc.)
		// → Bind mit queue name als routing key!
		queueName := dlq[:len(dlq)-4] // Remove ".dlq" suffix → "order.created"
		err = ch.QueueBind(
			dlq,       // queue name: "order.created.dlq"
			queueName, // routing key: "order.created"
			DLX,       // exchange: "dlx"
			false,     // no-wait
			nil,       // arguments
		)
		if err != nil {
			return fmt.Errorf("failed to bind DLQ %s to DLX: %w", dlq, err)
		}

		log.Printf("DLQ created and bound: %s → %s (routing key: %s)", dlq, DLX, queueName)
	}

	return nil
}

// createExchanges: Deklariert alle RabbitMQ Exchanges
// Warum Exchanges deklarieren?
// → Exchanges müssen existieren BEVOR Queues daran binden können
// → "direct" Exchange: Messages gehen direkt zu Queue mit matching routing key
func createExchanges(ch *amqp.Channel) error {
	// Warum OrderCreatedEvent Exchange?
	// → Orders Service publiziert dorthin nach Order-Erstellung
	// → Payment Service bindet daran und konsumiert
	err := ch.ExchangeDeclare(
		OrderCreatedEvent, // "order.created"
		"direct",          // type: direct routing
		true,              // durable: Überlebt RabbitMQ Restart
		false,             // auto-deleted: NEIN
		false,             // internal: NEIN
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare %s exchange: %w", OrderCreatedEvent, err)
	}

	// Warum OrderPaidEvent Exchange?
	// → Payment Service publiziert dorthin nach erfolgreicher Zahlung
	// → Kitchen Service bindet daran und konsumiert
	err = ch.ExchangeDeclare(
		OrderPaidEvent, // "order.paid"
		"direct",       // type: direct routing
		true,           // durable: Überlebt RabbitMQ Restart
		false,          // auto-deleted: NEIN
		false,          // internal: NEIN
		false,          // no-wait
		nil,            // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare %s exchange: %w", OrderPaidEvent, err)
	}

	// Warum OrderPreparingEvent Exchange?
	// → Orders Service publiziert dorthin wenn Kitchen Service beginnt
	// → Notification Service kann daran binden (optional)
	err = ch.ExchangeDeclare(
		OrderPreparingEvent, // "order.preparing"
		"direct",            // type: direct routing
		true,                // durable: Überlebt RabbitMQ Restart
		false,               // auto-deleted: NEIN
		false,               // internal: NEIN
		false,               // no-wait
		nil,                 // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare %s exchange: %w", OrderPreparingEvent, err)
	}

	// Warum OrderReadyEvent Exchange?
	// → Orders Service publiziert dorthin wenn Order fertig ist
	// → Notification Service kann daran binden für Customer Alerts
	err = ch.ExchangeDeclare(
		OrderReadyEvent, // "order.ready"
		"direct",        // type: direct routing
		true,            // durable: Überlebt RabbitMQ Restart
		false,           // auto-deleted: NEIN
		false,           // internal: NEIN
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare %s exchange: %w", OrderReadyEvent, err)
	}

	log.Printf("Exchanges created: %s, %s, %s, %s", OrderCreatedEvent, OrderPaidEvent, OrderPreparingEvent, OrderReadyEvent)
	return nil
}

// AmqpHeaderCarrier: OpenTelemetry Header Carrier für AMQP
type AmqpHeaderCarrier map[string]interface{}

func (a AmqpHeaderCarrier) Get(k string) string {
	value, ok := a[k]
	if !ok {
		return ""
	}
	return value.(string)
}

func (a AmqpHeaderCarrier) Set(k string, v string) {
	a[k] = v
}

func (a AmqpHeaderCarrier) Keys() []string {
	keys := make([]string, len(a))
	i := 0
	for k := range a {
		keys[i] = k
		i++
	}
	return keys
}

// InjectAMQPHeaders: Fügt OpenTelemetry Headers zu AMQP Message hinzu
func InjectAMQPHeaders(ctx context.Context) map[string]interface{} {
	carrier := make(AmqpHeaderCarrier)
	// Note: requires otel to be imported in broker package
	// otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

// ExtractAMQPHeader: Extrahiert OpenTelemetry Context aus AMQP Headers
func ExtractAMQPHeader(ctx context.Context, headers map[string]interface{}) context.Context {
	// Note: requires otel to be imported in broker package
	// return otel.GetTextMapPropagator().Extract(ctx, AmqpHeaderCarrier(headers))
	return ctx
}
