# 运行时探针证据

里程碑 1 使用 Baymax 与 Stele 固定的公共 API 作为集成边界。确定性命令为：

```powershell
$env:GOCACHE = "$PWD/.gocache"
$env:GOSUMDB = "off"
$env:GOPROXY = "off"
go run ./cmd/runtime-probe offline > runtime-probe.json
```

该命令使用 Baymax 的 `integration/fakes.Model`，然后为 Run 和 Stream 运行真实的
`core/runner.Engine`。它不会联系模型端点。在配置真实的 Stele 服务与 PostgreSQL
记录系统之前，Stele 表示为 `skipped-unconfigured`；不使用任何内存替代实现。

选择性启用的实时路径是独立的。它针对一个已配置端点，运行临时 `live-smoke` 闸门
所覆盖的每一种执行形式：

```powershell
$env:OPENAI_BASE_URL = "https://provider.example/v1"  # must implement the Responses API
$env:OPENAI_API_KEY = "..."
$env:OPENAI_MODEL = "model-name"
go run ./cmd/runtime-probe live-smoke > live-runtime-probe.json
```

形式：`direct`、`child`、`team_parallel`、`team_vote`、`workflow`、`mailbox`。
每种形式都经由 Danny 的授权适配器运行，因此证据记录的是产品路径，而不是仅用于
探针的捷径。`DANNY_LIVE_FORMS` 可将重跑限制为某个子集（未知名称会被拒绝）：
`DANNY_LIVE_FORMS=direct,child`。`DANNY_LIVE_OWNER_ROOT` 覆盖每次运行的 owner
根目录，其默认值为 `.tmp/live-probe/<run-id>`，且绝不会在多次运行之间复用。

端点必须实现 OpenAI Responses API（`/v1/responses`）。固定的适配器 profile 声明
`responses.v1`；仅支持 chat-completions 的端点会在发送任何请求之前被以
`openai_endpoint_profile_unsupported_api_shape` 拒绝。`team_vote` 要求模型只用
单个 `approve`/`reject` token 作答，因为投票聚合会拒绝散文式回答。

某个形式失败时，它自己产生的事件会被标记为 `failed-contract`，运行以非零状态
退出，但已通过形式的结果包仍会写入 stdout。这样部分通过的闸门仍可诊断，又不会
看起来像整体通过。不要为该命令设置 `STELE_URL`：live-smoke 的 Stele 调用不会
加载运行时凭据，真实服务会拒绝它。请单独运行 `stele-smoke`。

2026-11-05，在未设置 `OPENAI_BASE_URL`、`OPENAI_MODEL` 和 `OPENAI_API_KEY` 的
情况下运行了该命令。它返回 `skipped-unconfigured`，未联系任何 provider；结果
作为未完成的验收记录在
`testdata/runtimeprobe/baymax-live-smoke-unavailable.evidence.json`。

2026-11-12，该命令针对一个实现了 `POST /v1/responses` 的 OpenAI 兼容网关
（`agnes-3.0-flash`）真实运行。六种形式全部通过，结果包提交为
`testdata/runtimeprobe/baymax-live-smoke.evidence.json`。该包中的 Stele 用例为
`skipped-unconfigured`，因此任务 7.3 在 Stele 一侧仍未完成。

### Provider 兼容垫片

2026-11-12 的运行需要 `DANNY_LIVE_PROVIDER_SHIM=stream-default`。该网关将
**省略**的 `stream` 字段视为流式调用，随后尝试对 SSE 响应体做 JSON 解码，返回
`500 bad_response_body`
（`invalid character 'd' looking for beginning of value`）。在 Responses API 中
省略 `stream` 意味着 `false`，而固定适配器对非流式请求会省略该字段，因此这是
provider 缺陷而非 Danny 或 Baymax 的缺陷：同样的请求体加上显式
`"stream": false` 就能成功。

该垫片是执行框架内部的环回反向代理。它把请求原样转发到真实端点，仅在字段缺失
时补充 `"stream": false`；已经带有 stream 标志的请求不会被修改。它是选择性启用
的，启用时结果包会记录 `"provider_compat": "stream-field-injected"`，因此通过的
运行绝不会看起来像一次干净的一致性运行。该标记在脱敏后仍会保留。不要对符合规范
的 provider 启用它，也不要将通过的结果包解读为网关符合规范契约的证明。

针对由 PostgreSQL 支撑的服务执行仅 Stele 的真实探针：

```powershell
$env:STELE_URL = "http://127.0.0.1:8080"
$env:STELE_CREDENTIAL_FILE = ".tmp/stele-smoke-credentials/runtime.credential"
$env:STELE_TENANT = "tenant-a"
$env:STELE_PROJECT = "project-a"
$env:STELE_NAMESPACE = "namespace-a"
go run ./cmd/runtime-probe stele-smoke > stele-runtime-probe.json
```

Stele 服务必须以 `STELE_PROVIDER_ENABLED=true` 和
`STELE_PROVIDER_SCHEMA_VERSIONS=provider-v1` 启动。引导凭据由 Stele 文档化的
`stele-bootstrap-smoke.ps1` 创建；它从文件中读取，绝不会被写入证据。

证据使用 schema `runtime-probe.v1`。合并探针用例之后会重写序号，校验会拒绝缺失
的 ID、断裂的祖先关联、序号缺口和重复的终态事件。`provider_compat` 是可选顶层
字段：缺省表示 provider 被原样调用，有值表示执行框架在调用途中做了适配。在写入
产物之前，`runtimeprobe.Redact` 会替换密钥并对敏感或无界值做哈希。凭据、
authorization 头、提示词和原始 provider 载荷不得提交。

