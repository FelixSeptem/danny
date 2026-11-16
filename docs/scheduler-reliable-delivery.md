# 调度器与可靠投递

## 调度身份

调度项持久化五字段 cron 表达式、IANA 时区、动作类型、重试上限，以及 `skip`、`coalesce_latest` 或有界 `backfill` 中的一种错过触发策略。每个触发实例都会存储调度修订版本、本地民用时间戳、解析后的 UTC 时刻、配置的 tzdata 标签，以及稳定的触发实例键。

夏令时切换期间不存在的本地时间会被跳过。有歧义的本地时间使用较早的 UTC 时刻。该决定会随触发实例一起存储，因此重启不会重新计算出不同的身份。

## 持久化派发

Danny 在同一个 SQLite 或 PostgreSQL 事务中提交触发实例与 `schedule_dispatch` 发件箱意图。重启校正会查询原始幂等键，并通过 Baymax 调度器与信箱适配器重新发布该意图。Danny 不实现 Baymax 的租约、重试、退避、QoS 或死信状态机。

Baymax 载荷携带触发实例键、调度修订版本、策略摘要、计划版本、能力摘要、投影摘要与效果命名空间。信箱 worker 必须把相同的触发实例幂等键传给任务执行框架的准入。

## 调度授权与 Stele 效果

定时工具调用使用 `PolicyScheduled` 与触发实例写作用域。检索到的文档及其他来源文本是不可信数据。它们不能授权 `save_memory`、`create_task`、`commit_compilation` 或 `send_webhook` 这类变更类工具。

定时的 Stele 写入通过公共提供方适配器使用触发实例效果键。适配器在请求元数据与 `Idempotency-Key` HTTP 头中发送该键；Danny 不额外添加本地记忆或引用存储。

## Webhook 投递

Webhook 投递是一条持久化记录，拥有自己的租约、尝试次数、重试时间与终态失败状态。Agent 终态结果与 webhook 意图一起提交。之后的投递失败绝不会改变此前已提交的 Agent 成功或失败结果。
