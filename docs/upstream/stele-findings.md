# Stele 上游问题记录

本文记录 Danny 集成 Stele 时发现的、能够稳定复现的问题和改进请求。

## 状态值

- `observed`：已观察到问题，尚未完成上游问题确认。
- `upstream-issue-filed`：已确认属于上游并提交给 Stele 维护者。
- `blocked-upstream`：依赖该契约的 Danny 开发任务必须阻塞。
- `fixed-upstream`：上游已有修复，且已使用修复版本重新验证原始复现。
- `closed`：问题已关闭，不再影响当前路线图。

每条记录都必须包含：准确的依赖版本或 commit、确定性复现步骤、预期行为、实际行为、证据位置、受影响的路线图任务、上游引用以及明确的解除阻塞条件。

需要上游改动时，依赖任务必须保持 `blocked-upstream`。禁止在 Danny 中加入生产 workaround。上游修复后，必须重新运行原始复现和受影响的集成检查，才能将状态改为 `fixed-upstream`。

## 已解决的问题

以下问题已经完成上游修复，并通过原始复现和 Danny 集成验证。

### STELE-001：Provider retrieve 拒绝文档声明的 `top_k` 字段

- **状态：** `fixed-upstream`
- **受影响版本：** `github.com/FelixSeptem/stele v0.0.0-20261004025609-a122447c80d9`
- **修复版本：** `github.com/FelixSeptem/stele v0.0.0-20261005093036-e137040d70ac`
- **复现步骤：** 使用 `STELE_PROVIDER_ENABLED=true` 和 `STELE_PROVIDER_SCHEMA_VERSIONS=provider-v1` 启动 Stele API，通过 `scripts/stele-bootstrap-smoke.ps1` 创建精确 scope 的 runtime principal，然后使用生成的 runtime credential 执行 `go run ./cmd/runtime-probe stele-smoke`。Danny 适配器发送了 OpenAPI 文档规定的 Provider retrieve 请求，其中包含 `input.query` 和 `input.top_k`。
- **预期行为：** `POST /v1/provider/retrieve` 应按照 `openapi/spec.go` 中的 `MemorySearchRequest` 接受 `top_k`，并返回有界检索结果。
- **实际行为：** API 返回 HTTP 400：`{"error":{"category":"validation","code":"invalid_request","message":"invalid request","retryable":false}}`。移除 `top_k` 后重复请求返回 HTTP 200，但结果为空的 `hits` 数组。
- **证据：** `testdata/runtimeprobe/stele-compose.evidence.json` 中 capability 为 `memory` 且 status 为 `failed-contract` 的事件。请求证据已脱敏，没有保存 credential。
- **初步原因：** Provider route 直接将请求严格解码为 `internal/retrieval.SearchInput`，其导出的 `TopK` 字段没有 `json:"top_k"` 标签，因此文档中的 snake-case 字段被当作未知字段拒绝。
- **受影响任务：** OpenSpec 任务 `3.2`、`7.1` 和 `7.3` 在 Provider route 拒绝文档请求结构期间处于阻塞状态。
- **上游引用：** Stele commit `e137040`（`fix: accept OpenAPI top_k in provider retrieve`）。
- **修复内容：** 上游 Provider HTTP route 现在接受 OpenAPI 中的 `input.top_k` 字段；Stele 增加了 `TestProviderRetrieveAcceptsOpenAPITopKField`。
- **修复验证：** 使用 `e137040` 构建的全新 PostgreSQL Compose 部署通过了 `go run ./cmd/runtime-probe stele-smoke`；`memory` 事件为 `passed`，包含 `input.top_k` 的 `/v1/provider/retrieve` 请求返回成功，context 重启投影和幂等重放也通过。
- **解除条件：** 已满足。Danny 适配器继续发送文档规定的 `top_k` 字段，没有加入请求重写或 fallback。

### STELE-002：Provider principal 的 PostgreSQL UUID 类型不兼容

