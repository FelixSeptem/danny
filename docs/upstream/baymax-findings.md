# Baymax 上游核查记录

本文记录 `github.com/FelixSeptem/baymax`
`v0.0.0-20261110150429-cca95f31f71b` 在 Danny 里程碑 8 边界上的可复现
契约结果。这里的“未解决”表示当前 pin（固定版本）的部署证据或 Danny 集成证据不足，
不把 Danny 的适配缺口误报为上游缺陷。

2026-11-11：依赖由 `v0.0.0-20261103091945-d2e08cd6dc8c` 升到远端 HEAD
`cca95f31f71b`（原 pin 落后 10 个提交）。上游在此期间归档了
`194-child-composer-continuation-contract`、
`196-deployable-source-history-store-contract`、
`198-mailbox-agent-pause-restore-boundary`，以及团队续接恢复（team continuation restore）。

2026-11-12：Danny 适配层完成接线。`internal/agent/baymax/owner_store.go` 与
`continuation_owners.go` 让生产适配器真正调用
`history.OpenOwnerStore` 和 child/team/mailbox 三类 continuation owner，
并补齐逐 form（形态）、逐暂停原因的新进程恢复复现。下面两条发现项（finding）按新证据
重新定级。

## 已解决

### source history 的显式 owner 绑定（Danny 侧解阻）

- **Pin / 公开契约**：`v0.0.0-20261110150429-cca95f31f71b`；
  `core/history.OpenOwnerStore`、`OwnerStoreOptions`、
  `OwnerStore.Capabilities`、`ReconcileProjection`。
- **原发现项**：Danny 生产路径没有显式绑定持久化 owner store，
  `Reconcile` 只读进程内 map，重启后无法恢复；执行期用的是内存日志。
- **接线方式**：`Client.BindOwnerStore(OwnerStoreConfig{Root, Retention,
  ProjectionRetention, DeploymentRequest})`。绑定后
  `RunAuthorized/ResumeAuthorized` 用 `runner.OpenNativeEventHistory`
  写 source-owned 文件日志（journal）；`Reconcile` 走
  `history.OpenOwnerStore` 重新打开 owner，检查 `Capabilities`，按有界
  cursor `ReadAfter`，逐事件 `ReconcileProjection`（幂等）。未绑定 owner 时
  保持原有内存行为，不会隐式升级持久化等级。
- **复现证据**：`internal/agent/baymax/owner_store_test.go`
  - `TestOwnerStoreBindingFailsClosedOnUnsupportedDeployment`：跨机配置档在
    绑定阶段即被拒绝，不降级。
  - `TestOwnerStoreReplaysTerminalAcrossFreshProcess`：进程 A 提交终态事实，
    进程 B 重新打开 owner 后读回同一身份与终态。
  - `TestOwnerStoreReconcilesProjectionIdempotentlyAfterCrashBeforeProjection`：
    来源已追加、投影未写时崩溃，重放后投影补一次且重复重放收敛。
  - `TestOwnerStoreReportsGapWhenSourceFactsAreUnavailable`：来源事实不可用
    时返回 recovery-required，不重跑不确定效果。
  - `TestOwnerStoreRetentionBoundsReplayAndRejectsExpiredCursor`：保留下限
    （retention floor）与过期游标（cursor）被拒绝。
- **仍然成立的边界（不是缺陷）**：上游 196 的非目标（non-goals）明确不提供跨机协调、
  多写者 CAS，也不与 Danny 构成跨 store 原子事务。Danny 侧同样只声明
  `same_host_single_writer_sequential_reopen`。按路线图（roadmap）「上游阻塞策略
  （Upstream blocking policy）」第 6 步，这一条不靠新增运行时日志（runtime journal）或假事务来“解决”，
  而是把边界写进部署范围与文档。
- **证据**：`internal/agent/baymax/owner_store.go`、
  `internal/agent/baymax/owner_store_test.go`、
  `docs/baymax-contract-evidence.md`、
  `openspec/changes/archive/196-deployable-source-history-store-contract/design.md`。
- **状态：** `fixed-upstream`
- **受影响任务：** 3.2、7.1 与 7.3（owner store 绑定与恢复读取）已闭合；
  跨机协调与多写者 CAS 仍按上游非目标保持不提供。
- **解除阻塞条件：** 已满足。`v0.0.0-20261110150429-cca95f31f71b` 已提供
  `core/history.OpenOwnerStore`，Danny 侧通过 `Client.BindOwnerStore` 显式绑定，
  重建后 `Reconcile` 可从 owner 重放；`TestOwnerStoreReplaysTerminalAcrossFreshProcess`
  与 `TestOwnerStoreReconcilesProjectionIdempotentlyAfterCrashBeforeProjection`
  在新进程中验证通过。

### continuation 按暂停原因逐项的实时闸门（live gate，已通过真实端点验证）

- **Pin / 公开契约**：`core/types.ContinuationCapabilities`、
  `ContinuationCapabilitySource`、
  `composer.Composer.ContinuationCapabilities`、
  `teams.Engine.ContinuationCapabilities`、
  `mailbox.Mailbox.ContinuationCapabilities` 与
  `mailbox.NewContinuationWorker`。
