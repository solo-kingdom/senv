## Why

用户目前用 text/config 组合存 SSH 密钥与主机信息，但字段散落、不原子、无联动：host 无法结构化引用密钥，新建 host 不能一步选密钥。需要把 SSH 资产纳入 senv 一等管理。

## What Changes

- 新增 `keypair` 实体：从既有私钥文件导入，私钥整体加密入 vault；公钥尽力派生存档，失败可留空后补
- 新增 `host` 实体：alias 全局唯一（OpenSSH `Host` token 语义），核心字段 hostname/user/port/proxyJump/identityKey/tags + 任意额外 KV 直传
- `host add` 联动：交互选择已有 keypair，或 `--key-file` 一步导入并关联；`identityKey` 引用做存在性校验
- 删除保护：默认拒绝删除被引用 keypair 并列出引用者；`--force` 删除并清空引用
- `senv host export` 生成 OpenSSH config 片段（默认全量、`--host` 过滤）；`senv keypair materialize` 落盘 `~/.ssh/senv/`（目录 0700、文件 0600）
- TUI 新增浏览板块（私钥遮蔽、显示指纹）；MCP 新增只读工具（不提供私钥明文）

## Capabilities

### New Capabilities

- `ssh-assets`: SSH host 与 keypair 的一等管理：CRUD、host→keypair 引用一致性、导出与私钥落地、TUI/MCP 集成

### Modified Capabilities

（无——不扩展 ref 体系，不改变现有 env/text/config 行为）

## 非目标

- 不生成新密钥对（仅导入既有密钥）
- 不内建 ssh 连接（`host ssh` 留作后续增量）
- 不解析/校验 ProxyCommand 内容（黑盒直传）
- MCP 不提供导出私钥明文的工具

## 安全性分析

- 私钥复用现有 vault 加密（AES-256-GCM）与文件权限约束（600/700）
- materialize 落盘 0600、目录 0700；TUI 默认遮蔽私钥、MCP 只读且不输出明文
- 同步零知识不变：server/git 仅见密文

## Impact

- 代码：`cmd/` 新命令组、`internal/storage` 新顶级集合、新增 `internal/ssh` 管理层、`internal/tui` 板块、MCP 工具注册
- 依赖：golang.org/x/crypto/ssh（已在 go.mod，零新增）
- 数据：vault 内新增数据目录，无既有 schema 迁移