## 能力状态

- `passed`：已配置的适配器完成了用例并产生了有效证据。
- `failed-contract`：适配器到达了依赖，但操作失败。
- `skipped-unconfigured`：该用例需要显式配置的外部服务。
- `blocked-upstream`：确定性复现证明了依赖契约缺陷。

Stele provider 操作使用 `/v1/provider/runtime`、`/v1/provider/context`、
`/v1/provider/intents` 以及固定 OpenAPI 文档中的其他路径。适配器在可用时发送
`X-Stele-Runtime-Binding`、`X-Stele-Runtime-Session` 以及按 occurrence 作用域
的 `Idempotency-Key`。

离线证据通过确定性测试夹具（fixture）覆盖 Baymax Run/Stream、类型化本地工具、
Composer 子任务派发、原生并行/投票团队、信箱生命周期，以及调度器/上下文适配器
调用。团队证据包括由执行派生的团队/成员身份、原生成员与团队游标、继承的权限、
原生 worker/超时/取消控制、投票来源证明，以及失败关闭（fail closed）的聚合。
Stele 持久化用例使用真实服务与 PostgreSQL 记录系统；它们绝不会被替换为进程内
存储。

仓库契约套件可以针对 Docker PostgreSQL 以及 SQLite 检验应用持久化边界：

```powershell
$env:DANNY_TEST_POSTGRES_URL = "postgres://stele:stele-local-only@127.0.0.1:5432/stele?sslmode=disable"
go test ./internal/storage -count=1
```

这检验 Danny 迁移、原生证据/游标投影、检查点权限、修订版本冲突、定时效果与
发件箱投递。它不能作为该 PostgreSQL 数据库就是固定 Stele 服务的证据。

最近一次提交的、由 Compose 支撑的 Stele 运行记录在
`testdata/runtimeprobe/stele-compose.evidence.json`。该历史产物是针对
`github.com/FelixSeptem/stele v0.0.0-20261007033528-a556ac0ddacc` 采集的，不能
作为当前固定版本的证据。当前适配器固定为
`github.com/FelixSeptem/stele v0.0.0-20261111135338-19fc90134492`。其公开的、
按轮次绑定的上下文契约由本地适配器测试夹具针对刷新后的 `openapi.SpecYAML()`
加以检验。`testdata/runtimeprobe/stele-f38016-*.evidence.json` 中的
PostgreSQL/pgvector 证据属于历史 `f38016e` 固定版本，不能作为当前固定版本的证据；
任务 7.3 仍需要在 `19fc901` 上重跑实时服务。下文的历史上下文、基础记忆追加/查询、
新适配器重启投影以及 occurrence 重放结果仅作历史背景。来自 `STELE-002` 的
provider principal UUID 问题、来自 `STELE-003` 的规范路径提升问题、来自
`STELE-004` 的摘要规范持久化问题，以及来自 `STELE-005` 的 provenance UUID 问题
均已在上游修复。在既有的异步治理 worker 完成后，引用用例为 `passed`：任务侧与
运行侧的精确路径读取都返回 true，数据库在 candidate 行和 canonical 行中保留了
非根路径。适配器使用 Stele 文档化的 `conversation.message` 事件类型，并继续发送
`input.top_k`；它不维护本地引用存储，也不重写路径。由于 Stele 治理在 worker 中
异步运行，实时冒烟验证会等待既有的 worker 投影完成后才执行精确路径读取。

历史的 `7ffd30f9e635` 上下文冒烟被披露路由闸门阻塞；其证据保留在
`stele-7ffd-live.evidence.json` 中，修复存在于 Stele 提交
`6d8983099147d79b432575347a1dfdf2abaf7fe7` 中。早先的 `b63151c0464d` 上下文冒烟
先通过 `POST /v1/provider/turns` 创建由服务器发放的 Provider 轮次，然后以
`continuity.mode=issue` 执行一次精确的按轮次绑定 `POST /v1/provider/context`
读取，最后以 `continuity.mode=compare` 重复一次。证据只记录轮次 ID、来源/应用
摘要和连续性结果；它绝不会存储上下文正文、查询、凭据或 authorization 头。该
固定版本的 `400 operation_failed` 结果保留在
`testdata/runtimeprobe/stele-b631-live.evidence.json` 与 `STELE-007` 中。
历史 `f38016e` 固定版本通过了原始 Stele 保障验证和 Danny 全新的
PostgreSQL/pgvector 端到端冒烟；脱敏报告位于
`testdata/runtimeprobe/stele-f38016-*.evidence.json`，包括
`stele-f38016-danny-smoke.evidence.json`。它们作为历史证据保留，且不覆盖当前
`19fc901` 固定版本。`f38016e` 固定版本还记录了一点：省略 `source_metadata` 的
事件默认归为 `untrusted/restricted`，并持续被排除在公开 retrieve/context 之外。
Danny 的引用分类必须遵循产品数据策略，而不是检索需要。

编译探针在 `knowledge/generations/<generation-id>` 使用相同的公共事件契约，载荷
为引用优先的 `conversation.message`。它验证重放时会返回原始事件 ID，并执行精确
路径查找，而不把产物正文复制进 Stele。在干净固定版本 `a556ac0ddacc` 的 Compose
验证中，API、重放、精确路径查找与异步治理全部通过。数据库显示了已处理的原始
事件（无治理错误）、已提升的候选记忆和活跃的规范记忆。探针与适配器不添加
provenance 重写、不跳过治理，也不维护本地引用存储；上游 provenance 修复记录为
`docs/upstream/stele-findings.md` 中的 `STELE-005`。
