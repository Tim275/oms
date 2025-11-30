package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/timour/order-microservices/common/broker"
	"github.com/timour/order-microservices/common/discovery"
	"github.com/timour/order-microservices/common/discovery/consul"
	"github.com/timour/order-microservices/common/logger"
	"github.com/timour/order-microservices/common/metrics"
	"go.mongodb.org/mongo-driver/mongo"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

type App struct {
	registry        discovery.Registry
	grpcServer      *grpc.Server
	metricsServer   *http.Server
	registration    *ServiceRegistration
	resilientConn   *broker.ResilientConnection
	mongoClient     *mongo.Client
	config          Config
	logger          *slog.Logger
	grpcMetrics     *metrics.GRPCMetrics
	businessMetrics *metrics.BusinessMetrics
}

type Config struct {
	ServiceName string
	InstanceID  string
	GRPCAddr    string
	MetricsAddr string
	ConsulAddr  string
	AMQPUser    string
	AMQPPass    string
	AMQPHost    string
	AMQPPort    string
	MongoURI    string
}

func NewApp(config Config, mongoClient *mongo.Client) (*App, error) {
	log := logger.NewLogger(config.ServiceName)

	// Warum createRegistry?
	// → Verbindet mit Consul für Service Discovery
	registry, err := createRegistry(config.ConsulAddr, log)
	if err != nil {
		return nil, err
	}

	// Warum RabbitMQ Connection hier?
	// → In NewApp (Startup): Verbindung wird EINMAL aufgebaut
	// → Channel wird in App gespeichert und an grpcHandler übergeben
	log.Info("connecting to rabbitmq with auto-reconnect",
		slog.String("host", config.AMQPHost),
		slog.String("port", config.AMQPPort),
	)
	resilientConn, err := broker.NewResilientConnection(config.AMQPUser, config.AMQPPass, config.AMQPHost, config.AMQPPort)
	if err != nil {
		log.Error("failed to connect to rabbitmq", slog.Any("error", err))
		return nil, err
	}
	log.Info("rabbitmq connected successfully with auto-reconnect enabled")

	// Warum ResilientConnection speichern?
	// → Wird an grpcHandler übergeben (zum Publizieren mit Auto-Reconnect)
	// → Wird in Shutdown() aufgerufen (Cleanup!)
	// → Automatischer Reconnect bei Connection Loss!
	//
	// ⭐ Initialize Prometheus Metrics
	grpcMetrics := metrics.NewGRPCMetrics(config.ServiceName)
	businessMetrics := metrics.NewBusinessMetrics(config.ServiceName)

	// ⭐ OpenTelemetry gRPC Server Middleware
	// Warum NewServerHandler?
	// → Automatisches Tracing für ALLE incoming gRPC Calls
	// → CreateOrder, UpdateOrder, GetOrder → Alle haben Traces!
	// → Trace Context wird von Client (Gateway/Payment) propagiert
	return &App{
		registry:        registry,
		grpcServer:      grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler())),
		resilientConn:   resilientConn,   // RabbitMQ Resilient Connection (Auto-Reconnect!)
		mongoClient:     mongoClient,     // MongoDB Client
		config:          config,
		logger:          log,
		grpcMetrics:     grpcMetrics,     // Prometheus gRPC Metrics
		businessMetrics: businessMetrics, // Prometheus Business Metrics
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	// 1. Register with Service Discovery
	registration, err := RegisterService(
		ctx,
		a.registry,
		a.config.InstanceID,
		a.config.ServiceName,
		a.config.GRPCAddr,
	)
	if err != nil {
		return err
	}
	a.registration = registration

	// 2. Setup Business Logic with MongoDB
	store := NewStore(a.mongoClient)
	svc := NewService(store)

	// Pass ResilientConnection to grpcHandler
	// → grpcHandler holt sich immer fresh channel bei jedem Publish!
	// → Automatisch reconnected wenn Connection lost
	NewGRPCHandler(a.grpcServer, svc, store, a.resilientConn, a.logger, a.registry)

	// 3. Start Prometheus Metrics HTTP Server
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())

	// Add /health endpoint for K8s liveness/readiness probes
	metricsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	a.metricsServer = &http.Server{
		Addr:    a.config.MetricsAddr,
		Handler: metricsMux,
	}
	go func() {
		a.logger.Info("starting metrics server", slog.String("addr", a.config.MetricsAddr))
		if err := a.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("metrics server error", slog.Any("error", err))
		}
	}()

	// 4. Start RabbitMQ Consumer for order.paid events
	// → EVENT-DRIVEN ARCHITECTURE!
	// → Payment Service publishes order.paid → Orders Consumer updates Order
	// ✅ NOW USING ResilientConsumer with Auto-Restart (Production Best Practice)
	// → Überwacht Channel closures
	// → Startet automatisch neu wenn Channel stirbt
	// → Holt frischen Channel von ResilientConnection
	consumer := NewResilientConsumer(store, a.logger, a.resilientConn)
	a.logger.Info("starting resilient consumer (auto-restart enabled)...")
	consumer.Start() // Non-blocking! Returns immediately

	// 5. Start gRPC Server
	lis, err := net.Listen("tcp", a.config.GRPCAddr)
	if err != nil {
		return err
	}

	a.logger.Info("starting grpc server", slog.String("addr", a.config.GRPCAddr))
	return a.grpcServer.Serve(lis)
}

func (a *App) Shutdown(ctx context.Context) error {
	a.logger.Info("shutting down gracefully")

	// Warum GracefulStop zuerst?
	// → Stoppt gRPC Server: Keine neuen Requests mehr
	// → Wartet bis laufende Requests fertig sind
	a.grpcServer.GracefulStop()

	// Shutdown metrics HTTP server
	if a.metricsServer != nil {
		if err := a.metricsServer.Shutdown(ctx); err != nil {
			a.logger.Error("error shutting down metrics server", slog.Any("error", err))
		}
	}

	// Warum resilientConn.Close() hier?
	// → Schließt Channel + Connection zu RabbitMQ gracefully
	// → Stops auto-reconnect monitor
	// → WICHTIG: Nach GracefulStop (keine Events mehr publishen!)
	if a.resilientConn != nil {
		if err := a.resilientConn.Close(); err != nil {
			a.logger.Error("error closing rabbitmq", slog.Any("error", err))
		}
	}

	// Warum Deregister am Ende?
	// → Entfernt Service aus Consul
	// → WICHTIG: Als letztes! (Erst Server stoppen, DANN aus Consul entfernen)
	if a.registration != nil {
		return a.registration.Deregister(ctx)
	}
	return nil
}

func createRegistry(addr string, log *slog.Logger) (discovery.Registry, error) {
	if addr == "" {
		log.Info("consul address not provided, service discovery disabled")
		return nil, nil
	}
	return consul.NewRegistry(addr, "orders")
}
