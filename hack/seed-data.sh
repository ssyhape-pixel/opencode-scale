#!/usr/bin/env bash
set -euo pipefail

ROUTER_URL="${ROUTER_URL:-http://localhost:8080}"

echo "=== Seeding test data for OpenCode Scale ==="

# Health check
echo ""
echo "--- Health Check ---"
curl -sf "$ROUTER_URL/health" | python3 -m json.tool || {
  echo "ERROR: Router not reachable at $ROUTER_URL"
  exit 1
}

# Submit test tasks
echo ""
echo "--- Submitting test tasks ---"

for i in $(seq 1 3); do
  echo ""
  echo "Task $i:"
  curl -sf -X POST "$ROUTER_URL/api/v1/tasks" \
    -H "Content-Type: application/json" \
    -d "{
      \"prompt\": \"Write a Go function that calculates fibonacci up to n=$((i * 10)) terms.\",
      \"timeout\": 300,
      \"userId\": \"test-user-$i\"
    }" | python3 -m json.tool || echo "  (failed)"
done

echo ""
echo "--- Submit task with controlled output ---"
curl -sf -X POST "$ROUTER_URL/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "OUTPUT:{\"answer\":42,\"status\":\"ok\"}",
    "timeout": 120,
    "userId": "test-user-output"
  }' | python3 -m json.tool || echo "  (failed)"

echo ""
echo "--- Submit task with schema validation ---"
curl -sf -X POST "$ROUTER_URL/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "OUTPUT:{\"host\":\"localhost\",\"port\":8080,\"tls\":false}",
    "outputSchema": "{\"type\":\"object\",\"properties\":{\"host\":{\"type\":\"string\"},\"port\":{\"type\":\"integer\"},\"tls\":{\"type\":\"boolean\"}},\"required\":[\"host\",\"port\"]}",
    "timeout": 120,
    "userId": "test-user-schema"
  }' | python3 -m json.tool || echo "  (failed)"

echo ""
echo "=== Seed complete ==="
