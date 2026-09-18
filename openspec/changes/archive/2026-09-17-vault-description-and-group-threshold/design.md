## Context

见 proposal.md。现状：env 组有 `.meta.enc`（name/created_at），text 组只有目录；env/text `Set` 在组不存在时自动创建。config/MCP 已有 description，Host/KeyPair/LLM Provider 没有。`ExportVariables` 对同名 key 后者覆盖且无提示。ADR-0026 已锁定产品决策。

## Goals / Non-Goals

**Goals:**
- 在现有 JSON 密文上加可选 `description`，旧记录缺字段即空。
- env 组 meta 扩字段；text 组补与 env 同构的组 meta。
- Manager 层统一：无组则拒绝写入；AddGroup 必填说明。
- CLI 与 TUI 同步；MCP 只为 env/text/config/group 提供写说明。

**Non-Goals:**
- 组类型（变体/存档）软件化、前缀校验、存量迁移命令。
- 改同步协议版本；说明随既有密文 JSON 走。

## Decisions

1. **字段名一律 `description`**  
   与 config/MCP 已有 JSON 对齐。拒绝 `note`/`remark`。校验：`strings.TrimSpace` 后字节长度 ≤ 2048；新建组要求 trim 后非空。

2. **text 组 meta 对齐 env**  
   新增 `texts/{group}/.meta.enc`，结构与 `EnvGroupMeta` 相同（name、created_at、description）。List 目录仍是组存在性来源；meta 缺失（存量）视为空说明，不强制写回。备选：独立 index 文件——多一个身份映射面，不如目录+meta。

3. **条目说明存在各自密文 JSON**  
   `EnvVarEntry`/`TextEntry`/`HostEntry`/`KeyPairEntry`/`LLMProviderEntry` 加 `description,omitempty`。更新值时若调用方未传说明则保留原说明（CLI 用「未设置 flag」与空字符串区分：Cobra `Changed()`；MCP 用 omitempty 指针或独立 `senv_env_set` 可选字段，缺省保留）。

4. **AddGroup 签名改为必填说明**  
   `AddGroup(name, description string) error`。测试与 TUI/MCP 全改。不提供「先建空组再补说明」的后门。

5. **隐式建组只堵 Set/import 路径**  
   `EnvGroupExists`/`TextGroupExists` 为 false 时返回明确错误（含 `senv env group add <name> --description ...` 指引）。旧格式 env 单文件组：存在即视为组已存在，仍走既有迁移，不当成隐式新建。

6. **冲突检测**  
   激活组列表（default ∪ ActiveGroups）上建 `key -> []group`；len>1 则 warning。覆盖者 = 遍历顺序中最后一个（与 `ExportVariables` 一致）。activate 在写入 settings 成功后按新集合检测。不在 MCP export 省略 warning：MCP `senv_env_export` 同样返回 warnings 字段或文本。

7. **TUI**  
   env/text 新建组表单增加必填 description；条目新建/编辑增加可选 description。Host/KeyPair/AI 表单同。config 只加长度校验。

## Risks / Trade-offs

- [Breaking] 脚本依赖 `senv env foo:BAR=x` 隐式建组 → 错误信息给 add 指引；ADR 已接受。
- [存量 text 无 meta] list 说明为空 → 不自动写盘，避免一次同步风暴。
- [说明含密钥] 只靠文档/skill，不扫描 → list 可能泄露操作者误写的口令；上限降低体积但不能杜绝。

## Migration Plan

无需数据迁移任务。旧 JSON 缺字段即空。回滚旧二进制：新字段 `omitempty`，旧代码忽略未知 JSON 字段（Go 默认）。新写的 text `.meta.enc` 对旧代码：若旧 list 只扫目录则仍可见组；若旧代码不识 meta 文件则当普通文件忽略（确认 list 实现只认目录与 `.enc` 条目）。

## Open Questions

无。组名约定与存量 `svc-*` 整理不在本 change。
