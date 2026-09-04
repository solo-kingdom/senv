## Why

b3b4c33 为 config index 引入严格身份校验后，旧版本写入的非可移植名称（如含 `:` 的 `feg:ai-ops-portal.pub`）会让新版在**每次**加载索引时硬失败，`config list/get/create`、`senv doctor`、rekey 与 TUI 全部被一条存量记录卡死。同时索引文件缺失时 `ErrNotExist` 被原样抛出，全新空库首次 `config list` 也会报错。

## What Changes

- `LoadConfigIndex` 将索引文件缺失（`ErrNotExist`）解释为空索引，空库只读与创建操作正常工作
- 索引中含非法名称的**存量**条目在只读路径（list/groups/TUI/doctor 探针）被隔离跳过并给出可见警告，不再毒死整个索引
- 破坏性操作（edit/export/install/delete/rekey/同步）在存在被隔离条目时**继续 fail-closed**，符合既有安全规约
- 新增 `senv config repair`：交互式将存量非法名称改写为可移植名称（同步改写 map key、`Name`、`EncryptedFile` 并重命名密文文件）；对缺失密文的陈旧条目提供显式丢弃选项

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `config-grouped-storage`: 索引缺失视为空库；存量非法名在只读路径隔离降级、破坏性路径维持 fail-closed；新增 repair 命令的安全改写要求
- `data-consistency`: `CheckConsistency`/`senv doctor` 遇到被隔离条目时输出警告并继续体检，不再整体失败

## Impact

- 代码：`internal/storage`（索引加载与规范化）、`internal/config`（manager/list/groups）、`cmd/config.go`（repair 命令）、`internal/storage/consistency.go`（doctor 探针）、TUI config tab
- 兼容性：旧非法名索引从"全链路不可用"恢复为"只读可用 + 显式修复"；新创建名称仍严格校验，不放宽
- 无加密算法、密钥或文件格式变更

## Non-goals

- 不放宽 create 入口的新名称校验
- 不做静默自动迁移，修复必须显式确认
- 不处理 env/text 领域可能存在的类似历史名（如有，另行立项）
- 不引入通用跨平台文件名映射框架

## 安全性分析

- 被隔离条目在只读路径**从不打开**对应文件，仅跳过并警告，不引入路径穿越面
- 破坏性操作维持既有 fail-closed 规约，存在隔离条目即整体拒绝
- repair 在任何改名前校验新名称为合法单路径段且不与现有条目冲突；改写与文件重命名在 vault 变更锁内完成；过程中不输出明文
