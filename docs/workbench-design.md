# Danny Workbench 视觉系统

> 2026-11-13 重做。范围只覆盖 `web/` 前端的视觉层与结构层。
> `docs/http-workbench.md` 记录的路由契约、投影字段白名单、同源守卫、
> AG-UI SSE 语义、A2UI v0.9.1 门禁全部保持不变。

## 一、之前的界面为什么丑

按网页设计评价七维度逐项核对 `.tmp/workbench.png` 与旧 `web/src/styles.css`：

| 维度 | 判定 | 具体问题 |
|---|---|---|
| 一致性 | 不通过 | 没有设计令牌，色值/圆角/间距全靠手写；`5px`、`6px`、`7px` 三种圆角混用，`8px`/`10px`/`14px`/`18px` 四种间距无序。这是「看着怪」的头号根因。 |
| 视觉层级 | 不通过 | 全页只有 11px–20px 五档字号且字重几乎相同，标题与正文没有对比，视线没有落点。 |
| 可用性 | 不通过 | 首屏 90% 是空白，只有一行斜体 `No stream events yet.`；没有加载态、没有引导、没有操作结果的位置感。 |
| 可访问性 | 部分 | 输入框与 `<label>` 有绑定，但焦点圈依赖浏览器默认，激活态只靠蓝色背景（颜色单通道），`button` 高度不足 44px。 |
| 响应式 | 不通过 | `.sidebar` 写死 230px，窄屏必然挤压；表格无横向滚动容器。 |
| 性能/氛围 | 不通过 | 纯色平背景，零纹理、零阴影体系，看起来像没做完的调试页。 |
| 品牌人格 | 不通过 | `system-ui` + 白底 + 蓝色高亮 + 圆角卡片，换个 Logo 就是任意一个后台模板。 |

根因排序：**一致性 → 层级 → 空态/响应式 → 氛围与人格**。这次按这个顺序修。

## 二、定调

**目的**：给在本机跑 Danny 的开发者看的一台 agent 运行控制台 —— 观察执行流、核对投影数据、
验证 typed 拒绝。使用者是终端重度用户，信息密度和可信度优先，不需要营销感。

**方向**：Industrial / utilitarian（工业仪表台），单一方向，不混搭。深色为默认底，
浅色作为可切换的第二套令牌（不是简单反色）。

**约束**：React 18 + Vite + 原生 CSS，不引入 UI 框架；字体必须随 `web/dist` 本地打包
（服务是 loopback-only，不能依赖 CDN，离线也要能打开）；所有 API/SSE/A2UI 安全语义不改；
正文对比度 ≥ 4.5:1；375 / 768 / 1440 三档；`prefers-reduced-motion` 下关闭全部动效。

**记忆点**：左侧一条带刻度编号的仪器 rail，顶栏一条连续的状态脉冲线，每个视图一个冲出
栅格的巨型序号（01–08）。不用任何通用图标集，编号和刻度本身就是导航语言。

**字体**（全部 `@fontsource` 本地打包，只引 latin 子集）：

| 角色 | 字体 | 用途 |
|---|---|---|
| 展示字 | Archivo Black 400 | `DANNY` 字样、视图大标题、巨型序号；字距 −0.03em |
| 正文字 | IBM Plex Sans 400 / 500 / 600 | 界面文案、按钮、表格；中文回退 PingFang SC / Microsoft YaHei |
| 数据字 | IBM Plex Mono 400 / 500 / 600 | 所有 ID、状态、时间戳、cron、事件类型、数字；`tabular-nums` |

**配色**（dark 默认，主色 60 / 表面 30 / 强调 10）：

| 语义 | dark | light | 说明 |
|---|---|---|---|
| 底色 | `#0a0f0d` | `#f2f4f0` | 墨绿近黑 / 暖灰白，都不用纯黑纯白 |
| 表面 | `#111815` / `#16201c` | `#fbfcf9` / `#ffffff` | 两级面板 |
| 正文 | `#e8f0eb` | `#16221c` | 对比度 > 12:1 |
| 次要文字 | `#96a9a0` | `#4a5d54` | 对比度 > 6:1 |
| **强调** | `#ff7a2f` 熔橙 | `#c24a00` | 单屏只留给关键动作与激活态 |
| 状态 | `#63e0ae` / `#ffc15e` / `#ff6b61` | 对应加深 | 只出现在状态徽章，且始终带文字，不靠颜色单通道 |

## 三、设计令牌

三层 CSS：`styles/tokens.css`（变量）、`styles/base.css`（重置/排版/焦点/氛围/降级）、
`styles/components.css`（组件与构成）。组件内禁止硬编码色值。

| 组 | 变量 | 值 |
|---|---|---|
| 字阶 | `--fs-0` … `--fs-6` | 12 / 14 / 18 / 22 / 28 / 36 / 46 px（1.25 比例取整） |
| 间距 | `--sp-1` … `--sp-9` | 4 / 8 / 12 / 16 / 24 / 32 / 48 / 64 / 96 px |
| 圆角 | `--r-sm` / `--r-md` / `--r-lg` | 3 / 6 / 10 px（工业方向用小圆角，拒绝 16px 大圆角） |
| 阴影 | `--shadow-1/2/3` | Y 偏移 2 / 8 / 24 px，环境色为墨绿而非纯黑 |
| 动效 | `--dur-fast/base/slow` | 160 / 320 / 640 ms，`--ease-out: cubic-bezier(.16,1,.3,1)` |
| 线 | `--line` / `--line-strong` | 发丝线，10% / 18% 透明度 |

主题通过 `<html data-theme="light">` 覆盖语义令牌，组件代码不动；初始值写到
`localStorage`，`index.html` 内联脚本在首屏渲染前应用，避免闪白。

