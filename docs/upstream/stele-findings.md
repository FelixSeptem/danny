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

## 未解决的问题

以下问题仍会阻塞依赖它们的 Danny 任务。未满足解除条件之前，不得加入本地 workaround。

当前没有未解决的 Stele finding。
