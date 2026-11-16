# 解析器服务

Danny 使用共享的 Python 解析器核心，提供两个适配器：

- `danny-parser-http` 是生产适配器。它在内部 Compose 网络上提供带版本号的 HTTP/JSON 服务。
- `danny-parser` 是本地、CI 与诊断用适配器。它读取 JSON 请求文件并写出 JSON 结果文件。

两个适配器使用相同的暂存对象、内容哈希、规范化 Markdown、来源信息、警告、失败与终态状态模型。

## HTTP 契约

当前契约为 `v1`：

```text
POST /v1/parser/jobs
POST /v1/parser/urls
GET  /v1/parser/jobs/{job_id}
GET  /v1/parser/jobs/{job_id}/result
POST /v1/parser/jobs/{job_id}/cancel
GET  /v1/health/live
GET  /v1/health/ready
GET  /v1/capabilities
```

作业请求包含不透明的 `staging_object_id`、来源指纹、内容哈希、文件名、格式提示、解析器策略与幂等键。它永远不会包含任意的主机文件系统路径。

## 本地 CLI

```powershell
python -m parser_service.cli `
  --request request.json `
  --result result.json `
  --staging-root data/staging
```

CLI 退出码映射到解析器终态：

| 退出码 | 状态 |
| ---: | --- |
| 0 | `succeeded` |
| 1 | `failed` |
| 10 | `unsupported` |
| 11 | `resource_limited` |
| 12 | `timeout` |
| 13 | `cancelled` |
| 20 | `provider_failed` |

## Docker 配置档

解析器的 profile 定义在 `compose.parser.yaml` 中：

```powershell
docker compose --profile parser -f compose.parser.yaml up --build
```

解析器默认不发布主机端口。应用可以通过内部 Compose 网络访问它。默认的应用监听仍仅限环回地址。

解析器镜像不安装 Pandoc、LibreOffice、Tesseract 或 `pdftotext`。它从 `parser_service/pyproject.toml` 安装固定版本的、源自 OpenKB 的 Python 依赖。

## 提供方模式

- 安装相应的 Python wheel 之后，本地 Markdown、文本、CSV、HTML、Office 与 PDF 转换始终可用。
- 长 PDF 的 PageIndex Cloud 增强通过 `PAGEINDEX_API_KEY` 与解析器策略按需启用。
- 云端失败会被记录，并可回退到本地 PDF 提取。
- 来源文本与提取出的 Markdown 在 KnowledgeAgent 上下文装配之前始终被标记为不可信数据。
