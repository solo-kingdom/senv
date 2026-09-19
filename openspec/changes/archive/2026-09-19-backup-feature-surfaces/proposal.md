## Why

backup 的人机与 agent 面必须独立于 Text Tab：不常用数据混在 text 里体验差。本切片落地 TUI Backup Tab、全局搜索标识、MCP 四件套与 `kind=backup` 分组。存储/CLI 由 `backup-feature-core` 提供。

## What Changes

- TUI 独立 Backup Tab（布局/键位对齐 Text，无 `D` 解引用）
- 全局搜索 `S` 包含 backup 的 group/key/description，不搜正文
- MCP：`senv_backup_get/set/delete/list`；`senv_group_add`/`senv_group_list` 接受 `kind=backup`
- TUI `default` 组不可改名/删除；`+` 建组必填说明
- skill 的 TUI/MCP 段；`senv mcp list-tools` 与注册一致

**安全性分析**：列表与搜索不展示 value；MCP list 无 value；无保留组封锁（产品已决）。

## Non-goals

- 存储/CLI（core）、同步 kind（sync）
- Text Tab 行为变更、根快捷、引用、CLI rename

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 由 driver 准备段切分支 |

## 验收标准

- [x] `senv tui` 有 Backup Tab；键位对齐 Text（无 decode 开关）
- [x] 全局搜索能命中 backup 标识并跳转，不匹配 value
- [x] `senv mcp list-tools` 含四个 backup 工具；group 工具接受 `kind=backup`
- [x] MCP list 无 value；缺组 set 失败
- [x] skill TUI/MCP 段已更新
