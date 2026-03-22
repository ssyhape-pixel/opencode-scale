# Getting Started

[English](getting-started.md) | [中文](getting-started.zh-CN.md)

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.25+ | Build the binaries |
| Docker | 24+ | Container images and local dev |
| kubectl | 1.28+ | Cluster management (K8s deployment only) |
| Helm | 3.13+ | Chart-based deployment (K8s deployment only) |

Optional:

- `golangci-lint` — for `make lint`
- `jq` — for parsing JSON responses
- `Kind` 0.20+ — for local K8s cluster

## Quick Start

```bash
git clone https://github.com/opencode-scale/opencode-scale.git
cd opencode-scale

# Install dependencies
make deps

# Build all 5 binaries
make build
# Outputs: bin/router, bin/controller, bin/worker, bin/mock-opencode, bin/mock-llm-api

# Run tests
make test
```

## Local Development (Docker Compose)

The fastest way to get a running environment. No K8s cluster required.

### Start all services

```bash
make compose-up
```

This starts 5 services:

| Service | Port | Purpose |
|---------|------|---------|
| Temporal | 7233 | Workflow orchestration |
| Temporal UI | 8233 | Web UI for workflow inspection |
| mock-opencode | 4096 | Simulated OpenCode Server |
| Router | 8080, 9090 | HTTP gateway + Prometheus metrics |
| Worker | — | Temporal activity executor |

### Verify it's running

```bash
curl -s http://localhost:8080/health | jq .
```

Expected:

```json
{
  "status": "ok",
  "version": "0.1.0",
  "poolUtilization": 0,
  "poolAllocated": 0,
  "poolMaxSize": 10,
  "queueDepth": 0
}
```

### Seed test data

```bash
make seed
```

Submits sample coding tasks (Fibonacci, controlled output, schema validation).

### View logs

```bash
make compose-logs
```

### Stop

```bash
make compose-down
```

## Running Without Docker

You can run the router standalone with the mock provider. No Temporal, K8s, or Docker needed.

1. Create a minimal config:

```yaml
# config-local.yaml
pool:
  minReady: 0
  maxSize: 10
  mode: local
  mockTarget: "localhost:4096"
router:
  listenAddr: ":8080"
temporal:
  hostPort: "localhost:7233"
  namespace: "opencode-scale"
  taskQueue: "coding-tasks"
```

2. Start the mock server and router:

```bash
./bin/mock-opencode &
./bin/router -config config-local.yaml
```

The router starts on `:8080`. If Temporal is unreachable, the task API is disabled but health and proxy endpoints still work.

## API Reference

### Authentication

Configured via `API_KEYS` environment variable (comma-separated) or `router.apiKeys` in YAML.

```bash
# Header-based
curl -H "X-API-Key: your-key" http://localhost:8080/api/v1/tasks

# Bearer token
curl -H "Authorization: Bearer your-key" http://localhost:8080/api/v1/tasks
```

When `apiKeys` is empty (default), authentication is disabled.

The `/health` endpoint always bypasses authentication.

### Create a Task

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Write a function that sorts a list of integers using merge sort",
    "timeout": 300,
    "userId": "test-user"
  }' | jq .
```

Response:

```json
{
  "taskId": "d290f1ee-6c54-4b01-90e6-d701748f0851",
  "status": "pending",
  "createdAt": "2026-01-15T10:30:00Z"
}
```

**Request fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `prompt` | string | Yes | The coding task description |
| `outputSchema` | string | No | JSON Schema to validate output against |
| `timeout` | int | No | Timeout in seconds (default 1800, max 7200) |
| `userId` | string | No | User identifier for tracking |
| `metadata` | object | No | Custom key-value metadata |

### Create a Task with Schema Validation

When `outputSchema` is provided, the system validates the LLM output against the schema. If validation fails, it automatically re-prompts the LLM with feedback about what went wrong (up to 3 attempts).

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Return a JSON object with fields: name (string) and age (integer)",
    "timeout": 120,
    "outputSchema": "{\"type\":\"object\",\"required\":[\"name\",\"age\"],\"properties\":{\"name\":{\"type\":\"string\"},\"age\":{\"type\":\"integer\"}}}"
  }' | jq .
```

### Check Task Status

```bash
curl -s http://localhost:8080/api/v1/tasks/{taskId} | jq .
```

Response (completed):

```json
{
  "taskId": "d290f1ee-6c54-4b01-90e6-d701748f0851",
  "sessionId": "sess-1705312200123",
  "status": "completed",
  "result": "def merge_sort(arr): ...",
  "createdAt": "2026-01-15T10:30:00Z",
  "completedAt": "2026-01-15T10:32:15Z"
}
```

