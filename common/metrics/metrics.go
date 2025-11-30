package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTPMetrics contains HTTP-related Prometheus metrics
type HTTPMetrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
}

// GRPCMetrics contains gRPC-related Prometheus metrics
type GRPCMetrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
}

// BusinessMetrics contains business-specific metrics
type BusinessMetrics struct {
	OrdersCreated      prometheus.Counter
	OrdersPaid         prometheus.Counter
	PaymentLinksCreated prometheus.Counter
	StripeAPIDuration  prometheus.Histogram
}

// NewHTTPMetrics creates HTTP metrics for a service
func NewHTTPMetrics(serviceName string) *HTTPMetrics {
	return &HTTPMetrics{
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: serviceName + "_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    serviceName + "_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
	}
}

// NewGRPCMetrics creates gRPC metrics for a service
func NewGRPCMetrics(serviceName string) *GRPCMetrics {
	return &GRPCMetrics{
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: serviceName + "_grpc_requests_total",
				Help: "Total number of gRPC requests",
			},
			[]string{"method", "status"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    serviceName + "_grpc_request_duration_seconds",
				Help:    "gRPC request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method"},
		),
	}
}

// NewBusinessMetrics creates business-specific metrics
func NewBusinessMetrics(serviceName string) *BusinessMetrics {
	return &BusinessMetrics{
		OrdersCreated: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: serviceName + "_orders_created_total",
				Help: "Total number of orders created",
			},
		),
		OrdersPaid: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: serviceName + "_orders_paid_total",
				Help: "Total number of orders paid",
			},
		),
		PaymentLinksCreated: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: serviceName + "_payment_links_created_total",
				Help: "Total number of payment links created",
			},
		),
		StripeAPIDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    serviceName + "_stripe_api_duration_seconds",
				Help:    "Stripe API call duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
		),
	}
}

// RecordHTTPRequest records an HTTP request metric
func (m *HTTPMetrics) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	m.RequestsTotal.WithLabelValues(method, path, status).Inc()
	m.RequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// RecordGRPCRequest records a gRPC request metric
func (m *GRPCMetrics) RecordGRPCRequest(method, status string, duration time.Duration) {
	m.RequestsTotal.WithLabelValues(method, status).Inc()
	m.RequestDuration.WithLabelValues(method).Observe(duration.Seconds())
}
