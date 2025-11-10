package discovery

import (
	"context"
	"fmt"
	"log"
	"math/rand"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ServiceConnection: Zentrale gRPC Connection Helper mit OpenTelemetry
// Warum dieser Helper?
// → DRY: Alle Services nutzen gleichen Code (keine Duplication!)
// → OpenTelemetry: Middleware ist ZENTRAL implementiert
// → Load Balancing: Random selection wenn mehrere Instances
// → Service Discovery: Nutzt Registry.Discover()
//
// Usage:
// conn, err := discovery.ServiceConnection(ctx, "orders", registry)
// if err != nil { ... }
// defer conn.Close()
// client := api.NewOrderServiceClient(conn)
func ServiceConnection(ctx context.Context, serviceName string, registry Registry) (*grpc.ClientConn, error) {
	// Warum registry.Discover?
	// → Findet alle verfügbaren Instances des Services
	// → z.B. ["localhost:9000", "localhost:9001"] (wenn 2 Instances)
	addrs, err := registry.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("no instances found for service %s", serviceName)
	}

	log.Printf("Discovered %d instances of %s", len(addrs), serviceName)

	// Warum rand.Intn?
	// → Simple Load Balancing: Random Instance auswählen
	// → Production: Könnte Round-Robin, Least-Connections, etc. sein
	selectedAddr := addrs[rand.Intn(len(addrs))]

	// Warum grpc.Dial (deprecated) statt grpc.NewClient?
	// → NewClient ist non-blocking (wartet nicht auf Connection)
	// → Dial ist blocking (wartet bis connected oder timeout)
	// → Für Service Discovery besser: Sofort wissen ob Service erreichbar!
	//
	// ⭐ OpenTelemetry Middleware:
	// → UnaryClientInterceptor: Für normale RPC Calls (CreateOrder, UpdateOrder, etc.)
	// → StreamClientInterceptor: Für Streaming RPCs (falls wir später haben)
	// → Automatisches Tracing: Span wird automatisch erstellt für jeden RPC!
	return grpc.DialContext(
		ctx,
		selectedAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// ⭐ OpenTelemetry Interceptors - DAS IST DER GAME CHANGER!
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
}
