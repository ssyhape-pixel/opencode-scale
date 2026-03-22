# 快速上手

[English](getting-started.md) | [中文](getting-started.zh-CN.md)

## 前置条件

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.25+ | 构建二进制 |
| Docker | 24+ | 容器镜像和本地开发 |
| kubectl | 1.28+ | 集群管理（K8s 部署） |
| Helm | 3.13+ | Chart 部署（K8s 部署） |

可选：

- `golangci-lint` — 用于 `make lint`
- `jq` — 用于解析 JSON 响应
- `Kind` 0.20+ — 用于本地 K8s 集群

## 快速开始

```bash
git clone https://github.com/opencode-scale/opencode-scale.git
cd opencode-scale

# 安装依赖
make deps

# 构建全部 5 个二进制
make build
# 产出：bin/router, bin/controller, bin/worker, bin/mock-opencode, bin/mock-llm-api

# 运行测试
make test
```

## 本地开发（Docker Compose）

最快的启动方式，无需 K8s 集群。

### 启动所有服务

```bash
make compose-up
```

启动 5 个服务：

| 服务 | 端口 | 用途 |
|------|------|------|
| Temporal | 7233 | 工作流编排 |
| Temporal UI | 8233 | 工作流查看 Web 界面 |
| mock-opencode | 4096 | 模拟 OpenCode Server |
| Router | 8080, 9090 | HTTP 网关 + Prometheus 指标 |
| Worker | — | Temporal Activity 执行器 |

### 验证运行状态

```bash
curl -s http://localhost:8080/health | jq .
```

预期输出：

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

### 导入测试数据

```bash
make seed
```

提交示例编码任务（斐波那契、受控输出、Schema 校验）。

### 查看日志

```bash
make compose-logs
```

### 停止

```bash
make compose-down
```

## 不用 Docker 运行

可以用 mock provider 单独运行 router，无需 Temporal、K8s 或 Docker。

1. 创建最小配置：

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

2. 启动 mock 服务和 router：

```bash
./bin/mock-opencode &
./bin/router -config config-local.yaml
```

Router 在 `:8080` 启动。如果 Temporal 连不上，Task API 会被禁用，但 health 端点和代理依然可用。

## API 参考

### 认证

通过 `API_KEYS` 环境变量（逗号分隔）或 YAML 中的 `router.apiKeys` 配置。

```bash
# Header 方式
curl -H "X-API-Key: your-key" http://localhost:8080/api/v1/tasks

# Bearer Token 方式
curl -H "Authorization: Bearer your-key" http://localhost:8080/api/v1/tasks
```

`apiKeys` 为空（默认）时，认证被禁用。

`/health` 端点始终跳过认证。

### 创建任务

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "用归并排序实现一个整数排序函数",
    "timeout": 300,
    "userId": "test-user"
  }' | jq .
```

响应：

```json
{
  "taskId": "d290f1ee-6c54-4b01-90e6-d701748f0851",
  "status": "pending",
  "createdAt": "2026-01-15T10:30:00Z"
}
```

**请求字段：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `prompt` | string | 是 | 编码任务描述 |
| `outputSchema` | string | 否 | 用于校验输出的 JSON Schema |
| `timeout` | int | 否 | 超时秒数（默认 1800，最大 7200） |
| `userId` | string | 否 | 用户标识符 |
| `metadata` | object | 否 | 自定义键值对元数据 |

### 创建带 Schema 校验的任务

提供 `outputSchema` 后，系统会校验 LLM 输出是否符合 Schema。校验失败会自动带反馈重新 prompt（最多 3 轮）。

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "返回一个 JSON 对象，包含 name（字符串）和 age（整数）字段",
    "timeout": 120,
    "outputSchema": "{\"type\":\"object\",\"required\":[\"name\",\"age\"],\"properties\":{\"name\":{\"type\":\"string\"},\"age\":{\"type\":\"integer\"}}}"
  }' | jq .
```

### 查询任务状态

```bash
curl -s http://localhost:8080/api/v1/tasks/{taskId} | jq .
```

完成时的响应：

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

**任务状态值：**

| 状态 | 说明 |
|------|------|
| `pending` | 工作流已启动，等待 Worker 拾取 |
| `running` | Worker 正在执行任务 |
| `completed` | 任务成功完成 |
| `failed` | 任务遇到错误 |
| `timeout` | 任务超时 |

