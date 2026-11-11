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
下面两条 finding 已按原始 reproduction 在新 pin 上复核，结论见各条。

## 已解决

- 暂无上游实现缺陷复现。Pinned history API 的 source identity、source-local
  顺序、retention floor、旧 cursor 拒绝和同机顺序重开由 Danny
  `TestPinnedSourceHistoryOwnersPreserveIdentityOrderAndFileReplay` 覆盖；
  该测试与 `go test ./...` 已在 `cca95f31f71b` 上重跑通过。这些仅验证
  source contract，不代表 Danny 的生产投影事务或多机部署已经完成。

## 未解决

### source history 必须由 owner 显式注入（部分收敛，未解阻）

- **Pin / public contract**：`v0.0.0-20261110150429-cca95f31f71b`；
  `core/history.OpenOwnerStore`、`OwnerStoreOptions`、
  `OwnerStore.Capabilities`、`ReconcileProjection`，以及
  runner/composer/teams/workflow/mailbox 的注入 API。
- **新 pin 复核**：新增 Danny fixture
  `internal/agent/baymax/pinned_upstream_contract_test.go`。
  `TestPinnedOwnerStoreRequiresExplicitDurableBinding` 验证未注入 store 返回
  `ErrHistoryUnsupported`、跨机 profile 返回 `ErrDeploymentUnsupported`、
  文件 backed store 只声明 `same_host_single_writer_sequential_reopen`；
  `TestPinnedOwnerStoreReplaysCommittedFactAfterFreshOpen` 验证新的 owner
  绑定（等价于新进程）可读回已提交 source fact、重复 append 收敛、冲突
  append 返回 `ErrHistoryConflict`、projection reconciliation 幂等。
  上游归档 196 的 non-goals 明确：不提供新数据库、跨机协调、多写者 CAS，
  也不与 Danny 构成原子事务。
- **期望 / 实际**：提案要求每个声称可用的 source 都有 source identity、
  bounded replay、retention floor、crash recovery 和跨产品边界的原子投影。
  当前 pin 把“显式 owner 绑定 + fail-closed + projection ledger”做成了公共
  seam，比旧 pin 的分散注入更可验证；但仍只在同机单写者（或带 coordinator
  的同机多实例 CAS）部署下成立，source append 与 Danny projection 之间仍无
  事务。
- **证据**：`internal/agent/baymax/pinned_upstream_contract_test.go`、
  `docs/baymax-contract-evidence.md`、上游 `core/history/deployment_test.go`、
  `openspec/changes/archive/196-deployable-source-history-store-contract/design.md`。
- **受影响任务**：1.2、1.3、3.2、3.3、4.1、4.3、4.4、4.6、4.7、5.2、
  5.4、7.2、7.3。
- **解阻条件（未满足）**：Danny 生产路径接入 `OpenOwnerStore` 绑定，完成
  fresh-process replay、retention/gap、crash-before-projection 与 terminal
  recovery 的原始 reproduction，并在 deployment scope 内声明支持范围。
  在此之前不勾选相关任务的可用性声明，不新增替代 runtime journal。

  Source history 的 file reopen 只能找回已提交的 source facts；它不证明
  source append 与 Danny projection 原子提交。应用投影提交前崩溃时，必须从
  同一 owner source replay 并在产品边界去重；source facts 不可用时进入
  recovery-required，不能重新执行不确定的 parent/child effects。

### continuation 支持仍是 path-specific（上游已补契约，Danny 未接入）

- **Pin / public contract**：`core/types.ContinuationCapabilities`、
  `ContinuationCapabilitySource`、
  `composer.Composer.ContinuationCapabilities`、
  `teams.Engine.ContinuationCapabilities`、
  `mailbox.Mailbox.ContinuationCapabilities` 与
  `mailbox.NewContinuationWorker`。
- **新 pin 复核**：上游 194/198 及 team continuation 归档了 child、team、
  mailbox handler 的 `input_required`、`approval_required`、
  `budget_exhausted` 分原因 capability evidence（例：composer capability
  引用 `TestComposerRestoresChildContinuationWithFreshOwnersForEachPauseReason`），
  且只在配置 file owner store、history、resumer 和 revalidator 后宣告支持。
  Danny fixture `TestPinnedContinuationOwnersFailClosedWithoutConfiguredRestore`
  验证未配置 owner 返回 `ErrCheckpointUnsupported`、部分 pause matrix 不能
  通过 `Validate()`。但 Danny 代码对这些新 owner 零引用，
  `internal/runtimeboundary/contracts.go` 中 child/team 仍为 `unsupported`，
  mailbox 仍注明 agent pause restore 不受支持。
- **期望 / 实际**：提案要求 pause reason、native checkpoint、authority
  binding 和 fresh-process restore 逐 path 在 Danny 的真实集成路径上证明。
  当前 pin 上游已提供公共契约与 fail-closed 语义，但 Danny 未接入、未跑
  真实集成，且声明的 deployment scope 仍是同机单写者。
- **证据**：上游归档 194/198、`orchestration/composer/capability.go`、
  `orchestration/teams/continuation.go`、`orchestration/mailbox/continuation.go`、
  `internal/agent/baymax/pinned_upstream_contract_test.go`、
  `docs/baymax-contract-evidence.md`。
- **受影响任务**：1.3、3.4、4.3-4.7、5.2-5.5、7.2-7.3。
- **解阻条件（未满足）**：在 Danny 生产 adapter 中接入并逐 form/pause
  reason 重放 source checkpoint 与 restore 证据，同步 capability manifest
  与 `docs/baymax-contract-evidence.md` 的矩阵。按 roadmap 的「Upstream
  blocking policy」第 9 步，只有真实集成路径通过后才把本条改为
  `fixed-upstream`。缺失时只记录 typed unsupported/recovery-required，
  不用应用层从头重跑或伪造 checkpoint。

## 复核记录

- 2026-11-11：`go get github.com/FelixSeptem/baymax@cca95f31f71b` 生成
  `v0.0.0-20261110150429-cca95f31f71b`；`go build ./...`、`go test ./...`
  与三个新增 pinned fixture 全部通过。两条 finding 状态保持“未解决”，
  不因上游补齐公共契约而自动解阻。
