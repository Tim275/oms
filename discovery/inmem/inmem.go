package inmem

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/timour/order-microservices/discovery"
)

// Registry: In-Memory Service Registry
// Warum brauchen wir das?
// → Unit Tests: Kein Consul nötig! Einfach inmem.NewRegistry()
// → Local Development: Ohne Docker arbeiten
// → CI/CD: Schnellere Tests ohne externe Dependencies
//
// Production: Nutzt consul.Registry (siehe consul/consul.go)
// Testing/Local: Nutzt inmem.Registry
type Registry struct {
	sync.RWMutex
	addrs map[string]map[string]*serviceInstance
}

type serviceInstance struct {
	hostPort   string
	lastActive time.Time
}

func NewRegistry() *Registry {
	return &Registry{addrs: map[string]map[string]*serviceInstance{}}
}

// Register: Registriert einen Service
// Warum lastActive?
// → TTL Tracking: Wir wissen wann Service zuletzt aktiv war
// → ServiceAddresses() filtert alte Services raus (siehe unten)
func (r *Registry) Register(ctx context.Context, instanceID, serviceName, hostPort string) error {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.addrs[serviceName]; !ok {
		r.addrs[serviceName] = map[string]*serviceInstance{}
	}

	r.addrs[serviceName][instanceID] = &serviceInstance{
		hostPort:   hostPort,
		lastActive: time.Now(),
	}

	return nil
}

// Deregister: Entfernt einen Service
func (r *Registry) Deregister(ctx context.Context, instanceID, serviceName string) error {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.addrs[serviceName]; !ok {
		return nil
	}

	delete(r.addrs[serviceName], instanceID)

	return nil
}

// HealthCheck: Updated lastActive timestamp
// Warum wichtig?
// → ServiceAddresses() filtert Services die > 5s alt sind raus!
// → Simuliert Consul TTL Health Checks
func (r *Registry) HealthCheck(instanceID, serviceName string) error {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.addrs[serviceName]; !ok {
		return errors.New("service is not registered yet")
	}

	if _, ok := r.addrs[serviceName][instanceID]; !ok {
		return errors.New("service instance is not registered yet")
	}

	r.addrs[serviceName][instanceID].lastActive = time.Now()

	return nil
}

// Discover: Gibt ALLE registrierten Instances zurück
// Warum kein TTL Filter?
// → Einfache Discovery: Gibt alles zurück (wie consul.Registry.Discover())
// → Für Tests: Wir wollen nicht dass Services "verschwinden"
func (r *Registry) Discover(ctx context.Context, serviceName string) ([]string, error) {
	r.RLock()
	defer r.RUnlock()

	if len(r.addrs[serviceName]) == 0 {
		return nil, errors.New("no service address found")
	}

	var res []string
	for _, i := range r.addrs[serviceName] {
		res = append(res, i.hostPort)
	}

	return res, nil
}

// ServiceAddresses: Wie Discover, aber MIT TTL Filter!
// Warum diese zusätzliche Funktion?
// → Production-like: Filtert Services die > 5s nicht mehr aktiv sind
// → Simuliert Consul's DeregisterCriticalServiceAfter
// → Für Advanced Tests: Service Health Simulation
func (r *Registry) ServiceAddresses(ctx context.Context, serviceName string) ([]string, error) {
	r.RLock()
	defer r.RUnlock()

	if len(r.addrs[serviceName]) == 0 {
		return nil, errors.New("no service address found")
	}

	var res []string
	for _, i := range r.addrs[serviceName] {
		// Warum Before(time.Now().Add(-5 * time.Second))?
		// → Wenn lastActive < (now - 5s) → Service ist "dead"
		// → Skip diesen Service!
		if i.lastActive.Before(time.Now().Add(-5 * time.Second)) {
			continue
		}
		res = append(res, i.hostPort)
	}

	return res, nil
}

// Compile-time check: Verify Registry implements discovery.Registry
var _ discovery.Registry = (*Registry)(nil)