- **原发现项**：Danny 代码对新 owner 零引用，child/team 在能力清单（capability
  manifest）中为 `unsupported`，mailbox 未声明处理程序暂停恢复（handler pause restore）。
- **已补齐的部分**：`Client.BindContinuationOwners` 为 child、team、mailbox
  分别绑定持久化 owner；`ContinuationCapabilities(ctx, runID)` 返回逐
  form、逐暂停原因的只读证据矩阵（不创建检查点、不恢复效果）。
  `internal/agent/baymax/continuation_restore_test.go` 覆盖 3 个 form ×
  3 个暂停原因的“一个进程暂停、另一个进程恢复”：
  child 走 `ResumeChildContinuation`，team 走
  `Engine.ResumeContinuation`，mailbox 走
  `Worker.ResumeHandlerContinuation`；三者都验证重复恢复收敛、
  已完成效果不重跑。
- **复现步骤：** 在配置了真实 OpenAI 兼容端点（`DANNY_MODEL_ENDPOINT`、
  `DANNY_MODEL_API_KEY`）与固定版本的 Baymax 环境执行
  `go run ./cmd/runtime-probe live-pause-restore`，对 child、team_parallel、
  team_vote、mailbox 四个 form × 三个暂停原因逐条执行暂停与恢复；
  恢复必须在全新 OS 子进程中重建 client 后走授权入口完成。
- **live gate 已通过（2026-11-12）**：
  `cmd/runtime-probe live-pause-restore` 在真实 OpenAI 兼容端点
  （`agnes-3.0-flash`，`POST /v1/responses`）上对 child、team_parallel、
  team_vote、mailbox 四个 form × `input_required`/`approval_required`/
  `budget_exhausted` 三个暂停原因共 12 条链逐条执行：暂停在本进程
  通过生产 owner 落盘由 source 拥有的检查点（checkpoint），恢复与重复恢复分别在两个
  全新 OS 子进程中重建 client 后走授权入口完成（真实提供方调用），
  重复恢复均收敛且不重跑已完成效果
  （`testdata/runtimeprobe/baymax-live-pause-restore.evidence.json`，
  12/12 通过）。过程中确认并记录了两个上游契约事实：
  1. parallel/vote 团队的暂停边界是整批级的（上游注释明确：在批次中途
     暂停会把未执行的成员持久化为 in-flight，伪造恢复材料），因此 live
     暂停点在全部成员派发之前，恢复阶段执行剩余成员；
  2. `Engine.ResumeContinuation` 校验 `teamPlanDigest(plan)` 与检查点
     的 `GraphDigest` 一致，暂停与恢复必须使用完全相同的计划参数
     （含 `ParallelMaxWorkers`），否则返回 `ErrTeamContinuationForeign`。
- **能力清单定级**：child（subagent_fanout）、team_parallel、
  team_vote 已提升为 `verified`，mailbox 的处理程序暂停恢复也已由
  live 证据覆盖（`internal/runtimeboundary/contracts.go`）。
- **证据**：上游归档 194/198、`orchestration/composer/capability.go`、
  `orchestration/teams/continuation.go`、
  `orchestration/mailbox/continuation.go`、
  `internal/agent/baymax/continuation_owners.go`、
  `internal/agent/baymax/continuation_live.go`、
  `internal/agent/baymax/continuation_restore_test.go`、
  `cmd/runtime-probe/pause_restore.go`、
  `testdata/runtimeprobe/baymax-live-pause-restore.evidence.json`、
  `internal/runtimeboundary/contracts.go`、
  `docs/baymax-contract-evidence.md`。
- **受影响任务**：1.3、3.4、4.3-4.7、5.2-5.5、7.2-7.3（均已闭合）。
- **状态：** `fixed-upstream`
- **解除阻塞条件：** 已满足。child、team_parallel、team_vote 的 capability 已在
  `internal/runtimeboundary/contracts.go` 提升为 `verified`，mailbox 的处理程序
  暂停恢复由 `cmd/runtime-probe live-pause-restore` 的 12/12 live 证据覆盖；
  重复恢复收敛且不重跑已完成效果。


## 未解决

（当前无未解决发现项。2026-11-12 的 live pause/restore 证据通过后，
continuation live gate 一条转入已解决。）

## 复核记录

- 2026-11-11：`go get github.com/FelixSeptem/baymax@cca95f31f71b` 生成
  `v0.0.0-20261110150429-cca95f31f71b`；`go build ./...`、`go test ./...`
  与三个固定版本的测试夹具（fixture）全部通过。当时两条发现项都还是“未解决”。
- 2026-11-12：owner store 绑定与 continuation owner 接线完成。
  `go build ./...`、`go vet ./internal/agent/baymax`、
  `go test ./internal/agent/baymax ./internal/tasks ./internal/runtimeboundary
  -count=1` 通过。source history 显式绑定一条转入“已解决”；
  continuation 一条只剩 live gate，不再归因于 Danny 未接线。
- 2026-11-12（晚）：`cmd/runtime-probe live-pause-restore` 在真实端点通过
  12/12 链（4 form × 3 暂停原因），恢复与重复恢复在全新 OS 子进程中
  完成。能力清单中的 child/team 提升为 `verified`，
  mailbox 的处理程序暂停恢复由 live 证据覆盖，continuation live gate
  一条转入“已解决”，本文不再有未解决发现项。
