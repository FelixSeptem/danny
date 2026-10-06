# Baymax 上游问题记录

本文记录 Danny 集成 Baymax 时发现的、能够稳定复现的问题和改进请求。

## 状态值

- `observed`：已观察到问题，尚未完成上游问题确认。
- `upstream-issue-filed`：已确认属于上游并提交给 Baymax 维护者。
- `blocked-upstream`：依赖该契约的 Danny 开发任务必须阻塞。
- `fixed-upstream`：上游已有修复，且已使用修复版本重新验证原始复现。
- `closed`：问题已关闭，不再影响当前路线图。

发现问题时，记录必须包含准确的依赖版本或 commit、确定性复现步骤、实际行为、预期行为、证据位置、受影响的路线图任务、上游引用以及明确的解除阻塞条件。

需要上游改动时，依赖任务必须保持 `blocked-upstream`。禁止在 Danny 中加入生产 workaround。上游修复后，必须重新运行原始复现和受影响的集成检查，才能将状态改为 `fixed-upstream`。

## 已解决的问题

当前没有已解决的 Baymax finding。

## 未解决的问题

当前没有未解决的 Baymax finding。