- **状态：** `fixed-upstream`
- **受影响版本：** `github.com/FelixSeptem/stele v0.0.0-20261005093036-e137040d70ac`
- **修复版本：** `github.com/FelixSeptem/stele v0.0.0-20261005143944-9ffe5e9cbf14`
- **复现步骤：** 在全新的 PostgreSQL Compose 数据库中，通过官方 bootstrap 脚本创建 public provider runtime，写入 `conversation.message` Provider event，并等待 worker 执行治理。
- **预期行为：** provider principal 可以参与治理 candidate/canonical 持久化，不触发 PostgreSQL UUID 类型错误。
- **实际行为（旧版本）：** 治理路径把 `id_...` principal 标识写入 UUID 字段，导致 raw event 无法完成治理。
- **证据：** 旧版证据和数据库复现见本 finding 的历史记录；修复版运行证据见 `testdata/runtimeprobe/stele-compose.evidence.json`，memory、context、restart、idempotency 均通过，worker 日志未出现 UUID 类型错误。
- **上游引用：** Stele commit `9ffe5e9`（`fix: generate postgres-compatible application IDs`）。
- **修复验证：** 使用 `9ffe5e9` 构建的全新 PostgreSQL Compose 环境完成 bootstrap，并运行 `go run ./cmd/runtime-probe stele-smoke`；事件已进入 candidate/canonical 治理，未再观察到 UUID/type 错误。
- **解除条件：** 已满足。该问题不再阻塞 Danny；references 的路径问题已由下方 `STELE-003` 修复版本解决。

### 维护者交接

以下 Danny 工作曾因 `STELE-001` 阻塞，在修复验证完成后可以继续：

- OpenSpec `3.2`：长期记忆追加/查询以及 task/run 引用；
- OpenSpec `7.1`：确定性 Milestone 1 退出门禁；
- OpenSpec `7.3`：Milestone 1 退出报告。

验证修复时固定到修复版本，并执行：

```powershell
$env:STELE_URL = "http://127.0.0.1:18080"
$env:STELE_CREDENTIAL_FILE = "D:\code\danny\.tmp\stele-smoke-credentials\runtime.credential"
$env:STELE_TENANT = "tenant-a"
$env:STELE_PROJECT = "project-a"
$env:STELE_NAMESPACE = "namespace-a"
go run ./cmd/runtime-probe stele-smoke
```

接受该修复的依据是：`memory` 事件为 `passed`，带有 `input.top_k` 的 retrieve 请求返回 HTTP 2xx，更新后的 evidence fixture 和集成测试均通过。

## 已解决的问题（续）

### STELE-003：canonical memory promotion 丢失非根 `memory_path`

- **状态：** `fixed-upstream`
- **受影响版本：** `github.com/FelixSeptem/stele v0.0.0-20261005143944-9ffe5e9cbf14`
- **修复版本：** `github.com/FelixSeptem/stele v0.0.0-20261006031612-63d1f3dc4675`
- **复现步骤：** 使用当前固定版本启动全新的 PostgreSQL Compose 服务，初始化 public provider runtime，通过 `POST /v1/provider/events` 追加一个 `event_type=conversation.message` 的事件，设置非根 `memory_path` 和有界 content，然后在相同 scope 下通过 `POST /v1/provider/retrieve` 使用 exact path 查询；同时使用原生 `POST /v1/memories/search` 做对照。Danny 侧的命令是 `go run ./cmd/runtime-probe stele-smoke`，其中 references case 执行等价的 task/run 写入和读取。
- **预期行为：** 已接受的事件应被处理为持久化 canonical memory，并通过文档规定的 exact-path 检索契约返回，且相同 scope 下保留事件 content 和 memory path。
- **实际行为（旧版本）：** `conversation.message` 事件写入成功并返回 `event_id`，worker 成功生成 candidate 和 canonical memory，但 candidate 的非根 `memory_path` 在 canonical promotion 后变成 `/`。相同 scope 的 exact-path Provider retrieve 和原生 `/v1/memories/search` 均返回空结果；canonical content 存在，但路径选择器无法命中。
- **证据：** 旧版 `testdata/runtimeprobe/stele-compose.evidence.json` 和独立 PostgreSQL smoke 数据库观察到 raw event、candidate `memory_path=runtimeprobe/...` 与 canonical `memory_path=/` 的差异。修复版证据已更新到同一文件，references 事件为 `status=passed`，`task_retrieved=true`、`run_retrieved=true`。仓库中没有保存 credential、authorization header、原始 provider 响应或无界内容。
- **初步原因：** Stele 的 canonical memory promotion 没有保留 raw event/candidate 的 `memory_path`，导致路径字段回退到根路径。默认 extractor 使用 `conversation.message` 已得到验证，不属于 Danny workaround。
- **受影响任务：** OpenSpec 任务 `3.2`、`7.1` 和 `7.3` 曾因此阻塞。
- **上游引用：** Stele commit `63d1f3d`（`fix: preserve candidate memory paths during promotion`）。
- **修复内容：** canonical promotion 现在从 candidate 记录保留 `memory_path`，并由 PostgreSQL repository 回归测试覆盖。
- **修复验证：** 使用 `63d1f3d` 构建的全新 PostgreSQL Compose 环境完成 bootstrap。等待 worker 治理完成后，Danny `stele-smoke` references case 返回 `status=passed`，task/run 两个 exact-path 检索均返回 `true`；数据库中 candidate 和 canonical 的非根 `memory_path` 一致。
- **解除条件：** 已满足：
  1. canonical promotion 必须保留 `conversation.message` 事件的非根 `memory_path`；或者提供一个文档化的公开 reference 读写契约，能够返回已记录的 identifier、value 和 path；
  2. 通过本条记录的全新 PostgreSQL 复现；
  3. 通过 Danny references 集成测试。