**Task status values:**

| Status | Description |
|--------|-------------|
| `pending` | Workflow started, waiting for worker |
| `running` | Worker is executing the task |
| `completed` | Task finished successfully |
| `failed` | Task encountered an error |
| `timeout` | Task exceeded its timeout |

### Stream Task Updates (SSE)

Real-time task progress via Server-Sent Events. The endpoint polls Temporal every 2 seconds.

```bash
curl -N http://localhost:8080/api/v1/tasks/{taskId}/stream
```

Output while running:

```
event: status
data: {"status":"running"}

event: status
data: {"status":"running"}
```

Output on completion:

```
event: result
data: {"status":"completed","sessionId":"sess-123","result":"...","duration":45.2,"tokensUsed":1500}
```

The stream closes automatically when the task reaches a terminal state (completed, failed, or timeout).

### Health Check

```bash
curl -s http://localhost:8080/health | jq .
```

```json
{
  "status": "ok",
  "version": "0.1.0",
  "poolUtilization": 0.2,
  "poolAllocated": 10,
  "poolMaxSize": 50,
  "queueDepth": 0
}
```

### Session Proxy (Direct Access)

For direct interaction with a sandboxed OpenCode Server, send requests to `/api/v1/*` with a session identifier:

```bash
# First request — allocates a sandbox, returns session ID
curl -v http://localhost:8080/api/v1/sessions \
  -H "X-User-ID: test-user"
# Response includes: X-Session-ID header + session_id cookie

# Subsequent requests — routed to same sandbox
curl http://localhost:8080/api/v1/sessions/{sessionId}/messages \
  -H "X-Session-ID: {sessionId}" \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Hello"}'
```

Session ID extraction priority: `X-Session-ID` header > `session_id` cookie > `session_id` query param.

## Configuration

Configuration is loaded from YAML with environment variable overrides. Load order:

1. Built-in defaults
2. YAML file values
3. Environment variables (highest priority)

All binaries accept `-config path/to/config.yaml` (default: `config.yaml`).

### Full Config Example

```yaml
pool:
  minReady: 3              # Warm sandbox instances to maintain
  maxSize: 50              # Max concurrent allocations
  idleTimeout: 10m         # Idle reclaim threshold
  gcInterval: 30s          # GC scan frequency
  warmPoolName: default    # SandboxWarmPool CRD name
  namespace: opencode-scale
  mode: local              # "local" (mock) or "k8s" (real sandboxes)
  mockTarget: localhost:4096

router:
  listenAddr: ":8080"
  sessionHeader: "X-Session-ID"
  sessionCookie: "session_id"
  sessionQuery: "session_id"
  apiKeys: []              # Empty = auth disabled
  maxBodyBytes: 1048576    # 1 MB

temporal:
  hostPort: "localhost:7233"
  namespace: "opencode-scale"
  taskQueue: "coding-tasks"

telemetry:
  serviceName: "opencode-scale"
  otelEndpoint: "localhost:4317"
  prometheusPort: 9090
  logLevel: "info"

litellm:
  endpoint: "http://localhost:4000"
  apiKey: ""
```

### Environment Variables

| Variable | Config Path | Default | Description |
|----------|-------------|---------|-------------|
| `POOL_MIN_READY` | `pool.minReady` | `3` | Warm sandbox count |
| `POOL_MAX_SIZE` | `pool.maxSize` | `50` | Max concurrent sandboxes |
| `POOL_IDLE_TIMEOUT` | `pool.idleTimeout` | `10m` | Idle reclaim threshold |
| `POOL_NAMESPACE` | `pool.namespace` | `opencode-scale` | K8s namespace |
| `POOL_MODE` | `pool.mode` | `local` | `local` or `k8s` |
| `MOCK_OPENCODE_TARGET` | `pool.mockTarget` | `localhost:4096` | Mock provider target |
| `ROUTER_LISTEN_ADDR` | `router.listenAddr` | `:8080` | HTTP listen address |
| `API_KEYS` | `router.apiKeys` | _(empty)_ | Comma-separated API keys |
| `MAX_BODY_BYTES` | `router.maxBodyBytes` | `1048576` | Max request body size |
| `TEMPORAL_HOST_PORT` | `temporal.hostPort` | `localhost:7233` | Temporal gRPC address |
| `TEMPORAL_NAMESPACE` | `temporal.namespace` | `opencode-scale` | Temporal namespace |
| `TEMPORAL_TASK_QUEUE` | `temporal.taskQueue` | `coding-tasks` | Temporal task queue |
| `OTEL_ENDPOINT` | `telemetry.otelEndpoint` | `localhost:4317` | OTel Collector endpoint |
| `PROMETHEUS_PORT` | `telemetry.prometheusPort` | `9090` | Prometheus metrics port |
| `LITELLM_ENDPOINT` | `litellm.endpoint` | `http://localhost:4000` | LiteLLM proxy URL |
| `LITELLM_API_KEY` | `litellm.apiKey` | _(empty)_ | LiteLLM API key |

