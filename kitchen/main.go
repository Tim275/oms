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

	common "github.com/timour/order-microservices/common"
	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/common/config"
	"github.com/timour/order-microservices/common/discovery"
	"github.com/timour/order-microservices/common/discovery/consul"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Service Configuration
var (
	serviceName  = config.GetEnv("SERVICE_NAME", "kitchen")
	httpAddr     = config.GetEnv("HTTP_ADDR", "localhost:8083")
	consulAddr   = config.GetEnv("CONSUL_ADDR", "")
	amqpUser     = config.GetEnv("AMQP_USER", "guest")
	amqpPass     = config.GetEnv("AMQP_PASS", "guest")
	amqpHost     = config.GetEnv("AMQP_HOST", "localhost")
	amqpPort     = config.GetEnv("AMQP_PORT", "5672")
	jaegerAddr   = config.GetEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
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

	// Initialize OpenTelemetry Tracer
	if err := common.SetGlobalTracer(context.TODO(), serviceName, jaegerAddr); err != nil {
		log.Fatalf("could not set global tracer: %v", err)
	}
	logger.Info("opentelemetry tracer initialized",
		slog.String("service", serviceName),
		slog.String("exporter", jaegerAddr),
	)

	ctx := context.Background()
	var registry discovery.Registry

	// Initialize Consul registry only if CONSUL_ADDR is provided
	if consulAddr != "" {
		reg, err := consul.NewRegistry(consulAddr, serviceName)
		if err != nil {
			log.Fatalf("failed to initialize consul registry: %v", err)
		}
		registry = reg

		instanceID := discovery.GenerateInstanceID(serviceName)

		if err := registry.Register(ctx, instanceID, serviceName, httpAddr); err != nil {
			log.Fatalf("failed to register service: %v", err)
		}
		defer registry.Deregister(ctx, instanceID, serviceName)

		logger.Info("consul registry initialized", slog.String("service", serviceName))
	} else {
		logger.Info("consul service discovery disabled", slog.String("service", serviceName))
	}

	// Connect to RabbitMQ with Auto-Reconnect (Production Best Practice)
	// Google SRE Pattern: Resilient connections with exponential backoff
	logger.Info("connecting to rabbitmq with auto-reconnect",
		slog.String("service", serviceName),
		slog.String("host", amqpHost),
		slog.String("port", amqpPort),
	)

	resilientConn, err := broker.NewResilientConnection(amqpUser, amqpPass, amqpHost, amqpPort)
	if err != nil {
		log.Fatalf("failed to connect to rabbitmq: %v", err)
	}
	defer resilientConn.Close()

	logger.Info("rabbitmq connected successfully with auto-reconnect enabled", slog.String("service", serviceName))

	// Initialize Gateway (gRPC client to Orders Service)
	gateway := NewGateway(registry, logger)
	logger.Info("orders gateway initialized", slog.String("service", serviceName))

	// ✅ Start ResilientConsumer with Auto-Restart (Production Best Practice)
	// → Überwacht Channel closures
	// → Startet automatisch neu wenn Channel stirbt
	// → Holt frischen Channel von ResilientConnection
	consumer := NewResilientConsumer(gateway, logger, resilientConn)
	logger.Info("starting resilient consumer (auto-restart enabled)...", slog.String("service", serviceName))
	consumer.Start() // Non-blocking! Returns immediately

	logger.Info("consumer started, waiting for messages...", slog.String("service", serviceName))

	// Setup HTTP Server (REST API for chef)
	mux := http.NewServeMux()
	handler := NewHTTPHandler(gateway, logger)
	handler.RegisterRoutes(mux)  // Already includes /metrics endpoint

	// Wrap with OpenTelemetry HTTP middleware for auto-tracing
	wrappedMux := otelhttp.NewHandler(mux, "kitchen-http-server")

	// Start HTTP Server
	srv := &http.Server{
		Addr:    httpAddr,
		Handler: wrappedMux,
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
