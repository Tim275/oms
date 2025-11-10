package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/timour/order-microservices/common/config"
	"github.com/timour/order-microservices/common/logger"
	"github.com/timour/order-microservices/common/tracing"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// Load configuration from environment variables with defaults
	cfg := Config{
		ServiceName: config.GetEnv("SERVICE_NAME", "orders"),
		InstanceID:  config.GetEnv("INSTANCE_ID", "orders-1"),
		GRPCAddr:    config.GetEnv("GRPC_ADDR", "localhost:9000"),
		MetricsAddr: config.GetEnv("METRICS_ADDR", "localhost:9001"),
		ConsulAddr:  config.GetEnv("CONSUL_ADDR", "localhost:8500"),
		AMQPUser:    config.GetEnv("AMQP_USER", "guest"),
		AMQPPass:    config.GetEnv("AMQP_PASS", "guest"),
		AMQPHost:    config.GetEnv("AMQP_HOST", "localhost"),
		AMQPPort:    config.GetEnv("AMQP_PORT", "5672"),
		MongoURI:    config.GetEnv("MONGO_URI", "mongodb://localhost:27017"),
	}

	log := logger.NewLogger(cfg.ServiceName)
	log.Info("starting service",
		slog.String("instance_id", cfg.InstanceID),
		slog.String("grpc_addr", cfg.GRPCAddr),
	)

	// ⭐ Initialize OpenTelemetry Tracing
	shutdown, err := tracing.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Error("failed to initialize tracer", slog.Any("error", err))
		os.Exit(1)
	}
	defer shutdown()

	// ⭐ Connect to MongoDB
	mongoClient, err := connectToMongoDB(cfg.MongoURI)
	if err != nil {
		log.Error("failed to connect to mongodb", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(ctx); err != nil {
			log.Error("failed to disconnect from mongodb", slog.Any("error", err))
		}
	}()

	app, err := NewApp(cfg, mongoClient)
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

// connectToMongoDB establishes connection to MongoDB
func connectToMongoDB(uri string) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping mongodb: %w", err)
	}

	return client, nil
}
