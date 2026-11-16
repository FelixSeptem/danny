# Agent UI 协议：兼容性说明

状态：2026-11-12（里程碑 9，变更 `agent-ui-protocol-boundary`）。

## AG-UI

- Danny 通过自有的版本化信封（`danny.agent-ui.projection.v1`、`internal/agentui`）投影执行证据，并通过本地适配器将其渲染为与 AG-UI 兼容的线格式（wire）事件。Danny 不嵌入任何 AG-UI SDK，也不导入任何上游（upstream）包。
- 兼容性声明：按当前实现并经测试夹具（fixture）验证，与 AG-UI 兼容。`internal/agentui/testdata/agui-run-fixture.json` 固定了完整一次运行的线格式渲染（生命周期、步骤、文本流与工具调用流，以及已记录的 `danny.*` 自定义事件）。该声明覆盖此测试夹具所展示的事件词汇和顺序语义；它不是协议一致性认证。
- 运行暂停（`input_required`、`approval_required`、`budget_exhausted`）会投影为带 `interrupt` 结果的 `RunFinished`；产品层面的终止状态是 `RunFinished`（成功）和 `RunError`。

## A2UI

- 声明的版本：**仅 A2UI v0.9.1**（`internal/agentui.RenderA2UI`）。v0.9.1 的 React 渲染器在上游被列为稳定（Stable）状态；线格式测试夹具为 `internal/agentui/testdata/surface-fixture.a2ui.json`。
- **A2UI v1.0 不受支持，也不作声明。** 截至 2026-10-09 的核查，并在实现时再次重申：v1.0 处于候选/发布候选（Candidate/Release candidate）阶段，路线图目标为 2026 年第四季度，且没有给出承诺的发布日期；React 渲染器对 v1.0 的支持处于已规划（Planned）状态，尚未发布。在满足以下条件之前，Danny 不得声明支持 v1.0：(a) 规范达到稳定发布版；(b) React 渲染器声明支持 v1.0；(c) 一份渲染后的兼容性测试夹具能在真实渲染器包上通过。与版本无关的 `DynamicSurface`/`SurfaceAction`/`SurfaceSubmission` 模型使未来的 v1.0 适配器保持可增补（additive）。
- MCP-UI/MCP Apps 仍然延后处理；它并不是本地工作台的替代方案。

## 安全边界

- 动态界面（surface）的组件目录仅由服务端注册。模型输出和不可信内容（标签、选项、来源摘录、检索到的记忆、导入的文档）都只是数据；不存在从内容通向组件目录注册、动作创建、工具能力扩展或提交门禁绕过的路径。
- 每一次动态界面提交都会在 Danny 既有的授权与幂等边界（`SurfaceService.Submit` + `idempotency_effects`）上校验执行 ID、动态界面 ID、动态界面修订版本（revision）、动作 ID、用户激活、输入 schema、授权策略、任务修订版本和幂等键。陈旧、重放、未授权、格式错误和不可信的提交都会以带类型的错误失败。

## 证据

- AG-UI 线格式测试夹具：`internal/agentui/testdata/agui-run-fixture.json`
- A2UI v0.9.1 动态界面测试夹具：`internal/agentui/testdata/surface-fixture.a2ui.json`
- 投影/重放契约：`internal/agentui`、`internal/storage/agentui_repo.go`
- SSE 重连/重放与提交收敛：`internal/server/agentui_test.go`
- 针对真实 `@a2ui/react` 包的 React 渲染器集成属于里程碑 10 的工作台任务，已被明确推迟，不作声明。
