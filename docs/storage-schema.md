# 存储 schema 复核清单

本清单是里程碑 2 规范 schema 的复核产物。迁移执行器在一个带版本号的迁移中创建这些表，并将版本记录到 `schema_migrations`。

## 实体覆盖

- [x] `conversations` 存储带作用域的上下文引用与乐观修订版本。
- [x] `tasks` 存储可变任务状态与单调递增的修订版本。
- [x] `schedules` 存储 cron 表达式、IANA 时区、动作类型、指令、启用状态与修订版本。
- [x] `executions` 存储任务/调度关联、触发实例（occurrence）身份、结果与修订版本。
- [x] `notifications` 存储渠道投递状态与载荷。
- [x] `source_metadata` 存储路径、内容哈希与已提交的来源字节。
- [x] `knowledge_generations` 存储单调递增的知识世代（generation）ID、状态、提交标记与提交时间。
- [x] `artifacts` 存储知识世代身份、类型、内容哈希、字节与血缘。
- [x] `manifests` 存储不可变的知识世代清单。
- [x] `revisions` 为未来实体提供共享的主体修订版本命名空间。
- [x] `idempotency_effects` 存储操作命名空间、主体、效果键、状态、请求与原始结果。
- [x] `leases` 存储所有者、栅栏值、到期时间与心跳元数据。
- [x] `outboxes` 存储带类型的投递意图、载荷版本、稳定幂等键、尝试次数、租约与重试时间。
- [x] `task_harness_executions` 存储持久化任务运行状态、修订版本、计划/能力/投影摘要、关联信息与准入幂等性。
- [x] `task_harness_plans` 存储不可变的计划笔记本版本、权限边界、子执行上限、效果引用与重新规划理由。
- [x] `task_harness_checkpoints` 存储安全点的恢复身份、暂停原因、当前步骤与已完成效果引用。
- [x] `task_harness_replans` 存储计划血缘与权限比较历史。
- [x] `task_harness_evidence` 存储带稳定效果键的父/子终态证据。

## 不变量

- [x] 实体 ID 与知识世代 ID 唯一。
- [x] `(namespace, subject, effect_key)` 在幂等效果中唯一。
- [x] `(kind, idempotency_key)` 在发件箱投递意图中唯一。
- [x] `(generation_id, kind, content_hash)` 在产物身份中唯一。
- [x] `(execution_id, version)` 在不可变计划笔记本条目中唯一。
- [x] `(execution_id, effect_key)` 在可安全重放的执行证据中唯一。
- [x] 执行的终态变更与检查点变更都需要期望修订版本。
- [x] 已提交的知识世代需要非空提交标记。
- [x] 可变任务更新需要期望修订版本谓词。
- [x] 发件箱认领需要处于待处理状态的行，或租约已过期的行。
- [x] 租约替换会递增栅栏值，过期所有者无法释放替换后的租约。
- [x] 文件系统物化只读取已提交的知识世代记录。

## 事务边界

- [x] schema 迁移与 schema 版本记录一起提交。
- [x] 产品状态与其发件箱意图通过 `WithTransaction` 一起提交。
- [x] 效果结果与受保护变更必须共享同一个事务边界。
- [x] 知识世代清单与提交标记一起提交。
- [x] 物化使用原子临时文件重命名，且可以从清单重复执行。
- [x] 执行准入在一个事务中写入执行记录与初始计划笔记本条目。
- [x] 执行证据重放在相同效果键下返回原始结果。
