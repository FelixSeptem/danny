# 存储持久性

里程碑 2 以已配置的数据库作为已提交应用状态的权威来源。

## 驱动

本地配置档使用 SQLite：

```powershell
$env:DANNY_STORAGE_DRIVER = "sqlite"
$env:DANNY_DATABASE_URL = "data/danny.db"
go run ./cmd/personal-agent
```

PostgreSQL 使用相同的仓储契约与迁移集合：

```powershell
$env:DANNY_STORAGE_DRIVER = "postgres"
$env:DANNY_DATABASE_URL = "postgres://user:password@127.0.0.1:5432/danny?sslmode=disable"
go run ./cmd/personal-agent
```

启动时会应用带版本号的迁移，然后在任何后续持久化 worker 被允许运行之前，先对已提交的知识世代（generation）执行校正。迁移或校正出错都会以失败关闭（fail closed）的方式终止。

## 事务边界

需要外部工作的状态变更会在同一个数据库事务中写入一行持久化发件箱记录。发件箱记录共有三种类型：

- `schedule_dispatch`：派发到 Baymax 信箱；
- `stele_projection`：投影到 Stele 上下文；
- `webhook_delivery`：通知投递。

发件箱记录带有稳定的幂等键、载荷版本、重试状态与租约归属。崩溃之后发布方可能会重复投递；下游适配器在每次尝试中都会收到同一个键。

可变实体使用期望修订版本（expected revision）。过期写入会返回修订版本冲突，无法覆盖更新的写入。worker 租约使用栅栏值（fencing value），因此过期的 worker 无法在替换所有者认领该工作之后提交。

## 知识世代

来源字节、生成的产物、血缘与清单都提交到数据库中。知识世代（generation）只有在其提交标记持久化之后才可见。产物目录下的文件是派生的物化结果。删除该目录是可恢复的：

```powershell
go run ./cmd/personal-agent
```

启动校正是从每个已提交知识世代的清单重建物化产物。它不会发布仍处于暂存状态的知识世代。

## 任务执行框架

任务执行框架使用相同的 SQLite/PostgreSQL 仓储边界。准入会在调用 Baymax 之前，一起提交执行记录与初始计划笔记本条目。计划版本不可变；检查点、重新规划（replan）以及父/子证据都会保留，用于重启恢复。每次执行变更都使用期望修订版本，证据使用以执行为作用域的效果键，因此重放的终态写入会返回原始结果。

持久化执行状态机支持准入、派发、运行中、暂停、重试、重新规划、需要恢复（recovery-required）、阻塞与终态。执行框架的首个版本支持 `direct` 与有界的 `subagent_fanout`；其他 Baymax 形态在其路线图边界实现之前，会返回带类型的不支持模式结果。

## 验证

使用以下命令运行 SQLite 契约测试套件：

```powershell
$env:GOCACHE = (Join-Path (Get-Location) '.go-cache')
$env:GOMODCACHE = (Join-Path (Get-Location) '.go-mod-cache')
go test ./internal/storage -count=1
```

同一套仓储契约测试套件计划在 PostgreSQL 配置可用时，针对一次性的 PostgreSQL 数据库运行。Baymax 信箱与 Stele 投影行为仍由其适配器负责；缺少上游确认（acknowledgement）或幂等契约时，必须记录在对应的 `docs/upstream/*-findings.md` 文件中，并阻塞依赖该契约的 worker。
