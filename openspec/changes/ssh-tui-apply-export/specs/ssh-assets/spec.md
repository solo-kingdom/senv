## ADDED Requirements

### Requirement: TUI 应用导出（Apply）
SSH Tab SHALL 提供 `A`（apply）键执行应用导出，与 CLI `senv host export` 同一 `Manager.Apply` 编排：host 栏聚焦时重建当前 host 所在组的整组片段；分组侧栏聚焦时重建选中组（「All」伪组 = 全量重建并清理幽灵组片段）。执行前 SHALL 弹确认框，列出将写入的组片段数、待落盘私钥数、Include 注册状态与 warning 计数；`enter/y` 确认执行，`esc/n` 取消且不产生任何副作用。结果 SHALL 以摘要 toast 呈现（重建/落盘/跳过/注册各项计数）。批量导出（`x` 多选）的输出目录表单与单条导出（预览后 `w`）的目标文件表单 MUST 拒绝 `~/.ssh/senv/` 内部路径（含 `groups/` 与 `keys/`），报错提示改用 `A` 应用导出——该目录树由 apply 全权拥有，外来文件会被幽灵清理删除。

#### Scenario: host 栏 apply 重建所在组
- **WHEN** 用户在 host 栏选中 host `web`（属组 `prod`）按 `A`，确认框列出 1 个组片段与待落盘密钥数，用户按 `y`
- **THEN** 系统 SHALL 调 `Apply({Host: "web"})` 重建 `groups/prod.conf`、落盘缺失私钥并保证 Include 注册，toast 展示摘要

#### Scenario: 侧栏按组 apply
- **WHEN** 用户在分组侧栏选中组 `prod` 按 `A` 并确认
- **THEN** 系统 SHALL 调 `Apply({Group: "prod"})` 只重建 `groups/prod.conf`，其余组片段不动

#### Scenario: 侧栏 All 全量 apply
- **WHEN** 用户在侧栏选中「All」按 `A` 并确认
- **THEN** 系统 SHALL 调 `Apply({})` 全量重建全部组片段并清理 vault 中已消失组的幽灵片段

#### Scenario: 取消无副作用
- **WHEN** 用户在确认框按 `esc`/`n`
- **THEN** 系统 MUST NOT 写任何组片段、私钥或 ssh config

#### Scenario: 批量导出目录防护
- **WHEN** 用户在批量导出表单的输出目录填入 `~/.ssh/senv/groups` 或 `~/.ssh/senv` 下任意路径并提交
- **THEN** 表单 SHALL 内联报错拒绝，提示该目录由 `A` 应用导出维护，应改用用户自有目录

#### Scenario: 单条导出目标文件防护
- **WHEN** 用户在单条导出的写文件表单中把目标文件填进 `~/.ssh/senv/` 内任意位置
- **THEN** 表单 SHALL 内联报错拒绝，提示改用 `A` 应用导出或另选用户自有路径

#### Scenario: 确认框呈现 warning 计数
- **WHEN** 待导出的 host 引用了本机 vault 缺失的 keypair，或落盘上存在未引用私钥
- **THEN** 确认框 SHALL 显示 warning 计数（明细在 CLI `senv host export` 可见），执行后 toast 保留该计数