Danny 没有增加本地 reference store、路径重写或替代写入路径。探针等待 Stele 已有的异步 worker 治理完成后再验证 canonical retrieval。

## 已解决的问题（续）

### STELE-004：Provider 编译投影在 summary canonical 写入阶段失败，事件无法完成治理

- **状态：** `fixed-upstream`
- **受影响版本：** `github.com/FelixSeptem/stele v0.0.0-20261006031612-63d1f3dc4675`（commit `63d1f3dc4675`）
- **修复版本：** `github.com/FelixSeptem/stele v0.0.0-20261007022008-54372e2fe4f3`（commit `54372e2fe4f3`）
- **复现环境：** 使用全新的 PostgreSQL Compose 数据卷和 provider-v1 runtime credential，通过 `go run ./cmd/runtime-probe stele-smoke` 追加 `knowledge/generations/987654321` 编译投影并等待治理。
- **实际行为（旧版本）：** API 写入、幂等 replay 和 exact-path lookup 可以成功，但治理 worker 最终报 `insert summary canonical memory: ERROR: INSERT has more target columns than expressions (SQLSTATE 42601)`，raw event 未完成处理。
- **上游引用：** Stele commit `54372e2fe4f3`（`fix: repair summary canonical persistence`）。
- **修复验证：** 使用 `54372e2fe4f3` 的全新 PostgreSQL Compose 环境重新执行原始复现后，summary canonical INSERT 错误消失；compilation API 写入、相同 event ID replay 和 exact-path lookup 均通过。后续发现的 provenance UUID 错误属于独立的 STELE-005，已由 `a556ac0ddacc` 修复。
- **解除条件：** 已满足。Danny 没有加入 event type 替换、重复写入、路径重写或本地 reference store workaround。
### STELE-005：Provenance 写入使用非 UUID 后缀，编译事件无法完成治理

