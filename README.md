# DevOps Assessment

A microservices application that needs to be debugged, deployed, and documented.

## Overview

| Service | Description | Port |
|---------|-------------|------|
| **gateway** | HTTP API gateway, routes to pinger and redis | 8000 |
| **pinger** | Health-check service, pings gateway periodically | 8000 |
| **redis** | Caching layer for ping results | 6379 |

The services run together. Gateway proxies requests to pinger and uses Redis for caching. Pinger periodically checks gateway health and stores results in Redis.

## Prerequisites

- Go 1.21+
- Docker & Docker Compose
- kubectl
- [kind](https://kind.sigs.k8s.io/) (for local Kubernetes, you may use any other alternatives)
- make

## Getting Started

```bash
# Build binaries locally
make build

# Run tests
make test

# Start with Docker Compose
make compose-up

# View logs
make compose-logs

# Stop
make compose-down
```

## Kubernetes Deployment

```bash
# Create a kind cluster
make setup-cluster

# Build and load images
make docker-images
kind load docker-image devops/gateway:latest --name devops-assessment
kind load docker-image devops/pinger:latest --name devops-assessment

# Deploy to cluster
make k8s-deploy

# Check status
kubectl -n assessment get pods

# Tear down
make k8s-destroy
```

## Repository Structure

```
.
├── services/
│   ├── gateway/          # API gateway service (Go)
│   ├── pinger/           # Health-check pinger service (Go)
│   └── redis/            # Redis configuration
├── k8s/
│   ├── base/             # Base Kubernetes manifests
│   └── overlays/         # Kustomize overlays (dev, staging)
├── docker-compose.yml    # Local multi-service setup
├── .gitlab-ci.yml        # CI/CD pipeline
├── scripts/              # Helper scripts
└── docs/                 # Documentation
```

---

## Assessment Tasks

Tiers below are suggested milestones that you may tackle in order. Tiers are cumulative — each builds on the previous.

### Tier 1 — Get it running

Fix the Dockerfiles, Docker Compose, and Kubernetes base manifests so that the entire stack works end-to-end.

**Goals**:
- Both service images build and run correctly
- `make compose-up` → all 3 services healthy, gateway responds on port 8000
- `make k8s-deploy` → all pods Running, all services have endpoints and respond

**Deliverable**: `docs/fixes.md` — for each fix, describe what was broken, how you found the issue (error messages, commands), and what you changed.

### Tier 2 — Make it reliable

Everything in Tier 1, plus:

- Fix the CI/CD pipeline (`.gitlab-ci.yml`) so it builds, tests, and packages correctly
- Fix Docker Compose so services start reliably and data persists correctly
- Update `docs/fixes.md` with your additional fixes

**Goals**:
- CI pipeline stages are correctly configured
- Compose services start reliably with proper dependency ordering
- Redis data persists across restarts

### Tier 3 — Make it production-ready

Everything in Tier 2, plus:

- Fix Kustomize overlays (dev and staging)
- Fix the deploy pipeline stage
- Propose an image tagging strategy (replace `latest`)

**Additional deliverable**: `docs/trade-offs.md` covering:
- Image tagging strategy and why
- Security improvements you'd make
- Observability recommendations
- Secrets management approach

---

## Submission

Submit your work as a Git repository with clean commit history. Each fix should be a separate, well-described commit.

Required files:
- `docs/fixes.md` (all tiers)
- `docs/trade-offs.md` (Tier 3 only)
