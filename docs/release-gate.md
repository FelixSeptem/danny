# 发布门禁证据（里程碑 12）

由 `go run ./cmd/release-gate` 于 2026-11-13 生成。
未执行的检查会连同其原因一并报告为未验证（unverified）；它绝不会被计为通过。

## 场景矩阵

| ID | 场景 | 类别 | SQLite | PostgreSQL | 证据 |
| --- | --- | --- | --- | --- | --- |
| S01 | 导入两篇 Markdown 文档并回答一个带引用的问题 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/knowledge:TestCompilerBuildsIncrementalPlanWithSectionTreeAndSummary` |
| S02 | 将个人偏好保存到 Stele，并在新会话中取回 | 需要实况（live-required） | 未验证：需要实况；使用凭据运行 `cmd/runtime-probe stele-smoke` 以生成证据包 | 未验证：实况场景；第二种存储模式不会改变实况路径 | `cmd/runtime-probe stele-smoke` |
| S03 | 跨会话创建、更新并完成任务，同时进行修订版本（revision）校验 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/scenarios:TestScenarioTaskRevisionAcrossConversations` |
| S04 | 运行每日自然语言摘要计划，并将结果保存到记忆 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/tasks:TestOccurrenceCalculatorStableNormalOccurrence` |
| S05 | 执行一个带失败 Webhook 的固定提醒，并验证各自独立的自动重试状态 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/storage:TestWebhookDeliveryLeaseFencingAndIndependentTerminalStatus` |
| S06 | 借助子 Agent、团队聚合、引用校验和原子知识世代（generation）提交来编译多章节文档 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/knowledge:TestCompileCoordinatorBlocksCommitWhenChildFails` |
| S07 | 触发规划器针对直接、子 Agent、团队、工作流、信箱（mailbox）和定时模式做出的动态选择 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/agent/knowledge:TestPlannerProducesDeterministicPlansAndRejectsUnsupportedModes` |
| S08 | 从执行框架（harness）检查点暂停并恢复一次运行 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/tasks:TestHarnessResumesNativeCheckpointThroughAdapter` |
| S09 | 在每一个已记录的发件箱、租约、物化和 Webhook 边界处注入崩溃，并验证恢复 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/storage:TestFailureInjectorCoversGenerationLeaseEffectAndMaterialization` |
| S10 | 在 SQLite 和 PostgreSQL 模式下运行完整测试套件 | 已执行 | 通过 | 通过 | `cmd/release-gate` |
| S11 | 确定性假模型契约测试，以及兼容 OpenAI 的实况冒烟测试 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/scenarios:TestScenarioDeterministicContractIsReproducible` |
| S12 | 通过解析器服务导入 PDF、DOCX、PPTX、XLSX、XLS、HTML、CSV、TXT 和 Markdown | 已执行 | 通过 | 未验证：解析器矩阵与存储无关 | `parser_service:test_format_matrix` |
| S13 | 验证处于活跃状态的上游（upstream）发现项均有回归测试夹具（fixture），且被阻塞的任务保持阻塞 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/scenarios:TestUpstreamFindingsHaveRegressionEvidence` |
| S14 | 在直接、子任务、团队、工作流、信箱和定时运行过程中重新连接，并验证投影（projection）收敛 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/server:TestSSEReconnectConvergesOnTerminalState` |
| S15 | 渲染受限的 A2UI 动态界面（surface），并拒绝陈旧、重放、未授权、格式错误和不可信的提交 | 已执行 | 通过 | 未验证：本次检查未执行第二种存储模式 | `internal/server:TestSurfaceSubmissionConvergence` |
| S16 | 验证 Baymax 执行恢复，以及 Stele 的有界上下文与重启行为 | 需要实况 | 未验证：需要实况；使用凭据运行 `cmd/runtime-probe live-pause-restore` 以生成证据包 | 未验证：实况场景；第二种存储模式不会改变实况路径 | `cmd/runtime-probe live-pause-restore` |

## 存储模式

- SQLite：通过
- PostgreSQL：通过
- 解析器格式矩阵：通过

## 发布门禁条款

| 条款 | 状态 | 证据 |
| --- | --- | --- |
| 单元、集成和 Docker 端到端测试套件均通过 | 通过 | `internal/scenarios:TestScenarioDeterministicContractIsReproducible` |
| Run 与 Stream 语义在终止边界和聚合边界上等价 | 通过 | `internal/server:TestSSEReconnectConvergesOnTerminalState` |
| 在所声明的功能集合内不存在未解决的、被上游阻塞的依赖 | 通过 | `internal/scenarios:TestNoBlockedUpstreamFindingIsClaimed` |
| 上游发现项记录版本、复现方式、证据、受影响任务和解阻条件 | 通过 | `internal/scenarios:TestUpstreamFindingsHaveRegressionEvidence` |
| 持久性声明由崩溃恢复测试支撑 | 通过 | `internal/storage:TestFailureInjectorCoversGenerationLeaseEffectAndMaterialization` |
| 执行投影、动态界面动作与持久化状态在刷新、重连、重试和重启后保持一致 | 通过 | `internal/server:TestSurfaceSubmissionConvergence` |

## 未验证

- S01（PostgreSQL）：本次检查未执行第二种存储模式
- S02（PostgreSQL）：实况场景；第二种存储模式不会改变实况路径
- S02（SQLite）：需要实况；使用凭据运行 `cmd/runtime-probe stele-smoke` 以生成证据包
- S03（PostgreSQL）：本次检查未执行第二种存储模式
- S04（PostgreSQL）：本次检查未执行第二种存储模式
- S05（PostgreSQL）：本次检查未执行第二种存储模式
- S06（PostgreSQL）：本次检查未执行第二种存储模式
- S07（PostgreSQL）：本次检查未执行第二种存储模式
- S08（PostgreSQL）：本次检查未执行第二种存储模式
- S09（PostgreSQL）：本次检查未执行第二种存储模式
- S11（PostgreSQL）：本次检查未执行第二种存储模式
- S12（PostgreSQL）：解析器矩阵与存储无关
- S13（PostgreSQL）：本次检查未执行第二种存储模式
- S14（PostgreSQL）：本次检查未执行第二种存储模式
- S15（PostgreSQL）：本次检查未执行第二种存储模式
- S16（PostgreSQL）：实况场景；第二种存储模式不会改变实况路径
- S16（SQLite）：需要实况；使用凭据运行 `cmd/runtime-probe live-pause-restore` 以生成证据包
