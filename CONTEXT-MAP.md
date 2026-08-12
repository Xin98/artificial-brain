# Context Map

本仓库包含多个限界上下文。这里仅记录领域关系；实现细节见正式设计文档和 ADR。

## Contexts

- [Identity](./docs/domain/identity/CONTEXT.md) — 识别访问者并管理其个人工作区与联系通道。
- [Todo](./docs/domain/todo/CONTEXT.md) — 管理用户承诺处理事项及其生命周期。
- [Conversation](./docs/domain/conversation/CONTEXT.md) — 将自然语言转换为可验证的意图提案和确认请求。
- [Reminder](./docs/domain/reminder/CONTEXT.md) — 规划并记录到期待办的外部提醒投递。
- [Portability](./docs/domain/portability/CONTEXT.md) — 在实例之间导出、预检和导入用户数据。

## Relationships

- **Conversation → Todo**：Conversation 提交经过验证的待办命令或查询；它不拥有待办事实。
- **Todo → Reminder**：Todo 的到期安排产生或失效提醒计划；Reminder 在执行前重新验证待办版本。
- **Identity → Todo / Conversation / Reminder / Portability**：Identity 提供当前用户、个人工作区和已验证联系通道。
- **Portability → Identity / Todo / Reminder**：Portability 读取版本化快照并通过各上下文的公开应用接口导入，禁止直接复制内部表。
