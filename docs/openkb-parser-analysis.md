# OpenKB 文件解析与运行时依赖分析

本文基于当前临时分析副本 `.tmp/openkb` 的源码、`pyproject.toml`、`uv.lock` 以及命令行和 REST API 文档整理。`.tmp/openkb` 仅用于分析，不属于 Danny 的交付内容。

## 结论

OpenKB 支持的核心格式是 PDF、Markdown、纯文本、CSV、HTML、DOCX、PPTX、XLSX 和 XLS。其主要实现是 Python 解析库组合，当前源码没有发现对 Pandoc、LibreOffice/soffice、Tesseract、pdftotext、Ghostscript 或其他系统级文档 CLI 的调用。

因此，Danny 不应把“安装一组系统 CLI”作为默认解析方案。更合适的边界是独立的 parser service：Go 主服务只依赖一个稳定的解析契约，Python parser service 在自己的容器中携带 OpenKB 风格的解析库。长 PDF 的 PageIndex Cloud 作为可选云能力，不应成为本地模式的硬依赖。

## 支持矩阵

| 输入 | OpenKB 路径 | 主要依赖 | 本地是否可用 | 外部服务 |
| --- | --- | --- | --- | --- |
| `.md`, `.markdown` | 直接读取 Markdown，复制相对图片 | Python 标准库 | 是 | 无 |
| `.txt` | `MarkItDown` 转换 | `markitdown`、`charset-normalizer`、`beautifulsoup4` 等 | 是 | 无 |
| `.csv` | `MarkItDown` 转换 | `markitdown` | 是 | 无 |
| `.html`, `.htm` | `MarkItDown` 转换；URL 输入先用 Trafilatura 提取正文 | `markitdown`、`trafilatura` | 是 | URL 抓取需要网络 |
| `.docx` | `MarkItDown` Office extra | `mammoth`、`lxml` | 是 | 无 |
| `.pptx` | `MarkItDown` Office extra | `python-pptx` | 是 | 无 |
| `.xlsx` | `MarkItDown` Office extra | `openpyxl`、`pandas` | 是 | 无 |
| `.xls` | `MarkItDown` Office extra | `xlrd`、`pandas` | 是 | 无 |
| 短 `.pdf` | PyMuPDF 文本和图片提取 | `pymupdf` | 是 | 无 |
| 长 `.pdf` | PageIndex 本地索引；云 key 存在时优先云端 OCR，失败回退本地 | `pageindex`、`pymupdf` | 是 | 可选 PageIndex Cloud |
| PageIndex `doc_id` | 拉取结构和页面内容后在本地编译 | `pageindex` | 否 | 必须 PageIndex Cloud |

当前 CLI 的 `SUPPORTED_EXTENSIONS` 为：`.pdf`、`.md`、`.markdown`、`.docx`、`.pptx`、`.xlsx`、`.xls`、`.html`、`.htm`、`.txt`、`.csv`。

## 解析链路

普通文件的统一转换入口是 `openkb/converter.py`：

```text
source file
  -> hash registry / raw copy
  -> Markdown conversion
  -> image extraction
  -> wiki/sources/<doc>.md
  -> section tree / summaries / concepts / entities
```

Markdown 文件走直接读取和相对图片复制；PDF 走 PyMuPDF；其他支持格式通过懒加载的 `MarkItDown` 转成 Markdown。MarkItDown 的懒加载是有意的，因为其导入链会加载 Magika 和 ONNX Runtime，体积较大且会增加导入耗时。

PDF 在转换前使用 PyMuPDF 计算页数。页数小于 `pageindex_threshold`（默认 20）时直接生成 Markdown；达到阈值时进入 PageIndex 长文档路径。长文档在未设置 `PAGEINDEX_API_KEY` 时本地处理；设置 key 时可以请求 PageIndex Cloud OCR 和页面内容，云端失败后回退到本地 PyMuPDF。

URL 输入不是通用下载器：PDF 通过标准库 `urllib` 流式下载并进行 magic-byte 检查；HTML 通过 Trafilatura 获取正文并输出 Markdown，避免把导航、页脚和 Cookie 提示直接交给模型。

## 外部工具核查

对 `openkb` 源码进行 `subprocess`、`Popen`、`shutil.which`、`os.system`、`pandoc`、`libreoffice`、`soffice`、`tesseract`、`pdftotext`、`ghostscript` 等检索，未发现文档解析链路调用系统命令。源码中的 `subprocess` 仅出现在技能市场相关功能和 CLI 输出说明，不属于文件转换路径。

这不等于所有底层 Python 依赖都不含原生代码：PyMuPDF、ONNX Runtime、部分 Office 依赖可能携带平台二进制 wheel。Docker 镜像仍需要选择兼容的 Python 基础镜像和架构，并在构建阶段安装这些 Python wheels；不需要额外安装 LibreOffice 或 Tesseract 才能复现 OpenKB 当前解析链路。

## 对 Danny 的边界决策

### Parser service

Danny 主服务保持 Go。parser service 使用 Python，接收受限大小的文件或受控临时文件引用，返回规范化 Markdown、页信息、图片引用、检测到的格式、解析器版本和警告。编译、索引、Stele context 和 Baymax Agent 不直接依赖 Python 包。

### 明确的模式

- `local`: Markdown/TXT/CSV/HTML/Office/PDF 使用本地 Python 依赖。
- `pageindex-cloud`: 只有配置 `PAGEINDEX_API_KEY` 时启用，记录 provider、请求状态和回退原因。
- `unsupported`: 对未知扩展名、解析器未安装或超过资源限制返回结构化错误，不把原始内容伪装成已解析知识。

### Docker

默认应用镜像不应把解析器和 Go API 强行合并。建议增加独立 `parser` service 和健康检查；Office/PDF 依赖只放在 parser 镜像。PageIndex Cloud 凭据通过 parser service 的环境变量注入。默认 Compose 仍绑定 loopback，并为 parser 设置 CPU、内存、并发、单文件大小、页数、超时和临时目录配额。

### 安全和可靠性

原始文件和解析出的 Markdown 都是 untrusted data。解析结果只能作为 Baymax context 的数据输入，不能授权 Agent 调用写工具。parser 必须返回稳定的 `document_id`、`content_hash`、`parser_version` 和 `source_fingerprint`，便于生成提交、重试、审计和重解析。解析失败必须是持久化状态，不能依靠 HTTP 请求生命周期中的临时错误。

## 下一份 OpenSpec 的范围

下一份提案使用 `document-ingestion-parser-boundary`，先交付 parser service contract、支持矩阵、资源和失败语义、Docker profile 以及与 KnowledgeStore generation 的输入边界。HTTP API/Web workbench 提案后置，只消费该契约，不在 Web 层隐式实现 Office/PDF 解析。

## Danny 与 parser service 的交互

Danny 的生产部署使用 HTTP/1.1 + JSON。Go 先把源文件写入受控 staging root，再通过 `staging_object_id`、内容 hash、格式提示和幂等键提交 parser job。Python parser 只能解析配置 root 下的 opaque object ID，不接收任意宿主机路径。

Python parser 同时提供 CLI 入口供本地调试、离线 fixture 和 CI 使用。CLI 与 HTTP 适配器共享同一个 parser core 和结果模型；CLI 不是 Docker 跨容器生产通信协议。
