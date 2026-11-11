# Baymax 上游核查记录

本文记录 `github.com/FelixSeptem/baymax`
`v0.0.0-20261022053159-3d0ab5949790` 在 Danny Milestone 8 边界上的可复现
契约结果。这里的“未解决”表示当前 pin 或部署证据不足，不把 Danny 的适配
缺口误报为上游缺陷。

## 已解决

- 暂无上游实现缺陷复现。Pinned history API 的 source identity、source-local
  顺序、retention floor、旧 cursor 拒绝和同机顺序重开已由 Danny
  `internal/agent/baymax.TestPinnedSourceHistoryOwnersPreserveIdentityOrderAndFileReplay`
  及 Baymax mailbox history 测试覆盖；这些仅验证 source contract，不代表
  Danny 的生产投影事务或多机部署已经完成。

## 未解决

### source history 必须由 owner 显式注入

- **Pin / public contract**：`v0.0.0-20261022053159-3d0ab5949790`；
  `core/types.NativeEventHistoryReader`、`core/history.Journal`、
  `core/runner.WithNativeEventHistory`、composer/teams/workflow 的同名注入
  API。
- **复现**：`TestPinnedSourceHistoryOwnersPreserveIdentityOrderAndFileReplay`
  对 runner/child/team/workflow 分别打开 file-backed source journal，验证
  source identity/sequence、payload omission、retention floor、过期 cursor，
  再在同一 source 和路径顺序重开并 replay。内存 owner journal 的
  `Capabilities.Durable=false`；不注入 owner history 时
  `UnsupportedNativeEventHistoryReader` 返回 `ErrHistoryUnsupported`。Mailbox
  另由 `TestFileMailboxHistoryReplaysAfterRestartWithoutPayload` 和
  `TestMailboxHistoryTerminalRestartProcess` 验证文件重开及新进程 replay。
- **期望 / 实际**：提案要求每个声称可用的 source 都有 source identity、
  bounded replay、retention floor 和 crash recovery。当前 pin 只在显式 owner
  journal 和相应部署下提供这些事实，默认 runner、child、team、workflow、
  mailbox 并不会自动创建一个跨进程、多写者历史库。
- **证据**：`internal/agent/baymax/adapter_test.go`、
  `docs/baymax-contract-evidence.md`、上游 `core/history/journal_test.go`、
  `orchestration/mailbox/history_test.go`。
- **受影响任务**：1.2、1.3、3.2、3.3、4.1、4.3、4.4、4.6、4.7、5.2、
  5.4、7.2、7.3。
- **解阻条件**：为每个声称支持的 source 注入其 owner store，完成 fresh-process
  replay、retention/gap、crash-before-projection 和 terminal recovery 的原始
  reproduction；在证据完成前保持相关任务未勾选，不在 Danny 增加替代 runtime
  journal。

  Source history 的 file reopen 证明只能找回已提交的 source facts；它不证明
  source append 与 Danny projection 原子提交。应用投影提交前崩溃时，必须从同一
  owner source replay 并在产品边界去重；source facts 不可用时进入
  recovery-required，不能重新执行不确定的 parent/child effects。Danny 的跨库
  投影/游标事务仍由任务 3.2-3.3 验收。

### continuation 支持是 path-specific

- **Pin / public contract**：`core/types.ContinuationCapabilities`、runner
  checkpoint/control、workflow continuation，以及 composer README 对 child/
  fanout、team、mailbox、planbridge 的边界说明。
- **复现**：上游 capability fixture 只在显式 checkpoint store、owner namespace
  和支持的 workflow/runner path 下报告 `CrossProcessRestore=true`；child/
  fanout、team、mailbox 和 resumed planbridge 没有同等 restore material 时返回
  unsupported/unavailable。`input_required`、`approval_required`、
  `budget_exhausted` 的完整跨 form 验收尚未建立。
- **期望 / 实际**：提案要求 pause reason、native checkpoint、authority binding
  和 fresh-process restore 逐 path 证明；仅有 DTO/API 存在不能满足该要求。
- **证据**：上游 runner `TestNativeContinuationCrossProcess`、workflow
  `TestEngineResumeContinuationRestoresFromFileStoreOnNewEngine` 与三类 pause
  capability evidence；composer `orchestration/composer/README.md` 明确要求
  child pause 独立证据并声明 team/mailbox/resumed-plan 不支持；mailbox
  `orchestration/mailbox/README.md` 区分 lifecycle replay 与执行恢复；另见
  `core/types/continuation_capabilities_test.go`、`core/checkpoint/store_test.go`
  和 `docs/baymax-contract-evidence.md`。
- **受影响任务**：1.3、3.4、4.3-4.7、5.2-5.5、7.2-7.3。
- **解阻条件**：为每个 mode/pause reason 提交可重放的 source checkpoint 和
  restore 证据；缺失时只记录 typed unsupported/recovery-required，不用应用层
  从头重跑或伪造 checkpoint。