- **状态：** `fixed-upstream`
- **受影响版本：** `github.com/FelixSeptem/stele v0.0.0-20261007022008-54372e2fe4f3`（commit `54372e2fe4f3`）
- **修复版本：** `github.com/FelixSeptem/stele v0.0.0-20261007033528-a556ac0ddacc`（commit `a556ac0ddacc4026a1bd7b02d7123d884185001a`）
- **复现环境：** 使用修复 commit 构建的 Stele 镜像、全新 PostgreSQL 数据卷、`STELE_PROVIDER_ENABLED=true`、`STELE_PROVIDER_SCHEMA_VERSIONS=provider-v1`，并通过官方 bootstrap 脚本创建精确 scope 的 runtime credential。
- **原始复现：** 在旧版本中运行 `go run ./cmd/runtime-probe stele-smoke`，追加 `knowledge/generations/987654321` 编译投影以及 task/run 引用；API 写入、幂等 replay 和即时 exact-path lookup 可以成功，但异步治理最终失败，错误为 `insert provenance: ERROR: invalid input syntax for type uuid: "<event-uuid>_prov_0" (SQLSTATE 22P02)`，`governance_processed_at` 为空。
- **预期行为：** candidate、canonical 和 provenance 记录使用合法 UUID，raw event 完成治理并设置 `governance_processed_at`；编译投影和 task/run 引用在异步治理及重启后继续可检索。
- **上游引用：** Stele commit `a556ac0ddacc4026a1bd7b02d7123d884185001a`（`fix: generate uuid provenance identifiers`）。
- **修复验证：** 使用修复版本和全新 PostgreSQL Compose 环境重新执行原始探针。`stele_version` 为 `v0.0.0-20261007033528-a556ac0ddacc`；`context`、`memory`、`references`、`compilation`、`restart`、`idempotent` 全部为 `passed`。compilation replay 返回相同 event ID 且 `replayed=true`，编译 exact-path lookup 为 `true`，task/run exact-path lookup 均为 `true`。数据库复核显示三条相关 raw event 的 `governance_processed_at` 已设置、`governance_last_error` 为空，candidate 状态为 `promoted`，canonical 状态为 `active`，未再出现 UUID 错误。
- **证据：** `testdata/runtimeprobe/stele-compose.evidence.json`、隔离 PostgreSQL 查询结果以及修复版本的脱敏 `go run ./cmd/runtime-probe stele-smoke` 输出。仓库中没有保存 credential、Authorization header 或原始 provider payload。
- **受影响任务：** OpenSpec `knowledge-compilation-workflow` 任务 `5.4` 和任务 `5.5` 的 STELE-005 阻塞已解除。
- **解除条件：** 已满足。Danny 没有加入 provenance ID 重写、禁用 provenance、跳过治理或本地 reference store workaround。

### STELE-006：公开 Provider route gate 拒绝已声明的 turns/retrieve 路由

- **状态：** `fixed-upstream`

- **受影响版本：** `github.com/FelixSeptem/stele v0.0.0-20261019065620-7ffd30f9e635`（commit `7ffd30f9e635d4740a5f7585f04f4b529746dede`）
- **复现环境：** 使用 exact pinned module source 构建 `cmd/stele`，连接 Docker PostgreSQL 18.6 的全新 `stele_pinned_7ffd` 数据库，设置 `STELE_PROVIDER_ENABLED=true`、`STELE_PROVIDER_SCHEMA_VERSIONS=provider-v1`，通过 pinned `scripts/stele-bootstrap-smoke.ps1` 创建精确 scope 的 runtime principal 和 `memory-reference-v1` disclosure grant，再运行 Danny `go run ./cmd/runtime-probe stele-smoke`。
- **确定性复现步骤：** bootstrap smoke 的 health、migration、principal、grant、event ingest/replay 均通过；随后 Danny 先调用 `POST /v1/provider/runtime` 成功，再调用文档要求的 `POST /v1/provider/turns`，收到 HTTP 403，响应 category/code 为 `scope/disclosure_denied`。同一次运行中 `POST /v1/provider/retrieve` 也收到 HTTP 403 `disclosure_denied`；`POST /v1/provider/events` 的同 key replay 返回原 event 结果并通过。
- **预期行为：** 已拥有 exact-scope disclosure grant 的 public runtime principal 应能调用 OpenAPI 声明的 `/v1/provider/turns` 和 `/v1/provider/retrieve`，由 route handler 继续执行 turn/retrieval 的 scope、profile 和 response-mode 检查；不应在公共 route gate 阶段被拒绝。
- **实际行为：** pinned `internal/app/disclosure_routes.go` 的 `disclosureRouteGate` allowlist 包含 `/v1/provider/runtime`、`/v1/provider/context`、`/v1/provider/events` 等路径，但遗漏 `/v1/provider/turns` 和 `/v1/provider/retrieve`。服务在业务 handler 前统一返回 403 `disclosure_denied`，导致 Danny 无法取得 server-issued turn，也无法完成 memory/reference/compilation exact-path retrieval。
- **证据：** `testdata/runtimeprobe/stele-7ffd-live.evidence.json`、`docs/stele-context-evidence.md`、pinned source `internal/app/disclosure_routes.go` 和脱敏 Danny smoke 输出。证据不包含 credential、Authorization header、原始 context body 或无界 provider payload。
- **受影响任务：** OpenSpec `runtime-context-ownership-boundary` 任务 `1.4`、`6.1`、`6.2`、`6.3`、`6.5`、`7.3`；依赖这些任务的 `8.2`、`8.3` 也不能完成。
- **修复版本：** `github.com/FelixSeptem/stele v0.0.0-20261024024237-b63151c0464d`（commit `b63151c0464dd49379cc9324eecc19e56d1866a1`，fix commit `6d8983099147d79b432575347a1dfdf2abaf7fe7`）。该版本将 turns/retrieve 纳入 public disclosure route gate；新 pin PostgreSQL smoke 确认 retrieve route 可进入 handler，原始 403 已消失。完整 turn/context 和 exact-path acceptance 另受 STELE-007 及后续验收影响。
- **解除条件：** 本条 route gate 缺陷已修复；未将完整 context capability 标为 verified。Danny 不加入 route fallback、绕过 disclosure 或改用未声明的 native endpoint。

