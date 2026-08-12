# Todo

Todo 管理用户承诺处理的事项及其可见生命周期。

## Language

**Todo（待办）**：
用户承诺处理的一件事项，可以有到期时间，也可以没有。
_Avoid_: Task、Job、Item

**Due Time（到期时间）**：
用户希望待办最迟得到处理的时刻。
_Avoid_: Reminder Time、Overdue Time

**Pending Todo（待处理待办）**：
尚未完成或删除的待办。
_Avoid_: Open Task、Active Job

**Completed Todo（已完成待办）**：
由用户明确标记为完成的待办。
_Avoid_: Closed Task

**Deleted Todo（已删除待办）**：
对普通视图不可见、但为审计与恢复语义保留的软删除待办。
_Avoid_: Purged Todo

**Overdue Todo（逾期待办）**：
当前时间晚于到期时间的待处理待办；它是派生视图，不是持久状态。
_Avoid_: Overdue Status

