## Why

`r` 有 rename/refresh/restore/keypair-rename 四义，`e`/`x`/`u`/`m`/`i` 各有多义，`g`/`G`、PgUp/PgDn 仅部分 Tab 支持；help overlay 靠解析 `Help()` 字符串生成，按键与说明可能漂移。grill D5/D7 已定统一方向与语义表。

## What Changes

- 新建中央 keymap 注册表（按键→动作+说明），各 Tab 键位声明迁入，`?` overlay 与状态栏提示由注册表渲染，废除 `Help()` 字符串解析
- 按键语义落地（**BREAKING**，直接切换不做 alias）：refresh 统一 `Ctrl+R`（腾出 `r`=rename 唯一）、history restore→`R`、env 组激活/停用统一 `t`（toggle）、text 导出 `o`→`x`、AI model-only `m`→`M`
- 确认框统一：`y`/`enter` 确认、`esc`/`n` 取消；config/MCP plan 页由「任意其他键=取消」收紧为「仅 `esc`/`n` 取消，其余键忽略」
- `esc`=回上一层规则写入 help；`g`/`G` 跳顶/底补齐 SSH/AI/MCP

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: 「Env Tab 浏览与操作」激活/停用场景改 `t`；「Text Tab 浏览与操作」导出场景 `o`→`x`；「错误处理与空状态」空状态文案 `r`→`Ctrl+R`；「键位总览」增加注册表一致性与全局动词约束
- `llm-provider-tui`: 「Tab 内切换操作」model-only 键 `m`→`M`

## Impact

- 代码：`internal/tui/keymap.go`（新增）、`model.go`、全部 `*_tab.go` 按键处理、`help.go`
- 文档：`.agents/skills/senv-cli/SKILL.md` TUI 键位小节

## Non-goals

- 不做 alias 过渡、不动多选/过滤/侧栏（后续子 change）、不改 Manager 层