## 曾经未解决的问题及复核状态

以下条目保留原始失败证据；当前阻塞状态以每条最新复核结论为准。

### STELE-007：公开 Provider turns 路由对符合 OpenAPI 的请求返回 operation_failed

- **状态：** `fixed-upstream`
- **受影响版本：** `github.com/FelixSeptem/stele v0.0.0-20261024024237-b63151c0464d`（commit `b63151c0464dd49379cc9324eecc19e56d1866a1`，包含修复 STELE-006 的 `6d8983099147d79b432575347a1dfdf2abaf7fe7`）
- **复现环境：** exact module-cache source 构建的 Stele API/worker，Docker PostgreSQL 18.6 全新数据库 `stele_pinned_b631`，全新 scope `danny-live-b631/runtime-context/acceptance`，使用文档 bootstrap smoke 生成的 public runtime credential 和 `memory-reference-v1` exact-scope grant。
- **确定性复现步骤：** 初始化 `/v1/provider/runtime` 获得 server binding/session 后，使用同一 credential 和绑定调用 `POST /v1/provider/turns`，body 仅含 OpenAPI 声明的 `metadata` 与 `input.query`，得到 HTTP 400 `validation/operation_failed`。加入 `context_budget`、`idempotency_key` 或省略这些可选字段均得到相同结果。Danny 修正 body 不再发送 OpenAPI 未声明的 `session_id` 后仍可复现。服务日志确认请求进入 `/v1/provider/turns` handler；错误响应只给出通用 `provider operation failed`，当前无法从公开错误中获得更细原因。同一 smoke 中 memory append/retrieve、restart context、events idempotency 通过；turn-bound context 和完整 references/compilation acceptance 未通过。
- **预期行为：** 符合 OpenAPI 的 `ProviderTurnRequest` 在有效 runtime binding、session 和 exact-scope disclosure grant 下应创建 server-issued turn 并返回 `turn_id`、`session_id`、disclosure mode 与 context evidence。
- **实际行为：** 已通过 route gate，但 turn operation 返回 HTTP 400 `operation_failed`，无 server-issued turn，无法继续 turn-bound context continuity 验收。
- **证据：** `testdata/runtimeprobe/stele-b631-live.evidence.json`、`docs/stele-context-evidence.md`、b631 pin 的 `openapi/spec.go` 和 `internal/app/provider_http.go`。Evidence 不含 credential、Authorization header 或原始 context body。
- **受影响任务：** OpenSpec `runtime-context-ownership-boundary` 任务 `1.4`、`6.1`、`6.2`、`6.3`、`7.3`，以及依赖完整 exit gate 的 `8.2`、`8.3`。`6.5` 还需独立完成 exact-path durable reference/compilation 验收。
- **解除条件：** 已由后续上游修复和下述全新 PostgreSQL/pgvector 验证满足。Danny 未加入兼容性猜测、绕过 grant 或本地 turn 替代。

