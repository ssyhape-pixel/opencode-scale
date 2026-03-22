# opencode-scale — 生产化 Roadmap

> 诚实评估：从当前状态到生产级别还需要什么。
> 最后更新：2026-03-18

## 状态图例

- [x] 已完成，代码已提交
- [~] 代码写了但未提交（工作区里躺着）
- [ ] 未开始

---

## Phase 0: 核心架构

全部完成，无遗留。

- [x] Go 1.25 项目骨架 + 模块布局
- [x] Pool Manager — 分配 / 释放 / 心跳
- [x] K8s SandboxProvider — SandboxClaim CRD 创建 + 等待 Ready
- [x] Mock SandboxProvider — 本地开发用
- [x] OpenCode HTTP Client — SSE 流式解析、多行 data 支持
- [x] Temporal Workflow — 5 个 Activity 编排（分配→会话→执行→验证→释放）
- [x] Task API — POST 创建 / GET 查询状态
- [x] Session Affinity Router — Header/Cookie/Query 提取 + 反向代理
- [x] Wait Queue — 池满时排队 + SSE 位置推送
- [x] 配置系统 — YAML 文件 + 16 个环境变量覆盖 + 校验

## Phase 1: 测试

全部完成。

- [x] 单元测试 — 99 个测试，≥80% 覆盖率
- [x] E2E 测试 — 7 个端到端场景（`test/e2e/`，build tag `e2e`）
- [x] Race 检测 — `go test -race ./...` 全通过
- [x] Mock 服务器 — mock-opencode（SSE 模拟）、mock-llm-api（限流模拟）

## Phase 2: 可观测性

全部完成。

- [x] OTel Tracing — 每个 Activity 独立 Span + 属性标注
- [x] Prometheus Metrics — pool/workflow/controller 三组指标
- [x] Grafana Dashboard — 预配置 JSON（`deploy/base/otel/grafana/`）
- [x] 结构化日志 — slog 全局使用

## Phase 3: 生产加固（5 个高优缺口修复）

刚刚完成，已提交。

- [x] Activity Retry Policies — 5 个 Activity 各自配置重试策略
- [x] GC 调度 — `StartGCLoop` 定时回收 idle 沙箱
- [x] Warm Pool 预分配 — `StartWarmPool` 维持 MinReady 个待命沙箱
- [x] Token 计数 — SSE usage 字段 + X-Usage-Total-Tokens Header 备选
- [x] SSE Stream 端点 — `GET /tasks/{id}/stream` 实时推送任务状态
- [x] Schema 校验重试循环 — 验证失败→重新 prompt→再验证（最多 3 轮）
- [x] 优雅关停 — 所有 3 个二进制均处理 SIGINT/SIGTERM
- [x] Timeout 上限 — 最大 2 小时，workflow 额外 10 分钟 buffer

## Phase 4: 部署与 CI/CD

大部分完成，有零散未提交。

- [x] Dockerfile — 多阶段构建，5 个二进制
- [x] Makefile — 完整构建系统（build/test/lint/docker/e2e）
- [x] Helm Chart v0.1.0 — Router/Controller/Worker 三组 Deployment
- [x] Kustomize — base + dev/prod overlays
- [x] Docker Compose — 本地开发一键启动
- [x] GitHub Actions CI — lint + test + build + docker push
- [x] **未提交的零散文件** — 6 次提交全部收拾完毕
- [ ] Helm Chart 自动发布（GitHub Pages / OCI registry）
- [x] CI 安全扫描（gosec + govulncheck + trivy）

### 未提交文件清单

以下文件在工作区但从未 commit：

**已修改（12 个）**：
```
Dockerfile, Makefile, cmd/controller/main.go, cmd/worker/main.go,
go.mod, hack/seed-data.sh, hack/setup-local.sh,
internal/config/config.go, internal/controller/reconciler.go,
internal/router/handler.go, internal/router/handler_test.go,
internal/router/proxy.go
```

**新增未跟踪（10 个）**：
```
cmd/mock-llm-api/main.go, docker-compose.yaml,
docker-compose.ratelimit.yaml, hack/bench.sh,
hack/config-local.yaml, hack/litellm-config.yaml,
internal/controller/metrics.go, internal/controller/reconciler_test.go,
internal/pool/mock_provider.go, test/e2e/e2e_test.go
```

