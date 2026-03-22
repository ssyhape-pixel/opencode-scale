# CLAUDE.md

## Project Overview

opencode-scale is a horizontally-scalable orchestration layer for AI coding agents. It manages sandboxed OpenCode Server instances via Temporal workflows, providing HTTP API for task submission, real-time SSE streaming, session proxy, and pool management.

**Module:** `github.com/opencode-scale/opencode-scale`
**Go version:** 1.25
**Key dependencies:** Temporal, Kubernetes (SandboxClaim/SandboxWarmPool CRDs), OpenTelemetry, PostgreSQL

## Architecture

```
Client → Router (HTTP gateway) → Temporal (workflow orchestration) → Worker (activity executor)
                                                                        ↓
                                                              Pool Manager → Sandbox (OpenCode Server)
```

- **Router** — HTTP gateway: task CRUD, SSE streaming, session proxy, health, auth
- **Worker** — Temporal activity executor, runs coding tasks in sandboxes
- **Controller** — K8s controller for SandboxClaim CRD lifecycle (K8s mode only)
- **Pool Manager** — Manages sandbox allocation, warm pool, idle GC
- Two modes: `local` (mock provider) and `k8s` (real sandboxes via CRDs)

## Directory Structure

```
cmd/                    # 5 binaries: router, controller, worker, mock-opencode, mock-llm-api
internal/
  config/               # YAML + env var config loading
  pool/                 # Sandbox pool manager, allocation, GC
  router/               # HTTP handlers, middleware, SSE
  workflow/             # Temporal workflow and activity definitions
  opencode/             # OpenCode Server client
  controller/           # K8s SandboxClaim controller
  schema/               # JSON Schema validation
  telemetry/            # OTel tracer + meter setup
api/v1/                 # API types (request/response structs)
hack/                   # Dev scripts, local config, Temporal config template
deploy/                 # Kustomize base + overlays (dev/prod/archival)
charts/opencode-scale/  # Helm chart
docs/                   # Getting started, architecture, configuration (EN + ZH)
test/e2e/               # End-to-end tests
```

## Common Commands

```bash
make build              # Build all 5 binaries to bin/
make test               # Tests with race detection + coverage
make test-short         # Quick tests
make test-e2e           # E2E: starts docker compose, runs tests, tears down
make compose-up         # Start local dev (Temporal + PG + mock-opencode + router + worker)
make compose-down       # Stop and clean volumes
make compose-archival   # Start with S3/MinIO archival enabled
make compose-ratelimit  # Start with LiteLLM rate-limit testing
make seed               # Submit sample tasks
make bench              # Stress test
make deploy-dev         # kubectl apply -k deploy/overlays/dev
make deploy-prod        # kubectl apply -k deploy/overlays/prod
```

## Code Conventions

### Go Patterns

- **Temporal status comparison:** Use `enumspb.WORKFLOW_EXECUTION_STATUS_*` constants, NOT `.String()`
- **Temporal not-found errors:** `serviceerror.NotFound` from `go.temporal.io/api/serviceerror`
- **WorkflowExecutionInfo:** Import from `go.temporal.io/api/workflow/v1` (not workflowservice)
- **OTel tracer:** `var tracer = otel.Tracer("opencode-scale/<pkg>")`
- **HTTP routing:** Go 1.25 `mux.HandleFunc("POST /path", handler)` syntax
- **Concurrency:** `sync.Map` for proxy cache, `sync.Mutex` for pool heartbeat writes
- **Config mode toggle:** `Pool.Mode` field (`"local"` / `"k8s"`) selects provider
- **SSE parsing:** Accumulate `data:` lines, empty line triggers event processing

### Testing

- Mock Temporal client must implement `client.Client` interface methods directly
- Use `-race` flag (default in `make test`)
- E2E tests use build tag `e2e`

## Configuration

Config loads: built-in defaults → YAML file → environment variables (highest precedence).

All binaries accept `-config path/to/config.yaml`.

Key env vars: `TEMPORAL_HOST_PORT`, `TEMPORAL_NAMESPACE`, `POOL_MODE`, `POOL_MAX_SIZE`, `MOCK_OPENCODE_TARGET`, `API_KEYS`

See `docs/configuration.md` for the full reference.

## Docker Compose Services

| Service | Port | Notes |
|---------|------|-------|
| postgresql | 5432 | Temporal backend |
| temporal | 7233 | auto-setup image, healthcheck via tctl |
| temporal-ui | 8233 | Web UI |
| mock-opencode | 4096 | Simulates OpenCode Server |
| router | 8080, 9090 | HTTP gateway + Prometheus metrics |
| worker | — | No exposed ports |
| minio | 9000, 9001 | S3 storage (archival profile only) |

## Archival

Optional S3/MinIO archival for Temporal workflow histories. Enabled via `ARCHIVAL_ENABLED=true`.

- Config template: `hack/temporal-config-template.yaml` — extends Temporal auto-setup default, adds S3 provider conditionally
- The template **must** stay aligned with the official auto-setup default template structure (archival is a top-level key, NOT nested under global)
- `tctl` in auto-setup binds to `$(hostname -i)`, not `127.0.0.1` — always use `sh -c 'tctl --address $(hostname -i):7233 ...'` when exec-ing into the container
- `--namespace` is a tctl global flag, goes BEFORE subcommands: `tctl --namespace default namespace update ...`

## Gotchas

- Temporal auto-setup sets `BIND_ON_IP=$(hostname -i)` in its entrypoint, overriding config template defaults
- Docker Compose `profiles: [archival]` services need `--profile archival` flag to start
- The Helm chart and Kustomize archival overlay exist but haven't been validated in a real K8s cluster
- `temporalio/auto-setup:latest` includes S3 archiver built-in, no extra plugins needed
