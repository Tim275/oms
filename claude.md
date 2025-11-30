# Kubernetes Migration Status - Order Management System

## ⚠️ GIT COMMIT RICHTLINIEN

**WICHTIG**: Alle Commits nur mit Tim275 als Autor!
- ✅ Autor: Tim275
- ❌ KEIN Co-Authored-By: Claude
- Commit Message: Kurz und präzise (z.B. "init", "fix cors", "add feature")

```bash
# Standard Commit (NUR Tim275):
git commit -m "init"
# NICHT Claude als Co-Author hinzufügen!
```

---

## 📚 DOKUMENTATION - NIEMALS LÖSCHEN!

**⚠️ KRITISCH: Folgende Dokumentations-Dateien NIEMALS löschen oder überschreiben:**

- ✅ `SETUP.md` - Part 1: Foundation, Clean Architecture, Gateway
- ✅ `SETUP2.md` - Part 2: Service Discovery, RabbitMQ, Payments
- ✅ `SETUP3.md` - Part 3: Stock Service, Kitchen, Production Features
- ✅ `SETUP4.md` - Kubernetes Homelab Deployment (in .gitignore)
- ✅ `ADVANCED_SETUP.md` - RabbitMQ Resilience, Production Best Practices
- ✅ `CLAUDE.md` - Kubernetes Migration Status & Projekt Dokumentation

**Diese Dateien sind die Haupt-Dokumentation des Projekts!**
- Sie enthalten die komplette IKEA-Style Aufbau-Anleitung
- Phase-by-Phase Setup mit Code-Beispielen
- Checkpoints und Test-Schritten
- Production Best Practices

**Wenn Updates nötig sind:**
- Immer nur erweitern oder präzisieren
- NIEMALS komplette Abschnitte löschen
- Bei Unsicherheit: User fragen!

---

## 📋 Projekt Übersicht

Migration des Order Management Systems von Docker Compose zu Kubernetes:
- **Phase 1**: k3d Testing (AKTUELL) ⚡
- **Phase 2**: Homelab Cluster Deployment (Talos)
- **Ziel**: Multi-Tenant Production Ready System

---

## ✅ Aktueller Stand (27.11.2025)

### 🎉 VOLLSTÄNDIGE DOCKER COMPOSE → KUBERNETES MIGRATION ERFOLGREICH!

#### Backend Services (alle Running)
- ✅ **Gateway**: 2/2 Running (HTTP :8080)
- ✅ **Orders**: 2/2 Running (gRPC :9000, Metrics :9001)
- ✅ **Payments**: 2/2 Running (HTTP :8082)
- ✅ **Stock**: 2/2 Running (gRPC :9003, HTTP :8083)
- ✅ **Kitchen**: 2/2 Running (RabbitMQ Consumer)

#### Infrastructure (alle Running)
- ✅ **PostgreSQL**: 1/1 Running (Cloud-Native Auto-Migration)
- ✅ **MongoDB**: 1/1 Running
- ✅ **RabbitMQ**: 1/1 Running
- ✅ **Redis**: 1/1 Running

#### Frontend
- ✅ **Customer App**: 2/2 Running (Dynamic API URL v1.0.3)
- ✅ **Kitchen Display**: 2/2 Running (Dynamic API URL v1.0.3)

---

## 🎯 Quick Start - System starten (k3d)

```bash
# Port Forwards starten
kubectl port-forward -n oms svc/gateway 8080:8080 &
kubectl port-forward -n oms svc/customer-app 3000:80 &
kubectl port-forward -n oms svc/kitchen-display 3001:80 &

# Browser öffnen
open http://localhost:3000        # Customer App
open http://localhost:3001        # Kitchen Display

# API testen
curl http://localhost:8080/api/menu | jq
```

---

## 📊 Services & Ports

| Service | Port | Protocol |
|---------|------|----------|
| Gateway | 8080 | HTTP |
| Orders | 9000, 9001 | gRPC, Metrics |
| Payments | 8082, 9002 | HTTP, Metrics |
| Stock | 9003, 8083 | gRPC, HTTP/Metrics |
| Kitchen | 8083 | Metrics |

---

## 🗂️ Kubernetes Konfiguration

### K8s Manifests Location
```
/Users/timour/Desktop/Golang/oms-k8s/
├── base/                    # Base deployments
└── overlays/
    ├── local-k3d/          # Local testing
    └── homelab/            # Talos production
```

---

## 📦 Docker Images (ghcr.io/tim275/)

| Image | Tag | Platform |
|-------|-----|----------|
| oms-gateway | v1.0.0 | linux/amd64 |
| oms-orders | v1.0.0 | linux/amd64 |
| oms-payments | v1.0.0 | linux/amd64 |
| oms-stock | v1.0.2 | linux/amd64 |
| oms-kitchen | v1.0.0 | linux/amd64 |
| oms-customer-app | v1.0.3 | linux/amd64 |
| oms-kitchen-display | v1.0.3 | linux/amd64 |

Build:
```bash
docker buildx build --platform linux/amd64 -t ghcr.io/tim275/oms-<service>:v1.0.0 -f <service>/Dockerfile --push .
```

---

## 🔑 Key Features

- **Cloud-Native Auto-Migration**: Stock service creates its own DB schema on startup
- **Dynamic API URL**: Frontend detects localhost vs production automatically
- **Consul Optional**: Services work with or without Consul
- **OpenTelemetry**: Full tracing (otelhttp, otelgrpc)
- **Prometheus Metrics**: All services expose /metrics
- **Structured Logging**: slog/zap with trace_id correlation

---

## 🐛 Troubleshooting

```bash
# Logs
kubectl logs -n oms -l app=<service> --tail=100

# Events
kubectl get events -n oms --sort-by='.lastTimestamp'

# Debug pod
kubectl run -it --rm debug --image=nicolaka/netshoot -n oms -- /bin/bash

# Restart
kubectl rollout restart deployment/<name> -n oms
```

---

## 📊 Environment Variables

Key configs in `oms-k8s/base/configmap.yaml`:
- `MONGODB_URI` - Orders database
- `POSTGRES_URI` - Stock database
- `RABBITMQ_URL` - Message broker
- `REDIS_ADDR` - Cache
- `CONSUL_ADDR` - Service discovery (optional)
- `OTEL_EXPORTER_OTLP_ENDPOINT` - Tracing

---

**Last Updated**: 27.11.2025
- ja die sind gefixt das ist gut :) Thank You!
#...
Order placed successfully

Processing your order... aber paar sachen stimmen immernoch cniht 1) ich bekomme keine ordner nummer..Thank You!
#...
Order placed successfully

Processing your order... bitte prüfe was nachden bezhalen passiert (backend logs)