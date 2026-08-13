#!/bin/bash
set -euo pipefail

CLUSTER_NAME="${1:-devops-assessment}"

echo "=== Setting up kind cluster: $CLUSTER_NAME ==="

# Check prerequisites
for cmd in kind kubectl docker; do
  if ! command -v "$cmd" &> /dev/null; then
    echo "ERROR: $cmd is required but not installed."
    exit 1
  fi
done

# Delete existing cluster if it exists
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  echo "Deleting existing cluster..."
  kind delete cluster --name "$CLUSTER_NAME"
fi

# Create cluster
echo "Creating cluster..."
kind create cluster --name "$CLUSTER_NAME" --config k8s/kind-config.yaml

# Wait for cluster to be ready
echo "Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=120s

echo "=== Cluster $CLUSTER_NAME is ready ==="
kubectl cluster-info
