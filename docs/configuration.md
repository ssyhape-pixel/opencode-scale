# Configuration

opencode-scale loads configuration from a YAML file with environment variable overrides. The load order is:

1. Built-in defaults
2. YAML file values (merged on top of defaults)
3. Environment variable overrides (highest precedence)

All three binaries (`router`, `controller`, `worker`) share the same config format and accept a `-config` flag (default: `config.yaml`).

## Config File Format

```yaml
pool:
  minReady: 3
  maxSize: 50
  idleTimeout: 10m
  gcInterval: 30s
  warmPoolName: default
  namespace: opencode-scale
  mode: local              # "local" or "k8s"
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

## Environment Variable Overrides

Every field that supports an env override is listed below. Env vars take precedence over YAML values.

| Env Variable | Config Path | Type | Default | Description |
|---|---|---|---|---|
| `POOL_MIN_READY` | `pool.minReady` | int | `3` | Minimum pre-warmed sandbox instances |
| `POOL_MAX_SIZE` | `pool.maxSize` | int | `50` | Maximum concurrent sandbox allocations |
| `POOL_IDLE_TIMEOUT` | `pool.idleTimeout` | duration | `10m` | Time before an idle sandbox is reclaimed |
| `POOL_NAMESPACE` | `pool.namespace` | string | `opencode-scale` | Kubernetes namespace for sandbox resources |
| `POOL_MODE` | `pool.mode` | string | `local` | Provider mode: `local` (mock) or `k8s` (real sandboxes) |
| `MOCK_OPENCODE_TARGET` | `pool.mockTarget` | string | `localhost:4096` | Target address for mock provider (local mode only) |
| `ROUTER_LISTEN_ADDR` | `router.listenAddr` | string | `:8080` | Router HTTP listen address |
| `API_KEYS` | `router.apiKeys` | []string | _(empty)_ | Comma-separated API keys. Empty = auth disabled |
| `MAX_BODY_BYTES` | `router.maxBodyBytes` | int64 | `1048576` | Max request body size in bytes (1 MB) |
| `TEMPORAL_HOST_PORT` | `temporal.hostPort` | string | `localhost:7233` | Temporal frontend service gRPC address |
| `TEMPORAL_NAMESPACE` | `temporal.namespace` | string | `opencode-scale` | Temporal namespace for workflows |
| `TEMPORAL_TASK_QUEUE` | `temporal.taskQueue` | string | `coding-tasks` | Temporal task queue name |
| `OTEL_ENDPOINT` | `telemetry.otelEndpoint` | string | `localhost:4317` | OTel Collector gRPC endpoint |
| `PROMETHEUS_PORT` | `telemetry.prometheusPort` | int | `9090` | Port for the Prometheus `/metrics` HTTP server |
| `LITELLM_ENDPOINT` | `litellm.endpoint` | string | `http://localhost:4000` | LiteLLM proxy base URL |
| `LITELLM_API_KEY` | `litellm.apiKey` | string | _(empty)_ | API key for LiteLLM proxy |

Duration values use Go duration syntax: `30s`, `5m`, `1h`.

### Archival Environment Variables (Temporal Server)

These variables configure the Temporal server's S3/MinIO archival. They are set on the Temporal container, not on application binaries.

| Env Variable | Default | Description |
|---|---|---|
| `ARCHIVAL_ENABLED` | `false` | Enable workflow history archival to S3 |
| `ARCHIVAL_S3_REGION` | `us-east-1` | S3 region |
| `ARCHIVAL_S3_ENDPOINT` | `http://minio:9000` | S3 endpoint URL (set for MinIO; leave empty for AWS S3) |
| `ARCHIVAL_S3_BUCKET` | `temporal-archival` | S3 bucket name for archived data |
| `ARCHIVAL_S3_FORCE_PATH_STYLE` | `true` | Use S3 path-style addressing (required for MinIO) |
| `AWS_ACCESS_KEY_ID` | `minioadmin` | S3 access key |
| `AWS_SECRET_ACCESS_KEY` | `minioadmin` | S3 secret key |

## Pool Configuration

Controls sandbox allocation, warm pool sizing, and garbage collection.

| Field | Type | Default | Description |
|---|---|---|---|
| `minReady` | int | `3` | Minimum warm instances kept pre-provisioned. The `StartWarmPool` goroutine maintains this many idle sandboxes ready to claim. Only active in `k8s` mode. |
| `maxSize` | int | `50` | Hard cap on total concurrent allocations. When reached, new requests are queued with SSE position updates. Must be >= 1 and >= `minReady`. |
| `idleTimeout` | duration | `10m` | Duration of inactivity before an active sandbox is eligible for GC. Reset on every heartbeat. Does not affect warm (idle) sandboxes. |
| `gcInterval` | duration | `30s` | How often the Pool Manager scans for idle allocations. Also used as the warm pool replenishment interval. |
| `warmPoolName` | string | `default` | Name of the `SandboxWarmPool` CRD to claim from (K8s mode). |
| `namespace` | string | `opencode-scale` | Kubernetes namespace where `SandboxClaim` and `SandboxWarmPool` resources are managed. |
| `mode` | string | `local` | Provider mode. `local` uses `MockSandboxProvider` (points to `mockTarget`). `k8s` uses `K8sSandboxProvider` (creates SandboxClaim CRDs). |
| `mockTarget` | string | `localhost:4096` | Target address for the mock provider. Only used when `mode=local`. |

