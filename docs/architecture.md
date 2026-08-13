# Architecture

## Service Communication

The gateway service acts as the entry point. It routes requests to the pinger service and uses Redis for caching.

The pinger service periodically checks the health of the gateway and stores results.

Redis is used as a shared cache between services.

## Configuration

All services are configured via environment variables. See each service's source code for available options.

## Networking

In Docker Compose, services communicate via the Docker network using service names as hostnames.

In Kubernetes, services communicate via ClusterIP services and DNS (e.g., `<service>.<namespace>.svc.cluster.local`).
