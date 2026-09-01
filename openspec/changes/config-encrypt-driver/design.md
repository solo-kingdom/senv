## Context

现状（详见 proposal.md - Why）：

- `internal/config` 已有加密存取（复用 `internal/crypto` 的 AES-256-GCM + PBKDF2，与 env/text 同一套密钥体系），但模型是扁平的：`storage.ConfigIndex.Configs map[string]ConfigFile`，`ConfigFile` 仅含 name/encrypted_file/target_path/时间戳，无分组、无描述。
- env/text 以 group 为一等公民组织数据；config 需要对齐这一模型。
- 现有 `config export` 只做简单写出，无变更校验、无备份、无 dry-run/确认。
- 交互入口有两处：经典交互菜单 `cmd/interactive_config.go` 与 TUI `internal/tui/config_tab.go`。

已确认的决策（用户答复）：

1. 加密方案与 env/text 共用（同一 password/derived key，沿用现有 `NewManagerWithKey` 路径）
2. 保存位置支持 `~` 展开与环境变量展开（存储原始写法，使用时展开）
3. 子 change 拆分：storage / install / uninstall / tui
4. TUI 有交互入口，需接入新能力
5. install 时父目录不存在则递归创建

## Goals / Non-Goals

**Goals:**
- config 分组管理 + meta（描述、保存位置），加密存储不变
- install / uninstall 作用于单条 / 单分组 / 全部，统一走 plan(dry-run) → 确认 → 执行
- install：内容变化先备份再覆盖，父目录递归创建
- uninstall：目标文件被改动需确认，未改动直接删除
- CLI 与 TUI 双入口

**Non-Goals:**
- 不改 env / text 的行为与存储格式
- 不做跨机器同步、远程分发、权限位策略（沿用 0644 写出）
- 不改加密算法与密钥派生参数

## Decisions

### D1: 分组落地为 ConfigFile 的 Group 属性，而非 env 式独立分组文件

`ConfigIndex` 增加 `Group`（缺省 `default`）与 `Description` 字段；加密实体文件仍为 `<name>.enc` 扁平存放，name 全局唯一。

- 理由：config 的粒度是整个文件，每条已有独立加密实体，无需 env 那种组内多 entry 的聚合加密文件；加字段即可满足"按组管理/按组安装"，迁移成本最低（旧数据无 group 字段，读入时落 `default`）。
- 备选：仿 env 建 `ConfigGroup` 聚合文件 —— 改动面大且对整文件加解密无收益，弃。

### D2: 保存位置存原始写法，使用时展开

meta 中的 target path 原样存储（可含 `~`、`$VAR`、`${VAR}`），install/uninstall/export 时统一经 `~` 展开 + `os.ExpandEnv` 得到实际路径。

- 理由：同一配置在不同机器/用户下可复用；展开失败（变量缺失）作为 plan 阶段的错误暴露，而不是写入时静默固化。

### D3: install/uninstall 统一走 Plan → Confirm → Execute

核心包提供 `PlanInstall(scope)` / `PlanUninstall(scope)` 返回纯数据的操作计划（每条：动作 create/skip/backup+overwrite/delete/keep、目标路径、diff 摘要、错误）；CLI/TUI 渲染 plan，确认后 `Execute(plan)`。`--dry-run` 只输出 plan。

- 理由：dry-run 与确认要求两条入口（CLI flag、TUI 弹窗）共用同一份计划数据，行为一致且可单测。
- 备份命名：`<target>.senv-backup-<yyyymmddHHMMSS>`，与目标同目录。

### D4: 变更判定用解密后内容与目标文件字节比较

install：目标不存在 → create；字节相同 → skip；不同 → backup + overwrite。uninstall：目标不存在 → 视为已完成（plan 标注）；字节相同 → delete；不同 → 标记 needs-confirm，执行时必须显式确认。

- 理由：字节级比较最直接，不引入 hash 缓存（文件通常很小）；时间戳不可靠。

## Risks / Trade-offs

- [旧索引无 group/description 字段] → 读取时零值落 `default` 分组、空描述；不写回，避免无谓写操作
- [环境变量展开结果为空导致写到意外路径] → 展开后路径为空或含未展开 `$` 时在 plan 阶段报错，不执行
- [备份文件残留敏感明文] → 备份继承目标文件权限；文档注明备份需自行清理
- [name 全局唯一限制同组语义] → 接受，文档说明 name 唯一、group 仅为分类维度

## Migration Plan

- 旧 `config_index.json` 无新字段：读入后 Group=`default`、Description=""。`senv config list` 正常展示。
- 回滚：新字段为纯增量，旧版本读取时忽略未知字段，无破坏性变更。

## 子 change 拆分

| 子 change | 范围 |
|-----------|------|
| `config-encrypt-storage` | 分组 + meta 模型、路径展开、旧格式迁移、CLI 基础命令（create/list/get/delete 对齐分组） |
| `config-encrypt-install` | Plan/Confirm/Execute 框架、install 单/组/全部、变更校验、备份、递归建目录 |
| `config-encrypt-uninstall` | uninstall 单/组/全部、改动确认（复用 install 的 plan 框架） |
| `config-encrypt-tui` | TUI config tab 接入分组视图、install/uninstall plan 确认弹窗 |
