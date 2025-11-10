package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/joho/godotenv/autoload"
	"github.com/timour/order-microservices/common/config"
	"github.com/timour/order-microservices/common/logger"
	"github.com/timour/order-microservices/common/tracing"
	"github.com/timour/order-microservices/payments/gateway"
)

var (
	endpointStripeSecret = config.GetEnv("STRIPE_ENDPOINT_SECRET", "whsec_...")
)

func main() {
	// Load configuration - PAYMENT SERVICE
	cfg := Config{
		ServiceName: config.GetEnv("SERVICE_NAME", "payment"),
		InstanceID:  config.GetEnv("INSTANCE_ID", "payment-1"),
		ConsulAddr:  config.GetEnv("CONSUL_ADDR", "localhost:8500"),
		AMQPUser:    config.GetEnv("AMQP_USER", "guest"),
		AMQPPass:    config.GetEnv("AMQP_PASS", "guest"),
		AMQPHost:    config.GetEnv("AMQP_HOST", "localhost"),
		AMQPPort:    config.GetEnv("AMQP_PORT", "5672"),
		StripeKey:   config.GetEnv("STRIPE_SECRET_KEY", ""),
		HTTPAddr:    config.GetEnv("HTTP_ADDR", "localhost:8082"),
		OrdersAddr:  config.GetEnv("ORDERS_GRPC_ADDR", "localhost:9000"),
	}

	log := logger.NewLogger(cfg.ServiceName)
	log.Info("starting service",
		slog.String("instance_id", cfg.InstanceID),
	)

	// ⭐ Initialize OpenTelemetry Tracing
	shutdown, err := tracing.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Error("failed to initialize tracer", slog.Any("error", err))
		os.Exit(1)
	}
	defer shutdown()

	app, err := NewApp(cfg)
	if err != nil {
		log.Error("failed to create app", slog.Any("error", err))
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("received shutdown signal")
		if err := app.Shutdown(ctx); err != nil {
			log.Error("error during shutdown", slog.Any("error", err))
		}
		cancel()
	}()

	// Initialize OrdersGateway BEFORE creating HTTP handler (CRITICAL for webhook handler!)
	app.ordersGateway = gateway.NewOrdersGateway(cfg.OrdersAddr)
	log.Info("orders gateway initialized", slog.String("orders_addr", cfg.OrdersAddr))

	// Start RabbitMQ Consumer in background
	go func() {
		if err := app.Start(ctx); err != nil {
			log.Error("failed to start app", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Start HTTP Server for Stripe Webhooks in background
	mux := http.NewServeMux()
	httpServer := NewPaymentHTTPHandler(app.channel, app.ordersGateway, cfg.OrdersAddr)
	httpServer.registerRoutes(mux)

	go func() {
		log.Info("starting http server", slog.String("addr", cfg.HTTPAddr))
		if err := http.ListenAndServe(cfg.HTTPAddr, mux); err != nil {
			log.Error("failed to start http server", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Block forever
	<-ctx.Done()
	log.Info("shutting down")
}