**二进制垃圾（2 个，应 gitignore）**：
```
mock-llm-api, mock-opencode
```

## Phase 5: 安全

这是最大的缺口。

- [x] **API Key 认证** — APIKeyAuth 中间件，Bearer/X-API-Key，空配置=禁用
- [x] **请求体大小限制** — MaxBodySize 中间件，默认 1MB
- [ ] **Router ↔ Sandbox TLS** — K8s 内部通信加密（mTLS 或 service mesh）
- [x] **审计日志** — AuditLog 中间件，记录 method/path/status/duration/user

## Phase 6: 可扩展性

当前单副本够用，多副本部署前需要解决。

- [ ] **Controller Leader Election** — 多副本防止重复协调
- [ ] **分布式限流** — Redis-backed，多 router 实例共享状态
- [ ] **Circuit Breaker** — 沙箱后端故障自动熔断 + 恢复
- [ ] **Per-User Quota** — 按用户限制并发任务数 / token 消耗

## Phase 7: 运维成熟度

生产跑起来后逐步完善。

- [ ] **Workflow Versioning** — 处理 workflow 定义变更，兼容在飞任务
- [ ] **Runbook** — 事故响应流程文档
- [ ] **性能调优指南** — 池大小、超时、worker 数量推荐值
- [ ] **成本归因** — 按用户追踪 token 消耗 + 沙箱时长

---

## 优先级排序

### 现在就该做（阻塞上线）

| # | 事项 | 预估工作量 | 理由 |
|---|------|-----------|------|
| ~~1~~ | ~~提交未 commit 的文件~~ | ~~DONE~~ | ~~6 次提交，工作区已干净~~ |
| ~~2~~ | ~~API Key 认证中间件~~ | ~~DONE~~ | ~~Bearer/X-API-Key，YAML 或 API_KEYS 环境变量~~ |
| ~~3~~ | ~~请求体大小限制~~ | ~~DONE~~ | ~~MaxBytesReader，默认 1MB~~ |

### 上线前应该做

| # | 事项 | 预估工作量 | 理由 |
|---|------|-----------|------|
| ~~4~~ | ~~.gitignore 排除二进制~~ | ~~DONE~~ | ~~已添加到 .gitignore~~ |
| ~~5~~ | ~~CI 安全扫描~~ | ~~DONE~~ | ~~gosec + govulncheck + trivy~~ |
| ~~6~~ | ~~审计日志~~ | ~~DONE~~ | ~~AuditLog 中间件，记录每个请求~~ |

### 多副本部署前

| # | 事项 | 预估工作量 | 理由 |
|---|------|-----------|------|
| 7 | Controller Leader Election | 2-4 h | 多副本会重复协调 |
| 8 | 分布式限流 | 4-8 h | 多 router 各自限流等于没限 |

### 可以后做

| # | 事项 | 预估工作量 | 理由 |
|---|------|-----------|------|
| 9 | Circuit Breaker | 2-4 h | 单沙箱故障不影响全局 |
| 10 | Per-User Quota | 3-5 h | 初期用户少，手动管理够用 |
| 11 | Workflow Versioning | 4-6 h | 首次变更定义时才需要 |
| 12 | Helm Chart 自动发布 | 2-3 h | 手动 helm push 能用 |
| 13 | 成本归因 | 3-5 h | 商业化阶段再做 |
| 14 | Runbook + 调优指南 | 4-6 h | 跑一段时间积累经验再写更实际 |

---

## 诚实总结

**核心功能 100% 完成。** Workflow 编排、池管理、SSE 流、Schema 校验、可观测性全部到位。

**最大风险是那 22 个未提交文件。** 包括 Dockerfile、Makefile、docker-compose、mock 服务器、E2E 测试、controller metrics——这些是 Phase 2-4 的产出，但从没 commit 过。一次 `git clean` 就全没了。

**上生产的硬性阻塞只有 3 件事：** 提交代码、加认证、限制请求体大小。其余都是锦上添花。
