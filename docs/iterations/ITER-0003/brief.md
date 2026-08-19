# ITER-0003 brief

Purpose: turn the ITER-0002 reminder seam into a reliable delivery closed loop — River-backed durable scheduling inserted atomically into business transactions, email/SMS delivery through real stdlib-only adapters gated by fakes, execution-time suppression with business idempotency, retries with business dead letters, HMAC-signed receipt callbacks deduplicated by provider message ID, real four-state dashboard counters with a delivery record list, and a deterministic JSON ops endpoint.

Scope is migrations 006–007 (schema 5→7, River v1 schema inlined), the Reminder module growing into a full delivery hexagon, River scheduler/worker adapters, fake/SMTP/Aliyun provider adapters, reminder HTTP (delivery list, ops endpoint, receipt webhook, gated dev outbox), the Todo title/owner seam with real dashboard reminder counters, the reminder contract plus extended dashboard contract, the dashboard web reminder tiles and records, the smoke reminder E2E, and iteration evidence. Out of scope: Portability, open-ended chat, MCP, real-provider calls from CI, channels beyond email/SMS, and any new dependencies besides the two sanctioned River modules (ADR-0002).

The governing [design](../../superpowers/specs/2026-08-19-iter-0003-reliable-reminder-delivery-design.md) and [implementation plan](../../superpowers/plans/2026-08-19-iter-0003-reliable-reminder-delivery.md) are authoritative. The design decisions D1–D8 (pipeline approach A, River behind the evolved `JobScheduler` port, plan-time deliveries, River SQL inlined into the tern sequence, stdlib-only real adapters, provider-keyed informational receipts, deterministic-SQL ops, and dashboard-page web scope) are recorded in [decisions.md](decisions.md).

## Acceptance criteria

1. 带到期时间且存在可用通道的待办，在创建/改期事务内原子产生 Plan、每通道一条 Delivery 与 River Job；完成、删除、改期 revoke 计划并尽力取消 job。
2. 到期后 30 秒内（冒烟环境）发起投递；每个已启用已验证的邮箱、短信通道分别产生 Delivery 且 Fake 发件箱可查到消息。
3. 到期前完成/删除/改期的待办，其旧 job 被领取后仅产生 `suppressed` Delivery，Fake 发件箱无消息。
4. 瞬态失败触发 River 重试；重试跳过已成功通道；worker 崩溃恢复后不产生重复消息。
5. 永久失败或重试耗尽产生 `failed` 终态与死信日志事件，并出现在仪表盘与运维端点。
6. 回执端点：有效签名按 ProviderMessageID 幂等落库；无效签名被拒绝；重复与未知回执安全处理。
7. 仪表盘四态计数真实；提醒记录列表端点与仪表盘区块可见逐条投递；运维端点返回队列深度、最老等待、状态计数、重试率、死信数、延迟 P95。
8. CI/本地仅经 Fake 适配器投递；生产配置选择 fake 启动失败；真实适配器单测（本地监听/夹具）通过。
9. `make verify`、`make migration-test`、`make smoke-test` 在干净检出全绿；架构政策绿；go.mod 仅新增 River 两个模块。
10. 迭代账本齐备，独立干净上下文回归 Agent 产出 PASS 报告。
