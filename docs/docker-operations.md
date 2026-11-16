# Docker 运维（里程碑 11）

Danny 以单个环回发布的应用程序容器运行。本文档是 `compose.yaml` 面向运维人员的
契约：构建、拓扑、健康、数据、备份与恢复。

## 前置条件

- Docker Engine 及 Compose 插件。本次变更基于 Docker 29.7.2 与 Compose v5.5.1
  开发。
- 将 `.env.example` 复制为 `.env` 并填入真实值。`.env` 被 Git 忽略；没有任何
  凭据被打入镜像层。

## 命令

| 拓扑 | 命令 |
| --- | --- |
| SQLite（默认） | `docker compose up -d --build` |
| PostgreSQL | `docker compose -f compose.yaml -f compose.postgres.yaml up -d --build` |
| 加入解析器服务 | `docker compose --profile parser up -d --build` |
| 静态配置检查 | `docker compose -f compose.yaml -f compose.postgres.yaml config` |
| 停止（默认） | `docker compose down` |
| 停止（PostgreSQL） | `docker compose -f compose.yaml -f compose.postgres.yaml down` |

PostgreSQL 拓扑是一个 override 文件，而不是 Compose profile 开关。启动、检查
或停止它时必须始终同时传入两个 `-f` 文件；只加载 `compose.yaml` 会让应用停留在
SQLite 上。解析器服务来自里程碑 7 的 `compose.parser.yaml`，因此内部网络上只有
一份解析器定义；该独立文件仍可按 `docs/parser-service.md` 所述的方式使用。

## 拓扑矩阵

| 拓扑 | 启动的服务 | 存储驱动 | 主机端口 |
| --- | --- | --- | --- |
| 默认 | `app` | `sqlite` | `127.0.0.1:8080` |
| PostgreSQL 覆盖配置 | `app`、`postgres` | `postgres` | `127.0.0.1:8080` |
| 解析器 profile | `app`、`parser` | `sqlite` | `127.0.0.1:8080` |

数据库和解析器都不发布主机端口。解析器只能通过内部 Compose 网络在
`http://parser:8090` 访问，该地址也是 `DANNY_PARSER_URL` 的默认值。

## 端口与信任边界

`security.ValidateListener` 在默认 profile 下拒绝非环回监听地址，该策略保持不变。
然而在容器内部，绑定 `127.0.0.1` 会使已发布的主机端口无法访问，因为 Docker 将
发布转发到容器的网络接口，而不是其环回接口。

因此容器 profile 使用显式选择性启用：

- 容器设置了 `DANNY_CONTAINER_LISTEN=true` 以及 `DANNY_LISTEN_ADDRESS=0.0.0.0:8080`。
  `security.ValidateContainerListener` 只接受通配地址，并拒绝具体的非环回 IP。
- `compose.yaml` 以 `127.0.0.1:<host>:8080` 发布端口。如果有任何发布未绑定主机
  地址或绑定了通配主机地址，Go 测试
  `internal/security/compose_publication_test.go` 就会失败。
- 里程碑 10 的同源防护仍然覆盖每一个改变状态的请求，包括 `/agent/` 取消和
  动态界面（surface）动作端点。
- 有意**不**使用 `DANNY_AUTHENTICATED` 来放宽监听范围：该标志背后没有实现任何
  认证，使用它将构成虚假的安全声明。

不要用裸的 `-p 8080:8080` 运行镜像。那会把端口发布到所有主机接口，破坏本次变更
所建立的边界。

## 启动顺序与健康检查

`cmd/personal-agent` 按顺序执行：

1. `config.LoadFromEnv` 与 `Validate`（包括监听策略）。
2. `storage.Open`，然后 `Migrate`（schema v10）。
3. `Reconcile`：释放过期的发件箱租约，并基于已提交的 `knowledge_generations`
   重建派生产物，然后创建产物目录。
4. `ListenAndServe`，在收到 SIGINT/SIGTERM 时优雅关闭。

| 端点 | 含义 |
| --- | --- |
| `GET /api/health` | 未改动的里程碑 10 静态边界标识符。 |
| `GET /api/health/live` | 进程存活；不做依赖检查。 |
| `GET /api/health/ready` | 真实的 `storage.Ping`、产物目录可用性以及已配置的 `storage_driver`；任何失败都返回带类型的 503。 |

