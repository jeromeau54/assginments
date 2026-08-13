.PHONY: build-gateway build-pinger build test compose-up compose-down k8s-deploy k8s-destroy setup-cluster clean

# Build
build-gateway:
	cd services/gateway && CGO_ENABLED=0 go build -o ../../bin/gateway ./cmd/gateway

build-pinger:
	cd services/pinger && CGO_ENABLED=0 go build -o ../../bin/pinger ./cmd/pinger

build: build-gateway build-pinger

# Test
test:
	cd services/gateway && go test ./...
	cd services/pinger && go test ./...

# Docker Compose
compose-up:
	docker compose up -d --build

compose-down:
	docker compose down -v

compose-logs:
	docker compose logs -f

# Kubernetes
setup-cluster:
	./scripts/setup-cluster.sh

k8s-deploy:
	kubectl apply -k k8s/base/

k8s-destroy:
	kubectl delete -k k8s/base/ --ignore-not-found

# Docker images
docker-gateway:
	docker build -t devops/gateway:latest -f services/gateway/Dockerfile services/gateway/

docker-pinger:
	docker build -t devops/pinger:latest -f services/pinger/Dockerfile services/pinger/

docker-images: docker-gateway docker-pinger

# Clean
clean:
	rm -rf bin/
	docker compose down -v 2>/dev/null || true
