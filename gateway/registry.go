package main

import (
	"context"
	"log"
	"time"

	"github.com/timour/order-microservices/discovery"
)

type ServiceRegistration struct {
	registry    discovery.Registry
	instanceID  string
	serviceName string
	stopChan    chan struct{}
}

// RegisterService: Registriert Service bei Consul und startet Health Checks
func RegisterService(
	ctx context.Context,
	registry discovery.Registry,
	instanceID, serviceName, addr string,
) (*ServiceRegistration, error) {
	if err := registry.Register(ctx, instanceID, serviceName, addr); err != nil {
		return nil, err
	}

	sr := &ServiceRegistration{
		registry:    registry,
		instanceID:  instanceID,
		serviceName: serviceName,
		stopChan:    make(chan struct{}),
	}

	go sr.startHealthCheck()

	return sr, nil
}

func (sr *ServiceRegistration) startHealthCheck() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sr.stopChan:
			return
		case <-ticker.C:
			if err := sr.registry.HealthCheck(sr.instanceID, sr.serviceName); err != nil {
				log.Printf("Health check failed: %v", err)
			}
		}
	}
}

// Deregister: Meldet Service bei Consul ab
func (sr *ServiceRegistration) Deregister(ctx context.Context) error {
	close(sr.stopChan)
	return sr.registry.Deregister(ctx, sr.instanceID, sr.serviceName)
}
