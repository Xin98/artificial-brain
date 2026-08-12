# Portability

Portability 使用户能够带走数据，并把私域实例的数据安全地导入云端。

## Language

**Export Bundle（导出包）**：
包含版本、来源、校验和及可迁移记录的用户数据归档。
_Avoid_: Backup、Database Dump

**Import Preview（导入预检）**：
在写入前展示新增、跳过、冲突和无效记录的确定性结果。
_Avoid_: Dry Run、Validation Report

**Source Identity（来源标识）**：
由来源实例与来源记录共同构成、用于重复导入去重的稳定标识。
_Avoid_: Local ID、UUID

**Import Conflict（导入冲突）**：
来源标识相同但内容无法安全合并的记录。
_Avoid_: Duplicate

