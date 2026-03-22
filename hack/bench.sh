#!/usr/bin/env bash
set -euo pipefail

ROUTER_URL="${ROUTER_URL:-http://localhost:8080}"
MOCK_LLM_URL="${MOCK_LLM_URL:-http://localhost:4099}"
CONCURRENCY="${CONCURRENCY:-20}"
REQUESTS="${REQUESTS:-100}"

echo "=== OpenCode Scale Stress Test ==="
echo ""
echo "  Router:      $ROUTER_URL"
echo "  Concurrency: $CONCURRENCY"
echo "  Requests:    $REQUESTS"
echo ""

# Check prerequisites.
if ! command -v curl &>/dev/null; then
    echo "ERROR: curl is required"
    exit 1
fi

# Health check.
echo "--- 1. Health Check ---"
curl -sf "$ROUTER_URL/health" | python3 -m json.tool
echo ""

# Check if mock-llm-api is available (rate-limit mode).
RATELIMIT_MODE=false
if curl -sf "$MOCK_LLM_URL/health" &>/dev/null; then
    RATELIMIT_MODE=true
    echo "--- Rate-limit mode detected (mock-llm-api at $MOCK_LLM_URL) ---"
    echo ""
fi

# Sequential baseline.
echo "--- 2. Sequential Baseline (10 tasks) ---"
TASK_IDS=()
START=$(date +%s)

for i in $(seq 1 10); do
    RESP=$(curl -sf -X POST "$ROUTER_URL/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -d "{\"prompt\":\"OUTPUT:bench-result-$i\",\"timeout\":60,\"userId\":\"bench-user-$i\"}")
    TASK_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['taskId'])" 2>/dev/null || echo "")
    if [[ -n "$TASK_ID" ]]; then
        TASK_IDS+=("$TASK_ID")
        echo "  Created task $i: $TASK_ID"
    else
        echo "  Failed to create task $i: $RESP"
    fi
done

END=$(date +%s)
echo "  10 tasks created in $((END - START))s"
echo ""

# Wait for tasks to complete.
echo "--- 3. Waiting for tasks to complete ---"
COMPLETED=0
FAILED=0
TIMEOUT=120
WAIT_START=$(date +%s)

while [[ $((COMPLETED + FAILED)) -lt ${#TASK_IDS[@]} ]]; do
    ELAPSED=$(($(date +%s) - WAIT_START))
    if [[ $ELAPSED -gt $TIMEOUT ]]; then
        echo "  TIMEOUT: waited ${TIMEOUT}s, $COMPLETED completed, $FAILED failed, $((${#TASK_IDS[@]} - COMPLETED - FAILED)) pending"
        break
    fi

    for TASK_ID in "${TASK_IDS[@]}"; do
        STATUS=$(curl -sf "$ROUTER_URL/api/v1/tasks/$TASK_ID" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])" 2>/dev/null || echo "unknown")
        case "$STATUS" in
            completed) COMPLETED=$((COMPLETED + 1)) ;;
            failed|timeout) FAILED=$((FAILED + 1)) ;;
        esac
    done

    if [[ $((COMPLETED + FAILED)) -lt ${#TASK_IDS[@]} ]]; then
        sleep 2
    fi
done

WAIT_END=$(date +%s)
echo "  Results: $COMPLETED completed, $FAILED failed in $((WAIT_END - WAIT_START))s"
echo ""

# Concurrent burst.
echo "--- 4. Concurrent Burst ($CONCURRENCY concurrent, $REQUESTS total) ---"
echo "  Submitting tasks..."
START=$(date +%s)

TMPDIR=$(mktemp -d)
for i in $(seq 1 $REQUESTS); do
    (
        RESP=$(curl -sf -X POST "$ROUTER_URL/api/v1/tasks" \
            -H "Content-Type: application/json" \
            -d "{\"prompt\":\"OUTPUT:burst-$i\",\"timeout\":60,\"userId\":\"burst-user\"}" \
            -w "\n%{http_code}" 2>/dev/null || echo "error")
        echo "$RESP" > "$TMPDIR/task-$i.txt"
    ) &

    # Limit concurrency.
    if (( i % CONCURRENCY == 0 )); then
        wait
    fi
done
wait

END=$(date +%s)
DURATION=$((END - START))

# Count results.
CREATED=0
ERRORS=0
for f in "$TMPDIR"/task-*.txt; do
    HTTP_CODE=$(tail -1 "$f" 2>/dev/null || echo "0")
    if [[ "$HTTP_CODE" == "202" ]]; then
        CREATED=$((CREATED + 1))
    else
        ERRORS=$((ERRORS + 1))
    fi
done

echo "  $REQUESTS requests in ${DURATION}s: $CREATED accepted, $ERRORS errors"
if [[ $DURATION -gt 0 ]]; then
    echo "  Throughput: $(( CREATED / DURATION )) tasks/sec"
fi

rm -rf "$TMPDIR"
echo ""

# Rate limit stats (if available).
if [[ "$RATELIMIT_MODE" == "true" ]]; then
    echo "--- 5. Rate Limit Status ---"
    curl -sf "$MOCK_LLM_URL/debug/rate-limits" | python3 -m json.tool 2>/dev/null || echo "  (not available)"
    echo ""
fi

echo "=== Stress test complete ==="
