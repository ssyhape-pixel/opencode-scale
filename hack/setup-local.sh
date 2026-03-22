#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# If --wait-only is passed, skip docker compose up.
WAIT_ONLY=false
if [[ "${1:-}" == "--wait-only" ]]; then
    WAIT_ONLY=true
fi

if [[ "$WAIT_ONLY" == "false" ]]; then
    echo "==> Starting docker-compose services..."
    docker compose up --build -d
fi

echo "==> Waiting for Temporal to be ready..."
MAX_RETRIES=60
for i in $(seq 1 $MAX_RETRIES); do
    if docker compose exec -T temporal tctl --address temporal:7233 cluster health 2>/dev/null | grep -q SERVING; then
        echo "    Temporal is ready."
        break
    fi
    if [[ $i -eq $MAX_RETRIES ]]; then
        echo "    ERROR: Temporal did not become ready within $MAX_RETRIES attempts."
        docker compose logs temporal
        exit 1
    fi
    sleep 2
done

echo "==> Waiting for router to be ready..."
for i in $(seq 1 30); do
    if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
        echo "    Router is ready."
        break
    fi
    if [[ $i -eq 30 ]]; then
        echo "    ERROR: Router did not become ready."
        docker compose logs router
        exit 1
    fi
    sleep 2
done

echo "==> Waiting for mock-opencode to be ready..."
for i in $(seq 1 15); do
    if curl -sf http://localhost:4096/health > /dev/null 2>&1; then
        echo "    Mock OpenCode server is ready."
        break
    fi
    if [[ $i -eq 15 ]]; then
        echo "    ERROR: Mock OpenCode server did not become ready."
        docker compose logs mock-opencode
        exit 1
    fi
    sleep 2
done

echo ""
echo "==> All services are up and running!"
echo ""
echo "  Router:          http://localhost:8080"
echo "  Router Health:   http://localhost:8080/health"
echo "  Prometheus:      http://localhost:9090/metrics"
echo "  Temporal UI:     http://localhost:8233"
echo "  Mock OpenCode:   http://localhost:4096"
echo ""
echo "To run seed data:  make seed"
echo "To run E2E tests:  make test-e2e"
echo "To stop:           make compose-down"