### 实时推送任务进度（SSE）

通过 Server-Sent Events 实时推送任务进度，端点每 2 秒轮询 Temporal。

```bash
curl -N http://localhost:8080/api/v1/tasks/{taskId}/stream
```

运行中的输出：

```
event: status
data: {"status":"running"}

event: status
data: {"status":"running"}
```

完成时的输出：

```
event: result
data: {"status":"completed","sessionId":"sess-123","result":"...","duration":45.2,"tokensUsed":1500}
```

任务到达终态（completed、failed 或 timeout）时流自动关闭。

### 健康检查

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

### 会话代理（直接访问）

直接与沙箱内的 OpenCode Server 交互，发送请求到 `/api/v1/*` 并携带会话标识：

```bash
# 首次请求 — 分配沙箱，返回会话 ID
curl -v http://localhost:8080/api/v1/sessions \
  -H "X-User-ID: test-user"
# 响应包含：X-Session-ID 头 + session_id Cookie

# 后续请求 — 路由到同一沙箱
curl http://localhost:8080/api/v1/sessions/{sessionId}/messages \
  -H "X-Session-ID: {sessionId}" \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Hello"}'
```

会话 ID 提取优先级：`X-Session-ID` 请求头 > `session_id` Cookie > `session_id` 查询参数。

## 配置

配置从 YAML 文件加载，支持环境变量覆盖。加载顺序：

1. 内置默认值
2. YAML 文件
3. 环境变量（最高优先级）

所有二进制都接受 `-config path/to/config.yaml`（默认：`config.yaml`）。

### 完整配置示例

```yaml
pool:
  minReady: 3              # 维持的 warm 沙箱实例数
  maxSize: 50              # 最大并发分配数
  idleTimeout: 10m         # 空闲回收阈值
  gcInterval: 30s          # GC 扫描频率
  warmPoolName: default    # SandboxWarmPool CRD 名称
  namespace: opencode-scale
  mode: local              # "local"（模拟）或 "k8s"（真实沙箱）
  mockTarget: localhost:4096

router:
  listenAddr: ":8080"
  sessionHeader: "X-Session-ID"
  sessionCookie: "session_id"
  sessionQuery: "session_id"
  apiKeys: []              # 空=禁用认证
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

### 环境变量

| 变量 | 配置路径 | 默认值 | 说明 |
|------|---------|--------|------|
| `POOL_MIN_READY` | `pool.minReady` | `3` | warm 沙箱数 |
| `POOL_MAX_SIZE` | `pool.maxSize` | `50` | 最大并发沙箱数 |
| `POOL_IDLE_TIMEOUT` | `pool.idleTimeout` | `10m` | 空闲回收阈值 |
| `POOL_NAMESPACE` | `pool.namespace` | `opencode-scale` | K8s 命名空间 |
| `POOL_MODE` | `pool.mode` | `local` | `local` 或 `k8s` |
| `MOCK_OPENCODE_TARGET` | `pool.mockTarget` | `localhost:4096` | mock provider 目标 |
| `ROUTER_LISTEN_ADDR` | `router.listenAddr` | `:8080` | HTTP 监听地址 |
| `API_KEYS` | `router.apiKeys` | _(空)_ | 逗号分隔的 API Key |
| `MAX_BODY_BYTES` | `router.maxBodyBytes` | `1048576` | 最大请求体大小 |
| `TEMPORAL_HOST_PORT` | `temporal.hostPort` | `localhost:7233` | Temporal gRPC 地址 |
| `TEMPORAL_NAMESPACE` | `temporal.namespace` | `opencode-scale` | Temporal 命名空间 |
| `TEMPORAL_TASK_QUEUE` | `temporal.taskQueue` | `coding-tasks` | Temporal 任务队列 |
| `OTEL_ENDPOINT` | `telemetry.otelEndpoint` | `localhost:4317` | OTel Collector 端点 |
| `PROMETHEUS_PORT` | `telemetry.prometheusPort` | `9090` | Prometheus 指标端口 |
| `LITELLM_ENDPOINT` | `litellm.endpoint` | `http://localhost:4000` | LiteLLM 代理 URL |
| `LITELLM_API_KEY` | `litellm.apiKey` | _(空)_ | LiteLLM API Key |