#### `8ae6028` 回归复核（2026-10-24）

- **复核 pin：** Stele `8ae6028d68fd0ff672e40a8b7e12017aec779aa6`，模块版本仍为 `v0.0.0-20261024024237-b63151c0464d`；Danny 依赖未更新。
- **复核环境：** 全新 PostgreSQL 数据库 `stele_live_8ae6028`、全新 scope `danny-live-8ae6028/runtime-context/acceptance`、最新 `cmd/stele` API/worker、官方 bootstrap smoke 创建的全新 public runtime credential 和 exact-scope `memory-reference-v1` grant。
- **新增修复覆盖：** 最新提交把 runtime binding 与 server-owned `memory_session_runs` 的创建放入 PostgreSQL 事务；数据库复核确认 runtime binding 和 active session 均已存在。
- **原始 smoke 结果：** `POST /v1/provider/turns` 仍返回 HTTP 400 `validation/operation_failed`；请求已通过 route gate，且服务日志显示进入 turns handler。失败后数据库 `memory_session_turns` 仍为零，无法取得 server-issued `turn_id`，因此 turn-bound context/continuity 不能继续验收。
- **同期结果：** `/v1/provider/retrieve`、events idempotency、restart context 通过；references 和 compilation exact-path 在本次运行中仍未通过，不能替代完整验收证据。
- **证据：** 本次脱敏 smoke 输出已保存为 `testdata/runtimeprobe/stele-8ae6028-live.evidence.json`；未提交 credential、原始 provider body 或日志。此前固定证据仍为 `testdata/runtimeprobe/stele-b631-live.evidence.json`。本次结果足以确认 `STELE-007` 在 `8ae6028` 仍为 `blocked-upstream`。

#### `09e51a7` 回归复核（2026-10-24）

- **复核 pin：** Stele `09e51a749be2778ce238e05f05675754e946332c`（`fix(memory): allow provider turns without conversation id`）；模块版本仍为 `v0.0.0-20261024024237-b63151c0464d`，Danny 依赖当时未更新。
- **复核环境：** 全新 PostgreSQL 数据库 `stele_live_09e51a7`、全新 scope `danny-live-09e51a7/runtime-context/acceptance`、最新 `cmd/stele` API/worker、官方 bootstrap smoke 创建的全新 public runtime credential 和 exact-scope `memory-reference-v1` grant。
- **新增修复覆盖：** runtime binding attribution 现在允许省略 `conversation_id`；这与 Danny 正常 runtime 初始化的调用形态一致。数据库复核确认 runtime session、runtime binding 已持久化，且正常 binding 的 `conversation_id` 为空。
- **原始 smoke 结果：** 正常的无 `conversation_id` 请求仍使 `POST /v1/provider/turns` 返回 HTTP 400 `validation/operation_failed`。失败后 `memory_session_turns` 仍为零，无法取得 server-issued `turn_id`。在相同 scope 中显式设置非空 `conversation_id` 的对照 binding 调用相同 public endpoint 也返回 HTTP 400，因此失败并非仅由 Danny 省略该可选字段导致。
- **同期结果：** `/v1/provider/retrieve`、events idempotency、restart context 通过；references 和 compilation exact-path 仍未通过，不能替代完整验收证据。
- **证据：** 脱敏 smoke 输出保存在 `testdata/runtimeprobe/stele-09e51a7-live.evidence.json`；未提交 credential、Authorization header、原始 provider body 或服务日志。本次复核确认 `STELE-007` 在 `09e51a7` 仍为 `blocked-upstream`。

#### `f38016e` 修复复核（2026-10-25）

