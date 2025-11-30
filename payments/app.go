package main

import (
	"context"
	"log/slog"

	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/common/logger"
	"github.com/timour/order-microservices/discovery"
	"github.com/timour/order-microservices/discovery/consul"
	"github.com/timour/order-microservices/payments/gateway"
	"github.com/timour/order-microservices/payments/processor"
)

type App struct {
	resilientConn *broker.ResilientConnection
	registry      discovery.Registry
	config        Config
	logger        *slog.Logger
	ordersGateway gateway.OrdersGateway
}

type Config struct {
	ServiceName string
	InstanceID  string
	ConsulAddr  string
	AMQPUser    string
	AMQPPass    string
	AMQPHost    string
	AMQPPort    string
	StripeKey   string
	HTTPAddr    string
	OrdersAddr  string
}

func NewApp(config Config) (*App, error) {
	log := logger.NewLogger(config.ServiceName)

	// Connect to Consul
	var registry discovery.Registry
	var err error
	if config.ConsulAddr != "" {
		registry, err = consul.NewRegistry(config.ConsulAddr)
		if err != nil {
			log.Error("failed to connect to consul", slog.Any("error", err))
			return nil, err
		}
		log.Info("consul registry initialized")
	}

	// Connect to RabbitMQ with Auto-Reconnect (Production Best Practice)
	log.Info("connecting to rabbitmq with auto-reconnect",
		slog.String("host", config.AMQPHost),
		slog.String("port", config.AMQPPort),
	)

	resilientConn, err := broker.NewResilientConnection(
		config.AMQPUser,
		config.AMQPPass,
		config.AMQPHost,
		config.AMQPPort,
	)
	if err != nil {
		log.Error("failed to connect to rabbitmq", slog.Any("error", err))
		return nil, err
	}

	log.Info("rabbitmq connected successfully with auto-reconnect enabled")

	return &App{
		resilientConn: resilientConn,
		registry:      registry,
		config:        config,
		logger:        log,
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	// 1. Initialize Stripe Processor
	stripeProcessor := processor.NewStripeProcessor(a.config.StripeKey)
	a.logger.Info("stripe processor initialized")

	// 2. OrdersGateway is now initialized in main.go BEFORE app.Start() to avoid race condition with HTTP handler

	// 3. Setup Business Logic
	// → Service nutzt Gateway für synchrone Calls
	// → Webhook handler wird später Events publishen!
	svc := NewService(stripeProcessor, a.ordersGateway, a.logger)

	// 4. Start RabbitMQ Consumer with ResilientConnection
	// ✅ NEW: Use ResilientConsumer for auto-restart on channel closure
	// → Überwacht Channel closures
	// → Startet automatisch neu wenn Channel stirbt
	// → Holt frischen Channel von ResilientConnection
	consumer := NewResilientConsumer(svc, a.logger, a.resilientConn)

	a.logger.Info("starting resilient consumer (auto-restart enabled)...")
	consumer.Start() // Non-blocking! Returns immediately

	// Block forever (consumer runs in background)
	<-ctx.Done()

	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	a.logger.Info("shutting down gracefully")

	// Close ResilientConnection (stops auto-reconnect monitor)
	if a.resilientConn != nil {
		if err := a.resilientConn.Close(); err != nil {
			a.logger.Error("error closing rabbitmq", slog.Any("error", err))
		}
	}

	return nil
}
