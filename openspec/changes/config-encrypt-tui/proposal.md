## Why
TUI（`internal/tui/config_tab.go`）与经典交互菜单（`cmd/interactive_config.go`）目前只有 list/create/edit/export/delete，未暴露分组、meta 与 install/uninstall。新能力需要在两个交互入口可用。

## What Changes
- TUI config tab：列表按分组组织/展示 group 与 description；新增 install / uninstall 操作，弹出 plan 预览，确认后执行；changed 条目单独确认
- 经典交互菜单 `interactive_config.go`：菜单项对齐新能力（分组创建、按组查看、install/uninstall 带 plan 确认）
- 创建/编辑入口支持录入 group 与 description

## Non-goals
- 不改 env / text tab
- 不新增 TUI 主题或布局体系，沿用现有 styles

## Capabilities
- `config-tui`: 交互入口（TUI 与经典菜单）的 config 分组视图与 install/uninstall 确认流

## 依赖
- `config-encrypt-storage` / `config-encrypt-install` / `config-encrypt-uninstall`

## 验收标准
- [ ] TUI 与经典菜单均可查看分组与 meta
- [ ] TUI 与经典菜单均可执行 install/uninstall，且先展示 plan、确认后执行
- [ ] uninstall 遇到 changed 条目时需显式确认