Duration values use Go syntax: `30s`, `5m`, `1h`.

## K8s Deployment

### Using Helm

```bash
helm install opencode-scale ./charts/opencode-scale \
  --namespace opencode-scale \
  --create-namespace \
  --set config.pool.mode=k8s \
  --set config.pool.minReady=3 \
  --set config.pool.maxSize=50
```

The chart deploys 3 components: Router (2 replicas), Worker (2 replicas), Controller (1 replica).

### Using Kustomize

```bash
# Dev environment (reduced resources, 1 replica each)
make deploy-dev

# Production environment
make deploy-prod
```

### Verify Deployment

```bash
kubectl -n opencode-scale get pods
kubectl -n opencode-scale get sandboxclaims
kubectl -n opencode-scale get sandboxwarmpool
```

## Archival (S3/MinIO)

Temporal workflows generate execution histories that accumulate in PostgreSQL. Archival moves completed workflow histories to S3-compatible object storage before they are purged by Temporal's retention policy.

### Quick Start with MinIO

One command starts MinIO alongside the existing services and configures archival:

```bash
make compose-archival
```

This:
1. Starts MinIO (S3-compatible storage) on ports 9000 (API) and 9001 (Console)
2. Creates the `temporal-archival` bucket
3. Mounts a Temporal config template that enables archival
4. Configures the `default` namespace with 72h retention and archival enabled

### Verify Archival is Working

```bash
# Check namespace configuration
docker compose exec -T temporal tctl --address $(docker compose exec -T temporal hostname -i):7233 \
  namespace describe --namespace default
# Look for: HistoryArchivalState: Enabled, VisibilityArchivalState: Enabled

# Submit tasks and wait for completion
make seed

# Open MinIO Console at http://localhost:9001 (minioadmin/minioadmin)
# Check temporal-archival bucket for history/ and visibility/ directories
```

### Using AWS S3 Instead of MinIO

For production, use a real S3 bucket:

```bash
ARCHIVAL_ENABLED=true \
  ARCHIVAL_S3_ENDPOINT="" \
  ARCHIVAL_S3_FORCE_PATH_STYLE=false \
  ARCHIVAL_S3_REGION=us-west-2 \
  ARCHIVAL_S3_BUCKET=my-bucket \
  AWS_ACCESS_KEY_ID=AKIA... \
  AWS_SECRET_ACCESS_KEY=... \
  docker compose up -d
```

No `--profile archival` needed when using AWS S3 directly (MinIO is not started).

### Default Behavior (No Archival)

Running `docker compose up -d` or `make compose-up` works exactly as before. Archival is disabled by default (`ARCHIVAL_ENABLED=false`).

## Rate-Limit Testing

Test LiteLLM key rotation and rate limiting with the mock LLM API:

```bash
# Start full stack with rate-limiting
make compose-ratelimit
```

This adds:

| Service | Port | Purpose |
|---------|------|---------|
| mock-llm-api | 4099 | Fake OpenAI API with per-key RPM/TPM limits |
| litellm | 4000 | Multi-key LLM proxy with rotation |

```bash
# Run stress test
make bench

# Check rate limit status
curl http://localhost:4099/debug/rate-limits | jq .
```

## Observability

### Prometheus Metrics

Available at `:9090/metrics` on all components.

| Metric | Type | Description |
|--------|------|-------------|
| `opencode_scale_pool_size` | Gauge | Total pool capacity |
| `opencode_scale_allocated_count` | Gauge | Currently allocated sandboxes |
| `opencode_scale_wait_queue_length` | Gauge | Queued requests |
| `opencode_scale_allocation_latency` | Histogram | Sandbox allocation time (seconds) |
| `opencode_scale_task_duration` | Histogram | Task execution time (seconds) |
| `opencode_scale_task_status` | Counter | Task completions by status |
| `opencode_scale_llm_tokens_total` | Counter | Total LLM tokens consumed |

### Grafana Dashboards

