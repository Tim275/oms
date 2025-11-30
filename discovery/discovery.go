package discovery

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

type Registry interface {
	Register(ctx context.Context, instanceID, serviceName, hostPort string) error
	Deregister(ctx context.Context, instanceID, serviceName string) error
	Discover(ctx context.Context, serviceName string) ([]string, error)
	HealthCheck(instanceID, serviceName string) error
}

// GenerateInstanceID: Generiert eine unique Instance ID
// Warum brauchen wir das?
// → Jeder Service braucht eine UNIQUE ID für Consul/Registry
// → Format: "orders-123456789" (serviceName + random number)
// → Random: Verhindert Kollisionen wenn mehrere Instances starten
//
// Usage:
// instanceID := discovery.GenerateInstanceID("orders")
// registry.Register(ctx, instanceID, "orders", "localhost:9000")
func GenerateInstanceID(serviceName string) string {
	return fmt.Sprintf("%s-%d", serviceName, rand.New(rand.NewSource(time.Now().UnixNano())).Int())
}
