# Baymax 上游核查记录

本文记录 `github.com/FelixSeptem/baymax`
`v0.0.0-20261110150429-cca95f31f71b` 在 Danny Milestone 8 边界上的可复现
契约结果。这里的“未解决”表示当前 pin 的部署证据或 Danny 集成证据不足，
不把 Danny 的适配缺口误报为上游缺陷。

2026-11-11：依赖由 `v0.0.0-20261103091945-d2e08cd6dc8c` 升到远端 HEAD
`cca95f31f71b`（原 pin 落后 10 个提交）。上游在此期间归档了
`194-child-composer-continuation-contract`、
`196-deployable-source-history-store-contract`、
`198-mailbox-agent-pause-restore-boundary`，以及 team continuation restore。

2026-11-12：Danny 适配层完成接线。`internal/agent/baymax/owner_store.go` 与
`continuation_owners.go` 让生产 adapter 真正调用
`history.OpenOwnerStore` 和 child/team/mailbox 三类 continuation owner，
并补齐逐 form、逐 pause reason 的新进程恢复复现。下面两条 finding 按新证据
重新定级。

## 已解决

### source history 显式 owner 绑定（Danny 侧解阻）

- **Pin / public contract**：`v0.0.0-20261110150429-cca95f31f71b`；
  `core/history.OpenOwnerStore`、`OwnerStoreOptions`、
  `OwnerStore.Capabilities`、`ReconcileProjection`。
- **原 finding**：Danny 生产路径没有显式绑定 durable owner store，
  `Reconcile` 只读进程内 map，重启后无法恢复；执行期用的是内存 journal。
- **接线方式**：`Client.BindOwnerStore(OwnerStoreConfig{Root, Retention,
  ProjectionRetention, DeploymentRequest})`。绑定后
  `RunAuthorized/ResumeAuthorized` 用 `runner.OpenNativeEventHistory`
  写 source-owned 文件 journal；`Reconcile` 走
  `history.OpenOwnerStore` 重新打开 owner，检查 `Capabilities`，按 bounded
  cursor `ReadAfter`，逐事件 `ReconcileProjection`（幂等）。未绑定 owner 时
  保持原有内存行为，不会隐式升级耐久等级。
- **复现证据**：`internal/agent/baymax/owner_store_test.go`
  - `TestOwnerStoreBindingFailsClosedOnUnsupportedDeployment`：跨机 profile 在
    绑定阶段即被拒绝，不降级。
  - `TestOwnerStoreReplaysTerminalAcrossFreshProcess`：进程 A 提交终端事实，
    进程 B 重新打开 owner 后读回同一 identity/terminal。
  - `TestOwnerStoreReconcilesProjectionIdempotentlyAfterCrashBeforeProjection`：
    source 已追加、投影未写时崩溃，重放后投影补一次且重复重放收敛。
  - `TestOwnerStoreReportsGapWhenSourceFactsAreUnavailable`：source 事实不可用
    时返回 recovery-required，不重跑不确定效果。
  - `TestOwnerStoreRetentionBoundsReplayAndRejectsExpiredCursor`：retention
    floor 与过期 cursor 被拒绝。
- **仍然成立的边界（不是缺陷）**：上游 196 的 non-goals 明确不提供跨机协调、
  多写者 CAS，也不与 Danny 构成跨 store 原子事务。Danny 侧同样只声明
  `same_host_single_writer_sequential_reopen`。按 roadmap「Upstream blocking
  policy」第 6 步，这一条不靠新增 runtime journal 或假事务来“解决”，
  而是把边界写进 deployment scope 与文档。
- **证据**：`internal/agent/baymax/owner_store.go`、
  `internal/agent/baymax/owner_store_test.go`、
  `docs/baymax-contract-evidence.md`、
  `openspec/changes/archive/196-deployable-source-history-store-contract/design.md`。

## 未解决

### continuation 支持仍缺 live gate（Danny 已接入，未通过真实集成）

- **Pin / public contract**：`core/types.ContinuationCapabilities`、
  `ContinuationCapabilitySource`、
  `composer.Composer.ContinuationCapabilities`、
  `teams.Engine.ContinuationCapabilities`、
  `mailbox.Mailbox.ContinuationCapabilities` 与
  `mailbox.NewContinuationWorker`。
- **原 finding**：Danny 代码对新 owner 零引用，child/team 在 capability
  manifest 中为 `unsupported`，mailbox 未声明 handler pause restore。
- **已补齐的部分**：`Client.BindContinuationOwners` 为 child、team、mailbox
  分别绑定 durable owner；`ContinuationCapabilities(ctx, runID)` 返回逐
  form、逐 pause reason 的只读证据矩阵（不创建 checkpoint、不恢复效果）。
  `internal/agent/baymax/continuation_restore_test.go` 覆盖 3 个 form ×
  3 个 pause reason 的“一个进程暂停、另一个进程恢复”：
  child 走 `ResumeChildContinuation`，team 走
  `Engine.ResumeContinuation`，mailbox 走
  `Worker.ResumeHandlerContinuation`；三者都验证重复恢复收敛、
  已完成效果不重跑。
- **仍然缺的部分**：这些是确定性 fixture，不是任务 7.3 的 live gate。没有
  `OPENAI_BASE_URL` / `OPENAI_MODEL` / `OPENAI_API_KEY`，真实
  OpenAI 兼容端点从未被调用。按 roadmap「Upstream blocking policy」第 9 步，
  真实集成路径通过前不能把本条改成 `fixed-upstream`，也不能把
  capability manifest 的 child/team 从 `unsupported` 提升为 `verified`。
  当前 manifest 的 reason 已改为“已接线、复现通过、仅差 live gate”，
  而不是原来的“未接线”。
- **解阻条件（唯一剩余）**：在具备真实凭据的环境中跑通 Milestone 8 的
  live smoke（任务 7.3），逐 form 逐 pause reason 记录同一 pin 下的真实
  端点证据。完成后才更新 manifest 状态与本文定级。
- **证据**：上游归档 194/198、`orchestration/composer/capability.go`、
  `orchestration/teams/continuation.go`、
  `orchestration/mailbox/continuation.go`、
  `internal/agent/baymax/continuation_owners.go`、
  `internal/agent/baymax/continuation_restore_test.go`、
  `internal/runtimeboundary/contracts.go`、
  `docs/baymax-contract-evidence.md`。
- **受影响任务**：1.3、3.4、4.3-4.7、5.2-5.5、7.2-7.3。

## 复核记录

- 2026-11-11：`go get github.com/FelixSeptem/baymax@cca95f31f71b` 生成
  `v0.0.0-20261110150429-cca95f31f71b`；`go build ./...`、`go test ./...`
  与三个 pinned fixture 全部通过。当时两条 finding 都还是“未解决”。
- 2026-11-12：owner store 绑定与 continuation owner 接线完成。
  `go build ./...`、`go vet ./internal/agent/baymax`、
  `go test ./internal/agent/baymax ./internal/tasks ./internal/runtimeboundary
  -count=1` 通过。source history 显式绑定一条转入“已解决”；
  continuation 一条只剩 live gate，不再归因于 Danny 未接线。