Validation rules:
- `minReady` must be >= 0
- `maxSize` must be >= 1
- `minReady` must be <= `maxSize`

## Router Configuration

Controls the HTTP server, session affinity, authentication, and request limits.

| Field | Type | Default | Description |
|---|---|---|---|
| `listenAddr` | string | `:8080` | Address the router binds to. Required. |
| `sessionHeader` | string | `X-Session-ID` | HTTP request header checked for session ID. |
| `sessionCookie` | string | `session_id` | Cookie name checked for session ID. |
| `sessionQuery` | string | `session_id` | Query parameter checked for session ID. |
| `apiKeys` | []string | _(empty)_ | List of valid API keys. When empty, authentication is disabled. Keys can be provided as comma-separated values via `API_KEYS` env var. |
| `maxBodyBytes` | int64 | `1048576` | Maximum request body size in bytes. Uses `http.MaxBytesReader`. Set to `0` to disable. |

Session ID extraction order: header, then cookie, then query param. The first non-empty value wins. If no session ID is found, a new sandbox is allocated.

API key authentication supports two formats:
- `Authorization: Bearer {key}`
- `X-API-Key: {key}`

The `/health` endpoint always bypasses authentication.

## Temporal Configuration

Controls the connection to Temporal server and workflow dispatch.

| Field | Type | Default | Description |
|---|---|---|---|
| `hostPort` | string | `localhost:7233` | Temporal frontend service gRPC address. Required. |
| `namespace` | string | `opencode-scale` | Temporal namespace. Must be created on the Temporal server before starting workflows. Required. |
| `taskQueue` | string | `coding-tasks` | Task queue name. Workers listen on this queue; the router dispatches workflows to it. Required. |

The router connects to Temporal on startup. If the connection fails, the task API (`/api/v1/tasks`) is disabled but the router continues to serve health and proxy endpoints.

The worker requires a working Temporal connection and will exit on failure.

## Telemetry Configuration

Controls tracing, metrics, and logging.

| Field | Type | Default | Description |
|---|---|---|---|
| `serviceName` | string | `opencode-scale` | Service name reported in OTel traces and metrics. The worker appends `-worker` and controller appends `-controller` automatically. |
| `otelEndpoint` | string | `localhost:4317` | OTel Collector gRPC endpoint for trace export. |
| `prometheusPort` | int | `9090` | Port for the `/metrics` HTTP endpoint. Set to `0` to disable. |
| `logLevel` | string | `info` | Log level: `debug`, `info`, `warn`, `error`. |

Exported metrics:

| Metric | Type | Source | Description |
|---|---|---|---|
| `opencode_scale_pool_size` | Int64Gauge | Pool | Total pool capacity |
| `opencode_scale_allocated_count` | Int64Gauge | Pool | Currently allocated sandboxes |
| `opencode_scale_wait_queue_length` | Int64Gauge | Pool | Requests waiting for a sandbox |
| `opencode_scale_allocation_latency` | Float64Histogram (s) | Pool | Time to allocate a sandbox |
| `opencode_scale_task_duration` | Float64Histogram (s) | Workflow | Task execution duration |
| `opencode_scale_task_status` | Int64Counter | Workflow | Task completions by status |
| `opencode_scale_llm_tokens_total` | Int64Counter | Workflow | Total LLM tokens consumed |
| `opencode_scale_sandbox_claims_total` | Int64Counter | Controller | SandboxClaim events by phase |
| `opencode_scale_gc_deletions_total` | Int64Counter | Controller | GC-deleted claims |

## LiteLLM Configuration

Controls the connection to the LiteLLM multi-provider LLM proxy.

| Field | Type | Default | Description |
|---|---|---|---|
| `endpoint` | string | `http://localhost:4000` | LiteLLM proxy base URL. OpenCode Server inside sandboxes routes LLM requests through this proxy. |
| `apiKey` | string | _(empty)_ | API key for authenticating to the LiteLLM proxy. Use `LITELLM_API_KEY` env var to avoid storing secrets in config files. |

## Production Recommendations

**Pool sizing**: Set `minReady` to your expected baseline concurrency and `maxSize` to your peak. Monitor `opencode_scale_allocation_latency` — if p99 is high, increase `minReady`.

**Idle timeout**: In production, 10-15 minutes is a good balance between reclaiming resources and avoiding re-allocation churn for bursty workloads.

**Temporal**: Run Temporal with a persistent store (PostgreSQL or MySQL). The default dev server uses in-memory storage and loses state on restart.

**Secrets**: Always set `LITELLM_API_KEY` and `API_KEYS` via environment variables or Kubernetes Secrets, never in the YAML config file.

**Replicas**: Run multiple router replicas behind Kong for HA. Note that the `AllocationCache` is per-process, so session affinity at the Kong level (consistent hashing by session ID) is needed to route returning sessions to the same router instance.

**Resource limits**: Set CPU/memory limits on sandbox pods via the `SandboxTemplate` CRD. Without limits, a single runaway sandbox can starve the node.

**Observability**: Always enable `otelEndpoint` and `prometheusPort` in production. The Grafana dashboards at `deploy/base/otel/grafana/` are pre-configured for the exported metrics.

**Namespace isolation**: Use a dedicated namespace (`pool.namespace`) for sandbox resources to simplify RBAC and resource quota enforcement.
