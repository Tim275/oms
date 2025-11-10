package main

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/timour/order-microservices/common/logger"
	"github.com/timour/order-microservices/common/metrics"
	"github.com/timour/order-microservices/discovery"
	"github.com/timour/order-microservices/discovery/consul"
)

type App struct {
	registry     discovery.Registry
	httpServer   *http.Server
	registration *ServiceRegistration
	config       Config
	logger       *slog.Logger
	metrics      *metrics.HTTPMetrics
}

type Config struct {
	ServiceName string
	InstanceID  string
	HTTPAddr    string
	ConsulAddr  string
}

func NewApp(config Config) (*App, error) {
	log := logger.NewLogger(config.ServiceName)

	registry, err := createRegistry(config.ConsulAddr, log)
	if err != nil {
		return nil, err
	}

	return &App{
		registry: registry,
		config:   config,
		logger:   log,
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	// 1. Load environment
	if err := godotenv.Load(); err != nil {
		a.logger.Info("no .env file found, using defaults")
	}

	// 2. Register with Service Discovery
	if a.registry != nil {
		registration, err := RegisterService(
			ctx,
			a.registry,
			a.config.InstanceID,
			a.config.ServiceName,
			a.config.HTTPAddr,
		)
		if err != nil {
			return err
		}
		a.registration = registration
	}

	// 3. Initialize Prometheus Metrics
	a.metrics = metrics.NewHTTPMetrics(a.config.ServiceName)

	// 4. Setup HTTP Server
	mux := http.NewServeMux()
	handler := NewHandler(a.registry, a.logger)
	handler.registerRoute(mux)

	// Add /metrics endpoint for Prometheus scraping
	mux.Handle("GET /metrics", promhttp.Handler())

	// Wrap mux with CORS + metrics middleware
	metricsHandler := a.metricsMiddleware(mux)
	corsHandler := a.corsMiddleware(metricsHandler)

	a.httpServer = &http.Server{
		Addr:    a.config.HTTPAddr,
		Handler: corsHandler,
	}

	a.logger.Info("starting http server", slog.String("addr", a.config.HTTPAddr))
	return a.httpServer.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	a.logger.Info("shutting down gracefully")

	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(ctx); err != nil {
			a.logger.Error("http server shutdown error", slog.Any("error", err))
		}
	}

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
	return consul.NewRegistry(addr)
}

// metricsMiddleware wraps HTTP handlers to record Prometheus metrics
func (a *App) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't record metrics for /metrics endpoint itself
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		// Create response recorder to capture status code
		recorder := &responseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Call next handler
		next.ServeHTTP(recorder, r)

		// Record metrics
		duration := time.Since(start)
		status := strconv.Itoa(recorder.statusCode)
		a.metrics.RecordHTTPRequest(r.Method, r.URL.Path, status, duration)
	})
}

// responseRecorder wraps http.ResponseWriter to capture status code
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

// corsMiddleware adds CORS headers for frontend communication
func (a *App) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow requests from localhost:3000 (customer-app) and localhost:3001 (kitchen-display)
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:3000" || origin == "http://localhost:3001" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
