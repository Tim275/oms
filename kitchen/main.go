package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/common/discovery"
	"github.com/timour/order-microservices/common/discovery/consul"
)

// Service Configuration
var (
	serviceName  = "kitchen"
	httpAddr     = "localhost:8083"
	consulAddr   = "localhost:8500"
	amqpUser     = "guest"
	amqpPass     = "guest"
	amqpHost     = "localhost"
	amqpPort     = "5672"
	jaegerAddr   = "localhost:4317"
)

func main() {
	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: false,
	}))

	logger.Info("starting service",
		slog.String("service", serviceName),
		slog.String("http_addr", httpAddr),
	)

	// Initialize Consul registry
	registry, err := consul.NewRegistry(consulAddr, serviceName)
	if err != nil {
		log.Fatalf("failed to initialize consul registry: %v", err)
	}

	ctx := context.Background()
	instanceID := discovery.GenerateInstanceID(serviceName)

	if err := registry.Register(ctx, instanceID, serviceName, httpAddr); err != nil {
		log.Fatalf("failed to register service: %v", err)
	}
	defer registry.Deregister(ctx, instanceID, serviceName)

	logger.Info("consul registry initialized", slog.String("service", serviceName))

	// Connect to RabbitMQ
	logger.Info("connecting to rabbitmq",
		slog.String("service", serviceName),
		slog.String("host", amqpHost),
		slog.String("port", amqpPort),
	)

	ch, close, err := broker.Connect(amqpUser, amqpPass, amqpHost, amqpPort)
	if err != nil {
		log.Fatalf("failed to connect to rabbitmq: %v", err)
	}
	defer close()

	logger.Info("rabbitmq connected successfully", slog.String("service", serviceName))

	// Initialize Gateway (gRPC client to Orders Service)
	gateway := NewGateway(registry, logger)
	logger.Info("orders gateway initialized", slog.String("service", serviceName))

	// Start Consumer (listens to order.paid events)
	consumer := NewConsumer(gateway, ch, logger)
	go consumer.Listen()

	logger.Info("consumer started, waiting for messages...", slog.String("service", serviceName))

	// Setup HTTP Server (REST API for chef)
	mux := http.NewServeMux()
	handler := NewHTTPHandler(gateway, logger)
	handler.RegisterRoutes(mux)

	// Start HTTP Server
	srv := &http.Server{
		Addr:    httpAddr,
		Handler: mux,
	}

	go func() {
		logger.Info("starting http server",
			slog.String("service", serviceName),
			slog.String("addr", httpAddr),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to start http server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...", slog.String("service", serviceName))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	logger.Info("server exited", slog.String("service", serviceName))
}