Compose 健康检查轮询 `/api/health/ready`。PostgreSQL override 以
`postgres: service_healthy`（`pg_isready`）作为应用的门控条件，因此迁移永远不会
与尚未就绪的数据库竞争。就绪检查失败时存活检查仍保持正常是有意为之：这让依赖
故障保持可诊断，而不是让容器看起来已死。

## 卷与数据

| 卷 | 容器路径 | 内容 | 是否需要备份 |
| --- | --- | --- | --- |
| `danny_data` | `/var/lib/danny/data` | SQLite 数据库（`danny.db`） | 是 |
| `danny_documents` | `/var/lib/danny/documents` | 导入的来源文档 | 是 |
| `danny_artifacts` | `/var/lib/danny/artifacts` | 派生的、可重建的产物 | 否 |
| `danny_parser_staging`、`danny_parser_jobs` | 解析器服务 | 暂存对象与解析器任务 | 可选（进行中的任务） |
| `danny_pgdata` | `/var/lib/postgresql/data` | PostgreSQL 数据（仅 override） | 是 |

知识世代（generation）清单、引用以及任务/执行记录存放在数据库中，因此数据库备份即可覆盖它们。
派生产物可以通过启动校正从已提交的知识世代显式重建，不属于必需备份的一部分。

## 备份与导出

- **SQLite**：停止服务，然后从卷中复制出数据库，例如
  `docker run --rm -v danny_danny_data:/data -v "$PWD/backup:/backup" alpine cp /data/danny.db /backup/danny.db`。
- **PostgreSQL**：`docker compose -f compose.yaml -f compose.postgres.yaml exec postgres pg_dump -U danny danny > backup/danny.sql`。
- **文档与暂存**：用同样的 `docker run --rm -v ...` 方式复制 `danny_documents`
  与 `danny_parser_staging` 卷。
- **Stele 数据**：Stele 拥有的上下文位于上游 Stele 服务中。Danny 只保留有界的
  投影与引用，由 Danny 数据库备份覆盖。请通过 Stele 自身的契约导出上游存储
  （参见 `docs/stele-context-evidence.md` 与 `docs/runtime-context-ownership.md`）；
  本 Compose 项目不拥有也不备份该存储。

## 恢复冒烟流程

1. 启动默认拓扑，等待 `GET /api/health/ready` 报告 `status: ready` 且
   `storage_driver: sqlite`。
2. 清空派生产物卷的内容（或删除并重建该卷），然后重启应用容器。
3. 启动校正会基于已提交的知识世代重建产物。在校正完成之前就绪检查不得成功；
   一次新的 `ready` 响应即确认恢复完成。
4. 在卷保持不变的情况下重复一次停止/启动，以确认数据库和文档在重启后仍然存在。

过期的发件箱租约与待处理工作的恢复由既有的 `internal/storage` 校正契约测试覆盖；
未覆盖的内容见“已知缺口”一节。

## 已知缺口（未声称）

- **独立工作进程。** 路线图列出“在运行时契约要求时”独立的 app、调度器、
  信箱/发件箱、物化器和投递工作进程。当前运行时将这些单元实现为库级的
  `RunOnce` 函数（`WebhookWorker`、`MailboxWorker`、投影 worker），没有独立的
  命令入口点。本次变更交付一个 `app` 容器，而不是添加空转容器，因此“重启每个
  worker”的退出闸门无法字面执行。恢复由启动校正和既有的契约测试覆盖。
- **PostgreSQL 测试套件。** PostgreSQL 模式下的完整仓库套件与端到端套件、在每个
  发件箱/租约/物化/webhook 边界的崩溃注入，以及发布闸门仍属于里程碑 12。
- **认证模式、远程暴露、TLS 与反向代理**超出范围。默认姿态仍为仅环回且无认证。
- **工作台的浏览器端到端验证**仍属于里程碑 12，与里程碑 10 的状态说明一致。

## 验证记录

环境：Docker 29.7.2、Docker Compose v5.5.1、守护进程 29.7.2 可连通；
Go 1.26.8；Windows 主机。以下所有命令均于 2026-11-13 执行。