- **修复 pin：** `github.com/FelixSeptem/stele v0.0.0-20261025080141-f38016ec0cba`；turn/session 修复来自 `4db6ce8`、`09e51a7`，PostgreSQL continuity 修复来自 `f81f920` 和 `f186f15`。
- **复现环境：** 官方 `scripts/stele-provider-context-verify.ps1` 创建随机隔离 Compose 项目、全新 PostgreSQL 18 + pgvector 数据库和新 scope，使用该 pin 的镜像完成 initial 与 API/worker restart 两阶段验证，结束后清理自有资源。
- **结果：** Provider context、durable references、runtime feedback projection 三组 assurance 在 initial/restart 均通过；覆盖 turn/context continuity、governance completion、durable source labels、non-root exact-path retrieve、reference replay/conflict 与 restart identity。
- **证据：** `testdata/runtimeprobe/stele-f38016-live.evidence.json`、`stele-f38016-reference.evidence.json`、`stele-f38016-projection.evidence.json` 及各自 `.initial.json` 文件。报告包含源码 revision 和镜像 digest，不含 credential 或原始 provider payload。
- **当前结论：** STELE-007 不再阻塞 Stele live context/retrieve acceptance；完整提案仍须满足其余 Baymax、Danny durable projection 和 OpenSpec exit criteria。

#### Danny `f38016e` 端到端复核（2026-10-26）

- **复核环境：** 随机隔离 Docker Compose 项目、全新 PostgreSQL 18 + pgvector 数据库、`f38016e` 镜像和 Stele 官方 bootstrap 创建的精确 scope runtime credential。
- **结果：** Danny `stele-smoke` 的 `context`、`memory`、`references`、`compilation`、`restart` 和 `idempotent` 六项 capability 全部为 `passed`。context 记录 server-issued `turn_id`、issue/compare continuity 结果以及独立 source/application digest；references 的 task/run exact-path retrieval 均为 `true`；compilation replay 保留同一 event ID；restart 和幂等重放均通过。
- **证据：** `testdata/runtimeprobe/stele-f38016-danny-smoke.evidence.json`。报告不含 credential、Authorization header 或无界 provider payload；运行结束后隔离容器、卷和临时 credential 已清理。
- **适配器说明：** Stele governance 是异步的，Danny `retrieveReference` 在同一个 retrieve body/idempotency key 下执行有界重试，直到 canonical memory 可见；不会重写 path、重复写 event 或用本地 reference store 替代 Stele。
- **当前结论：** STELE-007 的原始 turn/context 缺口和后续 Danny live smoke 均已复核通过；Stele context、reference、compilation 和 restart acceptance 不再是当前提案的阻塞项。剩余未完成项是 Baymax/OpenAI-compatible live evidence、assembly ownership cleanup 和 scheduled projection delivery。

## 当前集成 pin 与 2026-11-11 复核

Danny 的 provider/context 适配器已由
`github.com/FelixSeptem/stele v0.0.0-20261025080141-f38016ec0cba`
（commit `f38016ec0cbafcd6877e2f66ee1da44ea8e4b057`）升级到远端 HEAD
`v0.0.0-20261111135338-19fc90134492`（commit
`19fc9013449246539be7ec2f6a875da160d671a2`，原 pin 落后 32 个提交）。
升级后 `go build ./...`、`go test ./...` 通过，pinned context fixture 对新
`openapi.SpecYAML()` 仍成立；`ProviderContextContinuityCapabilities` 的
`exact_snapshot_replay` 在新 pin 仍为 `false`，因此
`stele.context.snapshot-replay` 继续保持 `unsupported`。

新 pin 的公开变化包括：`ProviderCapabilityDocument.required` 新增
`corrections`、`MemoryIntentCorrectionAttribution`，以及
`versioned-context-projections`、`governed-memory-intents`、
`runtime-memory-provider-contract` 等规格。Danny 当前未消费
`ProviderCapabilityDocument`，因此这些变化不构成编译期破坏，但所有基于
`f38016e` 的 live-service 结论都不能自动迁移到新 pin。

上文 `f38016e` 的 Stele 官方 fresh PostgreSQL/pgvector initial/restart
assurance 与 Danny 六项端到端 smoke 属于**历史证据**，文件保留不改；
`STELE-007` 仍为已解除状态，未发现新的上游缺陷。
`testdata/runtimeprobe/stele-capabilities.json` 中的
`stele.context.live-service` 已标为 `incomplete`：当前 pin 尚未在真实
PostgreSQL/pgvector 服务上重跑，属 OpenSpec 任务 7.3 的验收范围。
详情见 `docs/stele-context-evidence.md` 和
`testdata/runtimeprobe/stele-f38016-*.evidence.json`。
