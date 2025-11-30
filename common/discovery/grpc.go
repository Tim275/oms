package discovery

import (
	"context"
	"log"
	"math/rand"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func ServiceConnection(ctx context.Context, serviceName string, registry Registry) (*grpc.ClientConn, error) {
	var selectedAddr string

	// Fallback: Direct service address if registry disabled (Consul disabled for Kubernetes)
	if registry == nil {
		// Default service addresses for Docker Compose / Kubernetes DNS
		serviceAddrs := map[string]string{
			"orders": "orders:9000",
			"stock":  "stock:9003",
		}

		addr, ok := serviceAddrs[serviceName]
		if !ok {
			log.Printf("ERROR: Unknown service %s (registry disabled)", serviceName)
			return nil, nil
		}

		selectedAddr = addr
		log.Printf("Registry disabled, using direct address for %s: %s", serviceName, selectedAddr)
	} else {
		// Original Consul discovery logic
		addrs, err := registry.Discover(ctx, serviceName)
		if err != nil {
			return nil, err
		}

		log.Printf("Discovered %d instances of %s", len(addrs), serviceName)
		selectedAddr = addrs[rand.Intn(len(addrs))]
	}

	// Create gRPC connection with OpenTelemetry
	// Use passthrough:/// scheme to bypass DNS resolver for direct addresses
	target := selectedAddr
	if registry == nil {
		target = "passthrough:///" + selectedAddr
	}

	return grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Add OpenTelemetry interceptors
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
}
