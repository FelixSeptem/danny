# HTTP API 与 Web 工作台边界（里程碑 10）

本文档记录了 `http-api-workbench` 变更的路由契约、安全模型、投影字段白名单、A2UI 版本策略和验证证据。它补充了 `docs/agent-ui-protocols.md` 中里程碑 9 的兼容性说明。

## 装配后的服务

`cmd/personal-agent/main.go` 校验配置（包括环回（loopback）监听策略），打开、迁移并校正存储，然后对外提供单个装配完成的 `server.Server.Handler()`。该 handler 按路径段分发：

| 前缀 | 归属 | 行为 |
|---|---|---|
| `/agent/` | `AgentUIService`（里程碑 9） | 原样挂载：SSE 执行事件、取消、动态界面动作提交。 |
| `/api/` | `Server` 资源 handler | 只读的 Danny 自有投影，外加一个感知修订版本（revision）的任务更新。 |
| 其余所有路径 | 静态工作台 | 来自 `DANNY_WORKBENCH_DIR`（默认 `web/dist`）的构建产物，带 SPA 回退；当该目录不存在时使用内置占位页。 |

零值依赖会失败关闭（fail closed）。在当前装配中，运行控制器（取消）和动态界面动作执行器被有意留空，因为它们的产品授权与提交门禁尚未接通；相应端点会返回带类型的 `unavailable`（503），而不是在缺少 Danny 授权的情况下执行。只读的运行来源和受限的组件目录（catalog）分别通过 `StorageRunSource`、`SurfaceService` 和 `agentui.DefaultCatalog()` 接通。

## 安全模型

- **仅环回**：`internal/security.ValidateListener` 通过 `config.Config.Validate` 生效。在默认的未认证配置下，非环回地址会在任何套接字打开之前被拒绝。认证、TLS 和非环回部署仍在范围之外。
- **同源守卫**：单个包装器会拒绝每一个改变状态的请求（POST、PUT、PATCH、DELETE），除非该请求携带的 `Origin` 的 scheme 和 host 与请求主机匹配，或携带 `Sec-Fetch-Site: same-origin`。拒绝时返回 HTTP 403 与 `same_origin_required`，且在任何 handler 之前执行，也包括里程碑 9 的取消端点和动态界面动作端点。GET/HEAD/OPTIONS 不需要任何信号。这封堵了针对环回服务的恶意来源和 DNS 重绑定表单提交。
- **不新增写入权限**：唯一新增的变更操作是感知修订版本的任务更新。所有其他变更仍走 Danny 既有的授权、`idempotency_effects` 和提交门禁。

## 路由契约

所有响应均为 JSON，并带有显式的 Danny 自有字段白名单。错误使用 `{"error": <code>, "message": <text>}`；错误码包括 `same_origin_required`、`unavailable`、`not_found`、`malformed`、`task_revision_conflict`、`method_not_allowed` 和 `projection_failed`。

| 方法 | 路径 | 响应 |
|---|---|---|
| GET | `/api/health` | 无依赖的存活检查 |
| GET | `/api/tasks` | 有界的任务列表 |
| GET | `/api/tasks/{id}` | 单个任务的投影 |
| PATCH | `/api/tasks/{id}` | 需要 `expected_revision`；修订版本过期时返回 409 |
| GET | `/api/schedules` | 有界的计划列表 |
| GET | `/api/schedules/{id}` | 单个计划的投影 |
| GET | `/api/schedules/{id}/occurrences` | 有界的执行历史 |
| GET | `/api/deliveries` | 有界的 Webhook 投递列表 |
| GET | `/api/deliveries/{id}` | 单次投递的投影 |
| GET | `/api/notifications` | 有界的通知列表 |
| GET | `/api/knowledge/generations` | 有界的知识世代（generation）列表 |
| GET | `/api/knowledge/artifacts?generation_id=N` | 有界的产物列表 |
| GET | `/api/knowledge/sources` | 有界的来源元数据列表 |
| GET | `/api/knowledge/status` | 已提交的知识世代、产物和来源计数 |
| GET | `/api/runs` | 有界的执行列表 |
| GET | `/api/runs/{id}` | 运行检查器投影 |
| GET | `/api/surfaces/{id}` | 对已存储且经组件目录批准的动态界面进行 A2UI v0.9.1 线格式（wire）渲染 |
| GET | `/agent/threads/{tid}/runs/{rid}/events` | 里程碑 9 的 AG-UI SSE 投影 |
| POST | `/agent/threads/{tid}/runs/{rid}/cancel` | 里程碑 9 的控制边界 |
| POST | `/agent/surfaces/{sid}/actions` | 里程碑 9 的经服务端校验的提交 |

