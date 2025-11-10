# Order Management System

> Event-driven microservices platform for order management with payment processing

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![gRPC](https://img.shields.io/badge/gRPC-Protocol_Buffers-244c5a?style=flat&logo=google)](https://grpc.io/)
[![RabbitMQ](https://img.shields.io/badge/RabbitMQ-Message_Broker-FF6600?style=flat&logo=rabbitmq)](https://www.rabbitmq.com/)
[![Stripe](https://img.shields.io/badge/Stripe-Payment_API-008CDD?style=flat&logo=stripe)](https://stripe.com/)
[![MongoDB](https://img.shields.io/badge/MongoDB-Database-47A248?style=flat&logo=mongodb)](https://www.mongodb.com/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-Database-4169E1?style=flat&logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-Cache-DC382D?style=flat&logo=redis)](https://redis.io/)
[![Jaeger](https://img.shields.io/badge/Jaeger-Tracing-66CFE3?style=flat&logo=jaeger)](https://www.jaegertracing.io/)


## Architecture 

<img width="1069" height="582" alt="image" src="https://github.com/user-attachments/assets/014c66cb-d84d-4a5a-abbf-ea5c4b738f52" />



## Local Development

### Docker Compose

For external services like RabbitMQ, MongoDB, PostgreSQL, Redis, and Jaeger, use docker compose:

```bash
docker compose up --build
```

### Start the Services

Start each microservice in a separate terminal:

```bash
cd gateway && air
cd orders && air
cd payments && air
cd stock && air
cd kitchen && air
```

### Start Stripe Server

Run the following command to start the stripe CLI:

```bash
stripe login
```

Then run the following command to listen for webhooks:

```bash
stripe listen --forward-to localhost:8082/webhook
```

Where `localhost:8082/webhook` is the payment service HTTP server address.

**Test card:** `4242 4242 4242 4242`

## UIs

- **RabbitMQ UI**: http://localhost:15672/# (guest/guest)
- **Jaeger UI**: http://localhost:16686

## Deployment

Build Docker images for each microservice and push them to a container registry. Deploy using Docker Compose or orchestration tools like Kubernetes.

**Planned:** Kubernetes deployment as tenant on [Talos Homelab Cluster](https://github.com/Tim275/talos-homelab)
