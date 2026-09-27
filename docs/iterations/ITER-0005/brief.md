# ITER-0005 brief

Purpose: upgrade the Conversation bounded context from a single-turn todo intent parser into a persistent multi-session assistant — free-form chat alongside the existing todo intents, full transcript persistence, session management (create/list/rename/delete) with a web sidebar, and history replay on session open. One model call now produces both a natural-language reply and an optional structured intent proposal; the delete-confirmation safety flow is semantically unchanged.

Scope is migration 010 (schema 9→10, `conversation.sessions` + nullable `messages.session_id`), the unified `ModelPort.Complete` envelope and both model adapters (deterministic echo-style dev replies; OpenAI-compatible unified prompt), envelope validation beside the untouched v1 proposal validation, session commands and history queries, the five new session routes plus `sessionId`/`reply` on the messages flow, the conversation OpenAPI contract update, `CONVERSATION_HISTORY_TURNS` platform configuration, the web conversation view (session sidebar + history-loading chat panel), the extended smoke block, and iteration evidence. Out of scope: conversation history in portability exports, session pagination/archival, confirmation-to-session binding, real-provider calls from CI, and any new Go or web dependencies. This iteration deliberately lifts the ITER-0004 "open-ended chat" exclusion; migrations 001–009 stay frozen.

The governing [design](../../superpowers/specs/2026-09-27-iter-0005-conversation-sessions-design.md) and [implementation plan](../../superpowers/plans/2026-09-27-iter-0005-conversation-sessions.md) are authoritative. Implementation decisions D1–D10 are recorded in [decisions.md](decisions.md).

## Acceptance criteria

1. 自由对话：开发/CI（deterministic 适配器）下，非待办输入返回 `kind:"chat"` 与固定回声式中文回复（含输入前 100 字符），不再返回 `unsupported`；注入类非法提案仍 fail-closed 为 `unsupported` 且绝不执行。
2. 待办意图（创建/查询/删除）行为与 ITER-0002/0004 完全一致：missingFields/低置信度澄清、候选列表、单次消费+版本绑定的删除确认流程无回归。
3. 会话保存：`POST /messages` 无 `sessionId` 时自动建会话（标题=首条消息前 30 字符）；显式 `sessionId` 做 workspace+user 归属校验，未命中 404 `session_not_found`；会话可列表（≤100，按更新时间倒序）、重命名（1..50 字符）、删除（204，消息级联删除）。
4. 对话历史：每回合持久化 user+assistant 两行（assistant 为模型回复或确定性中文摘要）；`GET /sessions/{id}/messages` 返回最近 ≤200 条升序历史；跨会话/跨用户严格隔离。
5. 多轮上下文：`CONVERSATION_HISTORY_TURNS`（默认 10，0..50 fail-closed）控制送入模型的历史窗口，每条截断 500 字符；deterministic 适配器忽略历史仍保持语料逐字节一致。
6. Web：/conversation 双栏布局——侧边栏新建/切换/重命名/删除会话；打开会话以纯文本回放历史（不重放确认倒计时/候选按钮）；`chat` 气泡渲染；自动建会话后侧边栏刷新且实时回合不丢。
7. 合约与门禁：conversation.yaml 新增 5 路由与 `chat`/`reply`/`sessionId`，contract 测试绿；`make verify`、`make migration-test`（版本 10）、`make smoke-test`（扩展对话块）干净检出全绿；迁移 001–009 零字节改动；go.mod 与 web 依赖零新增；CI 无任何真实模型出口。
