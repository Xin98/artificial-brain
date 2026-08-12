# Conversation

Conversation 把用户文字转换为受应用规则约束的操作提案；它不拥有业务事实。

## Language

**Intent Proposal（意图提案）**：
模型对用户意图和参数的结构化建议，必须经过应用校验后才能成为命令或查询。
_Avoid_: Tool Call、Action、Command

**Clarification（澄清）**：
当必要信息缺失、含糊或可信度不足时，向用户请求的补充信息。
_Avoid_: Retry、Guess

**Confirmation Request（确认请求）**：
对一个已解析的破坏性候选操作发出的、短时有效且绑定用户与目标的确认要求。
_Avoid_: Approval Token、Delete Command

**Candidate（候选待办）**：
与用户删除描述相符、可供用户明确选择的待办摘要。
_Avoid_: Match Result