Pre-configured dashboards at `deploy/base/otel/grafana/dashboards/opencode-scale.json`.

```bash
# K8s
kubectl -n opencode-scale port-forward svc/grafana 3000:3000

# Open http://localhost:3000
```

### Temporal UI

Inspect workflow executions, activity history, and retry attempts.

```bash
# Docker Compose: http://localhost:8233
# K8s: kubectl -n opencode-scale port-forward svc/temporal-frontend 8233:7233
```

### Audit Logs

Every HTTP request is logged as structured JSON to stdout:

```json
{
  "level": "INFO",
  "msg": "audit",
  "method": "POST",
  "path": "/api/v1/tasks",
  "status": 202,
  "duration_ms": 15,
  "remote": "127.0.0.1:45678",
  "user_id": "test-user"
}
```

## Makefile Reference

| Target | Description |
|--------|-------------|
| `make build` | Build all 5 binaries to `bin/` |
| `make test` | Run tests with race detection + coverage |
| `make test-short` | Quick test run |
| `make test-e2e` | End-to-end tests (starts Docker Compose) |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code |
| `make vet` | Run `go vet` |
| `make coverage` | Generate HTML coverage report |
| `make docker-build` | Build all Docker images |
| `make docker-push` | Push images to registry |
| `make compose-up` | Start local dev environment |
| `make compose-down` | Stop and clean up |
| `make compose-logs` | Follow service logs |
| `make compose-archival` | Start with S3/MinIO archival |
| `make compose-ratelimit` | Start with rate-limit testing |
| `make bench` | Run stress test |
| `make deploy-dev` | Deploy to K8s dev overlay |
| `make deploy-prod` | Deploy to K8s prod overlay |
| `make seed` | Submit sample tasks |
| `make clean` | Remove binaries and coverage files |

## Troubleshooting

### Router fails to start: "reading config file"

The router expects a config file at the path given by `-config` (default: `config.yaml`). Ensure the file exists and is valid YAML.

### Task API returns 404

The task API is only registered when Temporal connects successfully. Check the router logs for:

```
WARN temporal client unavailable, task API disabled
```

Ensure Temporal is running and `TEMPORAL_HOST_PORT` is correct.

### Sandbox allocation timeout (5 min)

The `K8sSandboxProvider` polls for `status.phase == Ready` with a 5-minute timeout. If it times out:

1. Check CRDs are installed: `kubectl get crd sandboxclaims.agents.x-k8s.io`
2. Inspect the claim: `kubectl -n opencode-scale describe sandboxclaim <name>`
3. Verify warm pool: `kubectl -n opencode-scale get sandboxwarmpool`

### Pool exhausted (all slots in use)

When all `maxSize` slots are occupied, new requests are queued with SSE position updates. To increase capacity:

- Raise `POOL_MAX_SIZE` (or `pool.maxSize` in config)
- For K8s: also increase the warm pool `maxSize`

### 401 Unauthorized

API key authentication is enabled. Either:

- Add your key to requests: `-H "X-API-Key: your-key"`
- Disable auth by leaving `apiKeys` empty in config

### Request body too large (413)

The default limit is 1 MB. Increase via `MAX_BODY_BYTES` env var or `router.maxBodyBytes` in config.

### Pods in CrashLoopBackOff

Check component logs:

```bash
kubectl -n opencode-scale logs -l app=opencode-scale-router
kubectl -n opencode-scale logs -l app=opencode-scale-worker
kubectl -n opencode-scale logs -l app=opencode-scale-controller
```

Common causes: missing config file, Temporal unreachable, insufficient K8s RBAC for SandboxClaim CRDs.

## Production Recommendations

- **Pool sizing**: Set `minReady` to expected baseline concurrency, `maxSize` to peak. Monitor `opencode_scale_allocation_latency` p99 — if high, increase `minReady`.
- **Idle timeout**: 10-15 minutes balances resource reclamation with re-allocation churn for bursty workloads.
- **Temporal**: Use a persistent store (PostgreSQL/MySQL) in production. The default dev server uses in-memory storage.
- **Secrets**: Set `LITELLM_API_KEY` and `API_KEYS` via environment variables or K8s Secrets, never in YAML config files.
- **Router HA**: Run multiple router replicas behind Kong with consistent hashing by session ID for sticky routing.
- **Resource limits**: Set CPU/memory limits on sandbox pods via the `SandboxTemplate` CRD to prevent resource starvation.
- **Namespace isolation**: Use a dedicated namespace for sandbox resources to simplify RBAC and quota enforcement.