| 检查项 | 命令 / 证据 | 结果 |
| --- | --- | --- |
| Compose 静态配置 | `docker compose -f compose.yaml config --quiet`、`--profile parser` 以及 `-f compose.yaml -f compose.postgres.yaml config --quiet` | 三种拓扑全部通过 |
| 归一化后的主机绑定 | `docker compose ... config --format json`，按服务逐一检查 | 通过：只有 `app` 发布端口，且 `host_ip=127.0.0.1`；`postgres` 与 `parser` 均不发布端口 |
| PostgreSQL override 合并 | 归一化后的 override 输出 | 通过：`depends_on/postgres/condition=service_healthy`、`DANNY_STORAGE_DRIVER=postgres`、`postgres` scheme URL |
| 应用镜像构建 | `docker compose -f compose.yaml build` | 通过：三个阶段（Node 工作台、`CGO_ENABLED=0` 的 Go 构建、最小运行时），镜像 `danny/personal-agent:local`，约 2 分 21 秒 |
| 运行镜像内容 | `docker run --rm --entrypoint sh danny/personal-agent:local -c ...` | 通过：用户 `danny`（uid 100），`/opt/danny/web/dist` 资产存在，`/src` 不存在，`node`/`npm`/`python`/`python3`/`go`/`gcc` 均不存在 |
| 镜像内的工作台构建 | `npm run build`（Dockerfile 阶段 1） | 通过：`tsc --noEmit && vite build` 产出了 CSS 与 JS 资产 |
| 一条命令启动 SQLite | `docker compose up -d --build` | 通过：`danny-app-1` 达到 `healthy` |
| 就绪 / 存活 | `GET /api/health/ready`、`/api/health/live`、`/api/health` | 通过：ready 返回 `{"status":"ready","storage_driver":"sqlite","artifacts_ready":true}`；live 与里程碑 10 的静态响应未变 |
| 重启持久性 | `docker compose restart app` | 通过：`danny.db`（344064 字节）完好保留在 `danny_data` 中，ready 有返回 |
| 派生产物恢复 | 删除 `/var/lib/danny/artifacts/*` 后重启 | 通过：约 2 秒内就绪，且启动校正已运行。注意事项：已提交的知识世代集合为空，因此这次检验的是校正路径，而不是用真实知识世代内容进行重建 |
| PostgreSQL 启动 | `docker compose -f compose.yaml -f compose.postgres.yaml up -d` | 通过：`danny-postgres-1 Healthy` 出现在 `danny-app-1 Starting` 之前；ready 返回 `storage_driver: postgres`；两个容器均健康；数据库端口未发布 |
| Go | `gofmt -l`、`go vet ./...`、`go build ./...`、`go test ./... -count=1` | 每个包均通过，包括 Compose 发布测试 |
| 定向 lint | `golangci-lint run ./internal/config/... ./internal/security/... ./internal/server/... ./cmd/personal-agent/...` | 通过：0 个问题 |
| OpenSpec | `openspec validate --all --strict` | 12/12 通过 |
| 空白字符 | `git diff --check` | 通过 |
| 容器清理 | `docker compose ... down` | 通过：没有 `danny-*` 容器残留运行 |

第一次构建尝试失败，因为一条多余的 `# syntax=docker/dockerfile:1` 指令使 BuildKit
从无法访问的仓库拉取前端镜像。该指令已被移除（Dockerfile 未使用任何 BuildKit
专有语法），随后构建通过。失败的尝试记录在此，而不是被隐藏。

### 未验证 / 延期

| 项目 | 原因 |
| --- | --- |
| PostgreSQL 模式下的完整仓库套件与端到端套件 | 里程碑 12；此处仅验证了启动、迁移与就绪 |
| 在每个发件箱/租约/物化/webhook 边界的崩溃注入 | 里程碑 12 |
| 用真实的已提交知识世代重建产物 | 校正路径已运行，但测试数据库中没有已提交的知识世代；完整重建是里程碑 12 的场景 |
| “重启每个 worker”退出闸门 | 无法执行：运行时没有独立的 worker 进程（见“已知缺口”） |
| 实时浏览器中的工作台端到端测试与渲染器认证 | 里程碑 12 |
| A2UI 1.0 | `a2ui-1-0-upgrade` 处于受阻状态，等待真实的 `@a2ui/react` 1.0 发布 |
| 全仓库 `golangci-lint run ./...` | 未运行：该沙箱在完整 lint 时会超时（里程碑 10 中记录的已知本地限制）。针对每个变更包的定向 lint、`go vet` 与完整测试套件已覆盖变更代码 |
