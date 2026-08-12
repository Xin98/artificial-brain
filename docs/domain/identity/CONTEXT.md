# Identity

Identity 识别访问者，并将其访问限定在一个个人工作区内。

## Language

**User（用户）**：
能够登录云端产品、拥有一个个人工作区的自然人。
_Avoid_: Account、Tenant

**Personal Workspace（个人工作区）**：
承载一名用户全部工作数据的隔离空间；私域实例同样只有一个个人工作区。
_Avoid_: Tenant、Namespace

**Contact Channel（联系通道）**：
已验证且可供提醒使用的邮箱或手机号及其启用状态。
_Avoid_: Address、Endpoint

**Login Challenge（登录挑战）**：
向手机号发出、短时有效且只能使用一次的登录验证要求。
_Avoid_: Password、Permanent Code

