## ADDED Requirements

### Requirement: 敏感文件写入不得跟随符号链接

所有 metadata、settings、config index、同步 state/cache、provider 凭据及加密数据写入 SHALL 拒绝目标或父路径中的符号链接，并相对于可信目录边界完成写入。若运行平台无法提供无竞态的 no-follow 保证，系统 MUST fail closed。

#### Scenario: settings 目标为符号链接
- **WHEN** `settings.json` 是指向其他文件的符号链接并触发保存
- **THEN** 保存失败，符号链接目标内容与权限均不改变

#### Scenario: data 子目录为符号链接
- **WHEN** 敏感条目的父目录被替换为指向 vault 外的符号链接
- **THEN** 写入失败，vault 外不产生任何变化

### Requirement: 单个敏感文件原子且持久替换

敏感文件覆写 SHALL 先在同一可信目录创建私有临时文件，完整写入并持久化后原子替换目标；中断后可观察到的目标 MUST 是完整旧内容或完整新内容，不得是截断或部分内容。临时文件 MUST 保持 0600 且不得遗留为正常数据入口。

#### Scenario: 覆写期间进程中断
- **WHEN** 敏感文件写入在目标替换前或替换时中断
- **THEN** 重启后目标为完整旧版本或完整新版本，不出现部分 JSON 或部分密文

#### Scenario: 临时文件创建失败
- **WHEN** 同目录临时文件无法以私有权限安全创建
- **THEN** 写入 fail closed，既有目标保持不变

### Requirement: 新建明文导出默认私有

senv 新建的 text 或 config 明文导出文件 SHALL 默认为 0600；新建父目录 SHALL 默认为仅当前用户可访问。用户仅可通过显式 mode 选择放宽权限，系统 MUST NOT 因 umask 或既有宽松默认值隐式创建 0644 明文秘密。

#### Scenario: 默认导出私钥
- **WHEN** 用户未指定 mode，将 text 或 config 导出到新文件
- **THEN** 文件权限为 0600，同机其他普通用户不可读

#### Scenario: 用户显式选择共享 mode
- **WHEN** 用户显式指定受支持的 `0644` mode 导出非秘密内容
- **THEN** 系统按该 mode 创建文件，并在执行前清楚体现这是权限放宽

#### Scenario: 覆盖既有文件不隐式放宽
- **WHEN** 导出覆盖权限比请求 mode 更严格的既有普通文件
- **THEN** 系统不因默认行为扩大其权限
