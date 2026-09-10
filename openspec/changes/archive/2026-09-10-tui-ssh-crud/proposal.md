## Why

SSH Tab 目前完全只读（`internal/tui/ssh_tab.go` 自述 `read-only`），只渲染 `alias → hostname`：既不显示 host 关联了哪把 keypair，也无法在 TUI 内新建、编辑、删除或关联。CLI 早已有整套能力（`senv host add/get/edit/list/delete`、`senv keypair import/list/materialize/delete`、`host export`），TUI 用户被迫在两个界面之间来回。ADR-0005 已把「尽可能可编辑」定为 TUI 定位。

## What Changes

- TUI 内 host CRUD：新建、编辑、删除、导出 OpenSSH 片段（预览后写文件）；表单字段含 alias、hostname、user、port、proxyJump（引用选择）、identityKey（keypair 选择）、tags，`extra` 走 `$EDITOR`。
- TUI 内 keypair：导入（私钥文件路径）、重命名（同步改写引用它的 host）、删除（被引用时默认阻止并列出引用者，强制删除清引用）、materialize（二次确认并展示落盘路径）。
- 联动可见：host 列表内联显示所用 keypair 名称与指纹摘要；编辑 host 时以选择器关联 keypair；校验复用 `ssh.Manager` 既有规则（identityKey 必须存在、proxyJump 必须引用已存在 alias）。
- 新增 `senv keypair rename <old> <new>`，与 TUI 共用同一存储层实现与联动语义。
- 私钥明文 MUST NOT 进入 TUI 状态：导入/materialize 只传递路径与名称，界面仅显示路径与指纹。

## Non-goals

- 不生成新密钥（沿用「只导入既有私钥」的既有边界）。
- 不支持改 host alias：alias 是主键，改名需级联改写其它 host 的 `proxyJump`，另议。
- 不改 MCP 只读契约（MCP 仍不提供私钥明文工具）。
- 不解析 ProxyCommand，仍作为自由文本字段直传。

## Capabilities

### Modified Capabilities

- `ssh-assets`: TUI 从只读浏览升级为完整编辑，并补齐 keypair 重命名与联动保护。

## Impact

`internal/tui/ssh_tab.go` 重写为可编辑（表单、选择器、确认流、审计接入）；`internal/ssh` 新增 `RenameKeyPair` 与 host 引用级联；`cmd/ssh.go` 新增 `keypair rename`。无 vault 格式变更，无新增依赖。