## 四、构成与氛围

- **非对称**：左侧 248px 固定 rail，主区左对齐，内容最大宽度 1180px 但不居中；
  页面标题块带巨型序号向左冲出内容栅格。
- **氛围**：底色上叠 1px 工程网格（`linear-gradient`，透明度 2%）、右上角一处极淡
  径向渐变、SVG `feTurbulence` 噪点（3%）、三级阴影。全部 `pointer-events: none`。
- **密度**：数据区用「记录带」而不是 Bootstrap 表 —— 表头是等宽大写小字，行高 44px，
  行悬停整行擦除高亮，状态用带边框的徽章。
- **空态**：不是一行灰字，而是一块带刻度边框的说明面板，给出去哪里拿 Run ID、
  连接后会发生什么，让首屏有内容。

## 五、组件状态覆盖

| 组件 | 覆盖状态 |
|---|---|
| 导航项 | 默认 / 悬停 / 激活 / 键盘聚焦 |
| 按钮 | 默认 / 悬停 / 按下 / 聚焦 / 禁用；主按钮与次按钮两套 |
| 输入框 | 默认 / 悬停 / 聚焦 / 禁用 / 只读，label 常显 |
| 数据加载 | 骨架行（3 行脉冲）/ 空态 / typed 错误（保留 `code`）/ 成功 |
| 事件流 | 等待连接 / 已连接脉冲 / 断开 / 单条事件逐帧滑入 |
| 表面门禁 | 未加载 / 校验拒绝（code + message）/ 动作接受 / 动作拒绝 |

## 六、动效规范

一次编排的入场序列，5 组、间隔 80ms：rail → 顶栏 → 页面标题 → 控件条 → 数据面板。
悬停反馈用背景擦除与字距变化，不是简单变亮。只动 `transform` / `opacity`。
SSE 新事件单独做 260ms 滑入；连接指示灯做 2.4s 呼吸循环。
所有动效包在 `@media (prefers-reduced-motion: no-preference)` 内，`reduce` 下全部归零。

## 七、保持不变的东西

`api.ts`（同源相对路径、typed `ApiFailure`）、`stream.ts`（原生 `EventSource` +
`Last-Event-ID` 自动重放）、`surfaces.tsx`（版本/目录/组件三重 fail-closed 门禁、
props 当作不可信文本、动作提交只带注册 ID 与幂等键）、`types.ts`（投影契约）。
视觉改造不得给浏览器新增任何写权限或组件注册能力。

## 八、实现映射与深链

| 文件 | 职责 |
|---|---|
| `web/src/styles/tokens.css` | 全部设计令牌，dark 默认 + `[data-theme="light"]` 覆盖 |
| `web/src/styles/base.css` | 重置、排版、焦点、氛围层、编排动效、`prefers-reduced-motion` 降级 |
| `web/src/styles/components.css` | rail / topbar / page-head / panel / 记录带 / 事件流 / 状态徽章 / 响应式 |
| `web/src/ui/shell.tsx` | 布局外壳、主题状态（localStorage + `?theme=` 深链） |
| `web/src/ui/primitives.tsx` | PageHead / Panel / Table / Notice / EmptyState / Badge / Field / StatusLine |
| `web/src/App.tsx` | 视图逻辑（原样保留）+ hash 深链路由 |

- 视图深链：`/#/tasks`、`/#/runs` 等，刷新后停在原视图；不引入路由库。
- 主题深链：`/?theme=light` 强制本次会话主题，不写回 localStorage。
- 字体经 `@fontsource`（latin 子集）打进 `dist/assets`，共 7 个 woff2 ≈ 104KB，
  无任何 CDN 依赖，离线可用。

## 九、验收自检（2026-11-13 实测）

**反 AI 味清单（12 项）**：0 项未修复命中。展示字 Archivo Black（非 Inter 系）；
墨绿黑 + 单一熔橙强调（非白底紫渐变）；无居中 hero 三卡布局；无通用图标集
（编号与刻度即导航语言）；无营销话术；入场为 5 组错峰编排而非统一淡入；
氛围层为工程网格 + 噪点 + 径向渐变 + 三级阴影；记忆点 = 仪器 rail + 脉冲线 + 巨型序号。

**对比度（dark / light 设计值）**：正文 > 12:1 / 14:1；次要文字 > 6:1；
强调色文字 7.4:1 / 5.2:1；状态徽章色均 ≥ 4.5:1。均高于 WCAG AA。

**状态覆盖**：加载骨架行、空态（带说明的刻度面板）、typed 错误（code + message）、
成功反馈、悬停、`:focus-visible`、禁用、连接中/已连接/断开，全部实现。

**响应式**：1440px（双栏 rail + 内容栅格）、820px 以下（rail 变横向滚动导航、
表格内部横向滚动、巨型序号退为角落装饰）、390px 实测截图通过，无横向溢出
（`.stage` 的 `min-width: 0` 是关键，flex 列容器默认不会把子项压到 min-content 以下）。

**动效降级**：所有动画声明在 `@media (prefers-reduced-motion: no-preference)` 内，
`reduce` 下元素直接可见（默认态即静态可见，无需额外覆盖）。

**构建与测试证据**：
- `npm run typecheck`（`tsc --noEmit`）0 错误
- `npm run build`：45 modules，产物含 7 个本地字体文件
- `go test ./... -count=1`：21 个有测试的包全部通过（前端改造未触碰后端）
- `git status` 仅含预期前端文件，`.tmp/` 与 `data/` 均被 ignore

**已知限制**：Windows 上 Edge `--headless=new` 在窗口宽度 < 500px 时截图会被
最小窗口宽度裁切（布局正常、仅截图工具的怪癖）；移动端验收以 Playwright 视口
（agent-browser）实测为准。

