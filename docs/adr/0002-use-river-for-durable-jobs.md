# 使用 River 承载 PostgreSQL 持久化任务

到期调度、并发领取、重试和崩溃恢复使用 River，而不在首版自研数据库队列，也不额外部署 Redis 或 RabbitMQ。River 仅实现应用层定义的调度接口，业务投递事实保存在 Reminder 自有模型中；因此未来可替换调度基础设施，而不把 River 状态机泄漏到领域层。

