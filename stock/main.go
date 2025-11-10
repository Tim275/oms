package main

import (
	"context"
	"fmt"
	"net"
	"time"

	_ "github.com/joho/godotenv/autoload"
	common "github.com/timour/order-microservices/common"
	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/common/config"
	"github.com/timour/order-microservices/common/discovery"
	"github.com/timour/order-microservices/common/discovery/consul"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var (
	serviceName = "stock"
	grpcAddr    = config.GetEnv("GRPC_ADDR", "localhost:2002")
	consulAddr  = config.GetEnv("CONSUL_ADDR", "localhost:8500")
	amqpUser    = config.GetEnv("RABBITMQ_USER", "guest")
	amqpPass    = config.GetEnv("RABBITMQ_PASS", "guest")
	amqpHost    = config.GetEnv("RABBITMQ_HOST", "localhost")
	amqpPort    = config.GetEnv("RABBITMQ_PORT", "5672")
	jaegerAddr  = config.GetEnv("JAEGER_ADDR", "localhost:4318")
	// PostgreSQL connection details
	postgresHost = config.GetEnv("POSTGRES_HOST", "localhost")
	postgresPort = config.GetEnv("POSTGRES_PORT", "5432")
	postgresUser = config.GetEnv("POSTGRES_USER", "stock")
	postgresPass = config.GetEnv("POSTGRES_PASSWORD", "stock123")
	postgresDB   = config.GetEnv("POSTGRES_DB", "stock")
	// Redis connection details
	redisAddr = config.GetEnv("REDIS_ADDR", "localhost:6379")
	redisTTL  = 5 * time.Minute // Menu items cache TTL
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	zap.ReplaceGlobals(logger)

	if err := common.SetGlobalTracer(context.TODO(), serviceName, jaegerAddr); err != nil {
		logger.Fatal("could set global tracer", zap.Error(err))
	}

	registry, err := consul.NewRegistry(consulAddr, serviceName)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	instanceID := discovery.GenerateInstanceID(serviceName)
	if err := registry.Register(ctx, instanceID, serviceName, grpcAddr); err != nil {
		panic(err)
	}

	go func() {
		for {
			if err := registry.HealthCheck(instanceID, serviceName); err != nil {
				logger.Error("Failed to health check", zap.Error(err))
			}
			time.Sleep(time.Second * 1)
		}
	}()

	defer registry.Deregister(ctx, instanceID, serviceName)

	// ⭐ PostgreSQL Connection
	// Connection String: postgres://user:pass@host:port/dbname?sslmode=disable
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		postgresUser, postgresPass, postgresHost, postgresPort, postgresDB)

	store, err := NewPostgresStore(connStr)
	if err != nil {
		logger.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer store.Close()

	logger.Info("Connected to PostgreSQL", zap.String("database", postgresDB))

	// ⭐ Redis Cache Connection
	// TTL: 5 minutes → Menu items ändern sich selten
	// Cache-Aside Pattern: GetItems prüft erst Redis, dann PostgreSQL
	cache, err := NewItemCache(redisAddr, redisTTL)
	if err != nil {
		logger.Fatal("failed to connect to redis", zap.Error(err))
	}
	defer cache.Close()

	logger.Info("Connected to Redis", zap.String("addr", redisAddr), zap.Duration("ttl", redisTTL))

	// ⭐ Wrap PostgreSQL Store with Cache-Aside Pattern
	// CachedStore implements StockStore interface
	// GetItems: Check Redis → PostgreSQL on miss → Populate cache
	// DecrementQuantity: Update PostgreSQL → Invalidate cache
	cachedStore := NewCachedStore(store, cache)

	ch, close, err := broker.Connect(amqpUser, amqpPass, amqpHost, amqpPort)
	if err != nil {
		logger.Fatal("failed to connect to broker", zap.Error(err))
	}
	defer func() {
		close()
		ch.Close()
	}()

	// ⭐ OpenTelemetry gRPC Server Middleware
	// Warum NewServerHandler?
	// → Automatisches Tracing für ALLE incoming gRPC Calls
	// → CheckIfItemIsInStock, GetItems → Alle haben Traces!
	// → Trace Context wird von Client (Orders Service) propagiert
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))

	l, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}
	defer l.Close()

	svc := NewService(cachedStore)
	svcWithTelemetry := NewTelemetryMiddleware(svc)

	NewGRPCHandler(grpcServer, ch, svcWithTelemetry)

	consumer := NewConsumer(cachedStore)
	go consumer.Listen(ch)

	// ⭐ Background Job: Cleanup expired reservations every 1 minute
	// Prevents "stuck" reservations from blocking stock
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			count, err := store.CleanupExpiredReservations(ctx)
			if err != nil {
				logger.Error("Failed to cleanup expired reservations", zap.Error(err))
			} else if count > 0 {
				logger.Info("Cleaned up expired reservations", zap.Int("count", count))
			}
		}
	}()

	logger.Info("Starting gRPC server", zap.String("port", grpcAddr))

	if err := grpcServer.Serve(l); err != nil {
		logger.Fatal("failed to serve", zap.Error(err))
	}
}
