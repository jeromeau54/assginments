#!/bin/bash
set -euo pipefail

NAMESPACE="${1:-assessment}"

echo "=== Seeding data in namespace: $NAMESPACE ==="

# Wait for Redis to be ready
echo "Waiting for Redis pod..."
kubectl -n "$NAMESPACE" wait --for=condition=Ready pod -l app=redis --timeout=120s

REDIS_POD=$(kubectl -n "$NAMESPACE" get pod -l app=redis -o jsonpath='{.items[0].metadata.name}')

echo "Seeding Redis with test data..."
kubectl -n "$NAMESPACE" exec "$REDIS_POD" -- redis-cli SET ping:status "initialized"
kubectl -n "$NAMESPACE" exec "$REDIS_POD" -- redis-cli SET ping:count "0"

echo "=== Seed data complete ==="
kubectl -n "$NAMESPACE" exec "$REDIS_POD" -- redis-cli KEYS '*'
