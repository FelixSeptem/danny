# 运行时与上下文所有权

本矩阵是里程碑 8 的产品边界。Danny 保留产品权限与持久业务状态。Baymax 拥有执行
机制。Stele 拥有记忆选择与连续性。公开的上游 API 是集成边界，而不是对 Danny
授权权与提交权的转移。

| 职责 | 所有者 | 公共契约 | 调用方 | 持久化所有者 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 模型 Run/Stream 与已注册工具 | Baymax | `core/runner.Engine.Run`、`Stream`、主机准入 | Danny 执行适配器 | Baymax 来源；Danny 投影 | `docs/baymax-contract-evidence.md` |
| 子任务派发与有界分配 | Baymax | `orchestration/composer.DispatchChild` | Danny 执行适配器 | Baymax 来源；Danny 证据 | `internal/agent/baymax/adapter.go` |
| 团队策略与工作流步骤 | Baymax | `orchestration/teams`、工作流包 | Danny 执行适配器 | Baymax 来源；Danny 证据 | `docs/baymax-contract-evidence.md` |
| 信箱租约、重试、确认与 DLQ | Baymax | `orchestration/mailbox` | 定时/通用执行适配器 | Baymax 信箱存储；Danny 派发意图 | `internal/agent/baymax/scheduled.go` |
| 定时准入与 occurrence 派发 | Danny + Baymax | Danny occurrence 契约加 `orchestration/scheduler` | 定时任务服务 | Danny occurrence/发件箱；Baymax 调度器 | `internal/agent/baymax/scheduled.go` |
| 安全点、检查点 schema 与原生恢复 | Baymax | runner 检查点/控制 API | Danny 恢复适配器 | Baymax 检查点存储加 Danny 绑定 | `docs/baymax-contract-evidence.md` |
| 原生取消、超时与因果重试 | Baymax | `Engine.CancelRun`、`RetryRun` | Danny 控制适配器 | Baymax 来源；Danny 控制证据 | `docs/baymax-contract-evidence.md` |
| 原生事件身份、来源顺序与终态仲裁 | Baymax | `types.EventEnvelope`、主机事件处理器 | Danny 事件投影器 | Baymax 来源；Danny 观测投影 | `internal/runtimeboundary/contracts.go` |
| 来源信任分类与工具授权 | Danny | `knowledge.Policy`、`ToolRegistry` | 产品准入与工具回调 | Danny AppStore | `internal/agent/knowledge/agent.go` |
| 规划选择、能力清单与任务修订版本 | Danny | `runtimeboundary.ExecutionRequest`、规划器 | 产品执行框架 | Danny AppStore | `internal/runtimeboundary/contracts.go` |
| 任务、知识世代（generation）、来源字节与引用 | Danny | 存储仓库 | 知识与任务服务 | Danny AppStore/文件系统知识世代日志 | `docs/storage-durability.md` |
| 产品观测日志与重放游标 | Danny | runtimeboundary 事件投影仓库 | 协议中立的观察者 | Danny AppStore | `openspec/specs/storage-durability/spec.md` |
| 效果账本与应用发件箱 | Danny | 存储效果/发件箱仓库 | 调度器、工具与投递 worker | Danny AppStore | `docs/scheduler-reliable-delivery.md` |
| 记忆摄取、路径与 provenance | Stele | 文档化的记忆/provider API | Stele 适配器 | Stele 存储 | `docs/upstream/stele-findings.md` |
| 记忆检索、排序与选择 | Stele | 文档化的上下文/检索 API | Stele 适配器 | Stele 存储 | `internal/context/stele/adapter.go` |
| 记忆会话、轮次与上下文连续性 | Stele | 文档化的会话/上下文字段 | Stele 适配器 | Stele 存储加 Danny 绑定 | `docs/upstream/stele-findings.md` |
| 最终模型历史压力与原生交接 | Baymax | 上下文装配器与 runner 上下文 API | Danny 执行适配器 | Baymax 来源 | `docs/baymax-contract-evidence.md` |
| 产品上下文信任、角色披露与引用 | Danny | 带版本的 `ContextEnvelope` | 产品上下文装配器 | Danny 准入的投影快照 | `internal/runtimeboundary/context.go` |
| 知识编译与产品聚合 | Danny | 知识任务/知识世代仓库与执行边界 | 知识编译器 | Danny 知识世代提交、效果与发件箱 | `internal/agent/knowledge`、`internal/storage` |

## 边界规则

1. 领域模块使用 Danny 拥有的带版本 DTO。只有适配器包可以导入 Baymax 公共 Go 包
   或解码 Stele HTTP 响应。
2. 只有当某个能力的精确依赖固定版本与可复现的证据测试夹具（fixture）都记录在
   能力清单中时，该能力才可用。
3. 失败或未验证的上游保证记录在相应的中文上游发现文档中，并阻塞依赖它的生产
   工作。
4. Danny 可以投影并持久化上游事实，但不得制造原生事件、原生检查点、原生顺序
   声明、记忆排序或最终模型历史决策。
5. 即便由 Baymax 或 Stele 通过回调执行，产品修订版本、写入作用域、效果命名空间、
   知识世代提交与发件箱仍归 Danny 所有。