Duration 值使用 Go 语法：`30s`、`5m`、`1h`。

## K8s 部署

### Helm 部署

```bash
helm install opencode-scale ./charts/opencode-scale \
  --namespace opencode-scale \
  --create-namespace \
  --set config.pool.mode=k8s \
  --set config.pool.minReady=3 \
  --set config.pool.maxSize=50
```

Chart 部署 3 组组件：Router（2 副本）、Worker（2 副本）、Controller（1 副本）。

### Kustomize 部署

```bash
# 开发环境（资源缩减，各 1 副本）
make deploy-dev

# 生产环境
make deploy-prod
```

### 验证部署

```bash
kubectl -n opencode-scale get pods
kubectl -n opencode-scale get sandboxclaims
kubectl -n opencode-scale get sandboxwarmpool
```

## 归档（S3/MinIO）

Temporal 工作流执行会在 PostgreSQL 中产生历史数据。归档功能会在 Temporal 保留策略清理前，将已完成的工作流历史移到 S3 兼容对象存储中。

### MinIO 快速启动

一条命令即可启动 MinIO 并配置归档：

```bash
make compose-archival
```

此命令会：
1. 启动 MinIO（S3 兼容存储），端口 9000（API）和 9001（控制台）
2. 创建 `temporal-archival` 桶
3. 挂载启用归档的 Temporal 配置模板
4. 将 `default` 命名空间配置为 72h 保留期并启用归档

### 验证归档是否生效

```bash
# 检查命名空间配置
docker compose exec -T temporal tctl --address $(docker compose exec -T temporal hostname -i):7233 \
  namespace describe --namespace default
# 查看：HistoryArchivalState: Enabled, VisibilityArchivalState: Enabled

# 提交任务并等待完成
make seed

# 打开 MinIO 控制台 http://localhost:9001 (minioadmin/minioadmin)
# 检查 temporal-archival 桶下是否有 history/ 和 visibility/ 目录
```

### 使用 AWS S3 替代 MinIO

生产环境中，使用真实的 S3 桶：

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

使用 AWS S3 时无需 `--profile archival`（不需要启动 MinIO）。

### 默认行为（无归档）

运行 `docker compose up -d` 或 `make compose-up` 行为与之前完全一致，归档默认禁用（`ARCHIVAL_ENABLED=false`）。

## 限流压测

使用 mock LLM API 测试 LiteLLM 密钥轮换和限流：

```bash
# 启动完整栈（含限流）
make compose-ratelimit
```

额外启动的服务：

| 服务 | 端口 | 用途 |
|------|------|------|
| mock-llm-api | 4099 | 模拟 OpenAI API（带 RPM/TPM 限制） |
| litellm | 4000 | 多密钥 LLM 代理（自动轮换） |

```bash
# 运行压测
make bench

# 查看限流状态
curl http://localhost:4099/debug/rate-limits | jq .
```

## 可观测性

### Prometheus 指标

所有组件在 `:9090/metrics` 暴露指标。

| 指标 | 类型 | 说明 |
|------|------|------|
| `opencode_scale_pool_size` | Gauge | 池总容量 |
| `opencode_scale_allocated_count` | Gauge | 当前已分配沙箱数 |
| `opencode_scale_wait_queue_length` | Gauge | 排队请求数 |
| `opencode_scale_allocation_latency` | Histogram | 沙箱分配耗时（秒） |
| `opencode_scale_task_duration` | Histogram | 任务执行耗时（秒） |
| `opencode_scale_task_status` | Counter | 按状态计数的任务完成数 |
| `opencode_scale_llm_tokens_total` | Counter | LLM Token 总消耗 |

### Grafana 仪表板

预配置仪表板在 `deploy/base/otel/grafana/dashboards/opencode-scale.json`。

```bash
# K8s
kubectl -n opencode-scale port-forward svc/grafana 3000:3000

# 打开 http://localhost:3000
```

### Temporal UI

查看工作流执行、Activity 历史和重试记录。

```bash
# Docker Compose：http://localhost:8233
# K8s：kubectl -n opencode-scale port-forward svc/temporal-frontend 8233:7233
```

### 审计日志

