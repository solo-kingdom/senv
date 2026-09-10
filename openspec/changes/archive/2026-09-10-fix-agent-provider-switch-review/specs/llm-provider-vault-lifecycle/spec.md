## Purpose

将 LLM Provider 纳入 vault 加密资产的一致生命周期，并约束本地敏感路径与存储身份校验，防止密文被 rekey、init 或一致性检查遗漏。

## ADDED Requirements

### Requirement: 加密集合纳入 vault 生命周期
Vault 生命周期 SHALL 把 `llm_providers` 视为与其他顶层加密集合同级的托管集合：修改 vault 口令或 rekey SHALL 迁移其中全部档案；无 metadata 初始化 SHALL 识别已有 provider 密文并拒绝继续；一致性检查 SHALL 报告 provider 密文能否用当前 key 解密。

#### Scenario: rekey 迁移 provider
- **WHEN** vault 内存在 LLM Provider 档案且用户修改 vault 口令
- **THEN** rekey 后 provider 密文可用新口令解锁，旧口令不再能解锁

#### Scenario: 孤立 provider 密文阻止重初始化
- **WHEN** vault metadata 缺失但数据目录存在 provider 密文且用户执行 init
- **THEN** init 以非 0 退出并提示存在会孤立的加密数据，不生成新 vault key

#### Scenario: 一致性检查覆盖 provider
- **WHEN** 某个 provider 密文无法用当前 key 解密且用户执行 vault 一致性检查
- **THEN** 报告包含该 provider 密文路径，且整体检查结果不为健康

### Requirement: 加载时复验 provider 档案
从 vault 加密条目加载 LLM Provider 时 SHALL 复验档案字段：base URL 必须是允许的 http(s) URL，模型集非空且默认模型属于模型集，凭据引用非空。任何字段不合法 SHALL 返回明确错误，不把档案交给切换、TUI 或 MCP 调用方。

#### Scenario: 拒绝损坏档案
- **WHEN** 解密后的 provider 档案 base URL 为空或默认模型不在模型集内
- **THEN** 加载返回明确校验错误，切换不写任何 agent 配置

### Requirement: 托管身份拒绝控制字符
Vault 托管身份（包括 LLM Provider alias）SHALL 拒绝空字符、换行、回车、tab 与其他控制字符；审计和输出 SHALL 使用同一已验证身份。

#### Scenario: 拒绝可污染日志的别名
- **WHEN** 用户尝试创建包含换行或 tab 的 provider alias
- **THEN** 命令在写入档案、凭据或审计成功记录前失败

### Requirement: 本地敏感路径权限收敛
Agent pointer 及本功能创建的 agent 配置所在目录 SHALL 使用 0700 权限；已存在目录权限更宽时 SHALL 在写入敏感文件前收敛为 0700。敏感文件 SHALL 为 0600。无法确定 home 目录时 SHALL 显式失败，不得退回到当前工作目录。

#### Scenario: 收紧宽松目录
- **WHEN** 目标 agent 配置目录已存在且权限为 0755，用户执行切换
- **THEN** 写入前目录权限收敛为 0700，写入后配置文件权限为 0600

#### Scenario: home 缺失时不写相对路径
- **WHEN** 运行环境无法确定用户 home 目录
- **THEN** 需要解析 agent 配置或指针路径的命令失败，且当前工作目录不出现新的 agent 配置
