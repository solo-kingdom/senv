## Purpose

保证 senv 在本地磁盘落盘的敏感文件（settings.json、session 缓存、provider 凭据等）无论新建还是覆写，都维持用户私有权限，消除“老版本创建的 0644 文件被反复覆写却永不收紧”的隐患。

## Requirements

### Requirement: 敏感文件写入权限保证

所有承载口令派生钥、provider 凭据（含 server token）或用户配置的敏感文件 SHALL 以 0600 权限创建，所在目录 SHALL 以 0700 创建。

#### Scenario: 新建 settings.json 权限

- **WHEN** 首次保存 settings.json
- **THEN** 文件权限为 0600，父目录权限为 0700

#### Scenario: server token 落盘权限

- **WHEN** 用户配置 server provider，token 写入 settings.json
- **THEN** 文件权限保持 0600，其他用户不可读

### Requirement: 覆写前收紧存量宽松权限

写入已存在的敏感文件前，系统 SHALL 检查其权限，宽于 0600（或目录宽于 0700）时 MUST 先收紧再写入；写路径 MUST NOT 依赖“创建时权限”而对存量文件失效。

#### Scenario: 存量 0644 文件被覆写后收紧

- **WHEN** 对一个权限为 0644 的既有 settings.json 执行保存
- **THEN** 写入完成后文件权限为 0600

#### Scenario: 存量宽松目录收紧

- **WHEN** 敏感文件所在目录权限宽于 0700
- **THEN** 写入前目录权限被收紧为 0700