列表由 `storage.ProjectionListLimit`（200）限制条数，并按提交时间戳排序。它们是某一时间点的快照式读取；实时执行状态来自 AG-UI SSE 流，而不是通过轮询这些视图获得。

## 投影字段白名单

每种投影类型都在 `internal/storage/projections.go` 中声明，并使用显式的 JSON 名称。任何投影都不会选取原始 payload 数据块、原生事件 ID、原生检查点标识、effect key、Stele 信封或 Baymax 事件结构。有代表性的白名单如下：

- task：`id, title, description, status, revision, created_at, updated_at`
- schedule：`id, cron, timezone, action_type, instruction, enabled, retry_limit, misfire_policy, backfill_limit, revision, created_at, updated_at`
- delivery：`id, execution_id, occurrence_id, status, attempt_count, next_attempt_at, last_error, created_at, updated_at`（不含 URL 或 payload）
- artifact：`id, generation_id, kind, content_hash, created_at`（不含内容或谱系）
- source：`id, path, content_hash, format, parser_version, provider_mode, untrusted_source, created_at, updated_at`（不含内容或警告）
- execution：`id, task_id, status, revision, mode, plan_version, parent_run_id, correlation_id, created_at, updated_at`（不含摘要值、幂等键、结果或失败数据块）
- run inspector：执行投影，外加方案版本、检查点摘要、上下文投影摘要值以及证据/事件计数。原生标识与原始结果被有意省略。

运行检查器的 `context_projections` 只携带摘要值和一个作用域字符串。它们是 Danny 自有的上下文投影引用，而不是 Stele 记录。

## 工作台客户端与版本策略

`web/` 是一个 React + TypeScript + Vite 单页应用（SPA）。它通过原生 `EventSource` 消费里程碑 9 的 AG-UI SSE 投影（浏览器在重连时会重放 `Last-Event-ID`），并渲染 `/api/` 投影。动态界面从只读的 `/api/surfaces/{id}` 端点获取，该端点通过里程碑 9 的 `RenderA2UI` 适配器（基于 `agentui.DefaultCatalog()`）渲染一个已存储且经服务端批准的 `DynamicSurface`。客户端渲染器是一道只读守卫：它只接受服务端注册的组件（`Card`、`ChoiceChips`、`Text`），对任何其他组件或组件目录以带类型的错误予以拒绝，并将 props 视为不可信文本。动作提交会携带幂等键向既有的经服务端校验的端点发起 POST，并原样展示带类型的接受或拒绝结果。任何由浏览器提供的组件数据都不会被映射为变更操作，客户端也无法注册组件目录条目。

**A2UI 版本声明：仅为 v0.9.1。** `agentui.A2UIWireVersion` 为 `0.9.1`，客户端也固定为同一值。我们并未声明支持 A2UI 1.0。截至 2026-11-13 的验证，`@a2ui/react` 仍为 `0.15.0`/v0.9，且没有 `v1_0` 入口点，因此 `a2ui-1-0-upgrade` 变更处于阻塞（BLOCKED）状态；本工作台不依赖它。

## 验证证据

| 检查项 | 位置 | 结果 |
|---|---|---|
| `go build ./...` | 仓库根目录 | 通过 |
| `go test ./... -count=1` | 仓库根目录 | 所有包通过 |
| 同源守卫（所有改变状态的方法、M9 控制端点） | `internal/server/http_test.go` | 通过 |
| 投影字段白名单、有界列表、带类型的未找到、检查器 | `internal/storage/projections_test.go` | 通过 |
| 修订版本冲突（409）与失败关闭的依赖 | `internal/server/http_test.go` | 通过 |
| 前端 TypeScript 类型检查与生产构建 | `web/` | `tsc --noEmit` 无报错；`vite build` 生成了 `web/dist`（34 个模块） |
| 对变更包运行 `golangci-lint run` | `internal/server`、`internal/storage`、`internal/agentui`、`internal/config`、`cmd/personal-agent` | 0 个问题。全仓库执行 `golangci-lint run ./...` 超出了沙箱超时（EXIT=124，属于已知的本地限制）；改用 `go vet ./...` 和完整的 `go test ./...` 均通过。 |
| `openspec validate --all --strict` | 仓库根目录 | 11/11 通过 |

本文并不声明浏览器端到端场景、实况渲染器认证运行、Docker profile 和发布门禁已完成。这些仍属于里程碑 11 和 12。里程碑 10 交付的是 HTTP API 边界和一个经构建验证的工作台客户端；没有任何声明主张已实现实况浏览器场景或 A2UI 1.0 支持。
