package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/timour/order-microservices/common/config"
	"github.com/timour/order-microservices/common/logger"
	"github.com/timour/order-microservices/common/tracing"
)

func main() {
	cfg := Config{
		ServiceName: config.GetEnv("SERVICE_NAME", "gateway"),
		InstanceID:  config.GetEnv("INSTANCE_ID", "gateway-1"),
		HTTPAddr:    config.GetEnv("HTTP_ADDR", "localhost:8081"),
		ConsulAddr:  config.GetEnv("CONSUL_ADDR", "localhost:8500"),
	}

	log := logger.NewLogger(cfg.ServiceName)
	log.Info("starting service",
		slog.String("instance_id", cfg.InstanceID),
		slog.String("http_addr", cfg.HTTPAddr),
	)

	// ⭐ Initialize OpenTelemetry Tracing
	// Warum hier?
	// → Vor app.Start(): Traces verfügbar wenn HTTP Server startet
	// → Service Name: "gateway" (für Jaeger UI)
	// → Shutdown: defer cleanup() flusht pending spans
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

	if err := app.Start(ctx); err != nil {
		log.Error("failed to start app", slog.Any("error", err))
		os.Exit(1)
	}
}