每个 HTTP 请求以结构化 JSON 输出到 stdout：

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

## Makefile 参考

| 目标 | 说明 |
|------|------|
| `make build` | 构建全部 5 个二进制到 `bin/` |
| `make test` | 运行测试（race 检测 + 覆盖率） |
| `make test-short` | 快速测试 |
| `make test-e2e` | 端到端测试（启动 Docker Compose） |
| `make lint` | 运行 golangci-lint |
| `make fmt` | 格式化代码 |
| `make vet` | 运行 `go vet` |
| `make coverage` | 生成 HTML 覆盖率报告 |
| `make docker-build` | 构建所有 Docker 镜像 |
| `make docker-push` | 推送镜像到 registry |
| `make compose-up` | 启动本地开发环境 |
| `make compose-down` | 停止并清理 |
| `make compose-logs` | 跟踪服务日志 |
| `make compose-archival` | 启动 S3/MinIO 归档环境 |
| `make compose-ratelimit` | 启动限流测试环境 |
| `make bench` | 运行压测 |
| `make deploy-dev` | 部署到 K8s 开发 overlay |
| `make deploy-prod` | 部署到 K8s 生产 overlay |
| `make seed` | 提交示例任务 |
| `make clean` | 清理二进制和覆盖率文件 |

## 故障排查

### Router 启动失败："reading config file"

Router 需要 `-config` 指定的配置文件（默认：`config.yaml`）。确保文件存在且是有效的 YAML。

### Task API 返回 404

Task API 仅在 Temporal 连接成功时注册。检查 Router 日志：

```
WARN temporal client unavailable, task API disabled
```

确保 Temporal 正在运行且 `TEMPORAL_HOST_PORT` 正确。

### 沙箱分配超时（5 分钟）

`K8sSandboxProvider` 以 5 分钟超时轮询 `status.phase == Ready`。如果超时：

1. 检查 CRD 是否安装：`kubectl get crd sandboxclaims.agents.x-k8s.io`
2. 检查 Claim 状态：`kubectl -n opencode-scale describe sandboxclaim <name>`
3. 检查 warm pool：`kubectl -n opencode-scale get sandboxwarmpool`

### 池满（所有位置已占用）

所有 `maxSize` 位置被占用时，新请求会排队并通过 SSE 推送位置。增加容量：

- 提高 `POOL_MAX_SIZE`（或配置中的 `pool.maxSize`）
- K8s 环境：同时增大 warm pool 的 `maxSize`

### 401 Unauthorized

API Key 认证已启用。两种解决方式：

- 在请求中添加 Key：`-H "X-API-Key: your-key"`
- 通过将 `apiKeys` 留空来禁用认证

### 请求体过大（413）

默认限制 1 MB。通过 `MAX_BODY_BYTES` 环境变量或 `router.maxBodyBytes` 配置调整。

### Pod 处于 CrashLoopBackOff

检查组件日志：

```bash
kubectl -n opencode-scale logs -l app=opencode-scale-router
kubectl -n opencode-scale logs -l app=opencode-scale-worker
kubectl -n opencode-scale logs -l app=opencode-scale-controller
```

常见原因：配置文件缺失、Temporal 不可达、K8s RBAC 对 SandboxClaim CRD 权限不足。

## 生产建议

- **池大小调优**：将 `minReady` 设为预期基线并发数，`maxSize` 设为峰值。监控 `opencode_scale_allocation_latency` p99 — 如果偏高，增大 `minReady`。
- **空闲超时**：10-15 分钟能平衡资源回收和突发负载下避免重新分配的开销。
- **Temporal**：生产环境使用持久化存储（PostgreSQL/MySQL）。默认开发服务用内存存储，重启会丢失状态。
- **密钥管理**：通过环境变量或 K8s Secret 设置 `LITELLM_API_KEY` 和 `API_KEYS`，不要写在 YAML 配置文件中。
- **Router 高可用**：在 Kong 后面运行多个 Router 副本，用 Session ID 一致性哈希做粘性路由。
- **资源限制**：通过 `SandboxTemplate` CRD 给沙箱 Pod 设置 CPU/内存限制，防止单个沙箱耗尽节点资源。
- **命名空间隔离**：使用独立命名空间管理沙箱资源，简化 RBAC 和配额管理。
