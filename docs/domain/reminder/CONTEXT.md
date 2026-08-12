# Reminder

Reminder 规划到期待办的外部通知，并保存每次投递的业务结果。

## Language

**Reminder Plan（提醒计划）**：
待办在特定到期版本上请求使用哪些联系通道提醒的安排。
_Avoid_: Queue Job、Cron

**Reminder Delivery（提醒投递）**：
通过一个联系通道执行的一次可审计提醒尝试及其最终结果。
_Avoid_: Message、Job

**Suppressed Delivery（已抑制投递）**：
因待办已完成、已删除、版本过期或通道不可用而被业务规则阻止的投递。
_Avoid_: Cancelled Job

**Delivery Receipt（投递回执）**：
外部供应商针对一次提醒投递返回或回调的状态证据。
_Avoid_: Success、Guaranteed Delivery

