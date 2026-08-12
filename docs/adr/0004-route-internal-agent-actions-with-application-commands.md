# 内部 Agent 动作直接路由到应用命令

MVP 让模型生成严格的意图提案，由 Go 在本进程完成 Schema、权限与确认校验后调用公开 Application Command，不通过 MCP 或内部 HTTP 回调自身。这样保留单一业务入口并缩小模型权限；未来调用飞书、钉钉、日历等外部系统时把 MCP Client 放在适配器 seam，需要向外部 Agent 开放产品能力时再提供受 OAuth Scope 与确认策略保护的 MCP Server Adapter。
