## Why

通过命令行查看 env/text/config 数据需要记忆大量子命令（`env list -g`、`text -g secrets list`、`config list`）和多步操作，体验繁琐。需要一个统一的 TUI 界面来浏览、搜索和编辑所有加密存储的数据。

## What Changes

- 新增 `senv tui` 命令，启动全屏 TUI 界面（基于 [bubbletea](https://github.com/charmbracelet/bubbletea)）
- 三个 Tab 承载各自完整功能：**Env**（group 列表 + 变量列表，支持激活/停用/内联编辑/解引用切换）、**Text**（group 列表 + 文本块列表，vim 编辑复用现有 `SetViaEditor`）、**Config**（单栏列表，vim 编辑复用现有 `Edit`）
- 敏感值默认遮蔽：env 值显示为 `sk-***`，按 `v` 单条切换明文；text/config 列表只显示元信息（key/size/time），内容进 vim 或详情才可见
- 全局跨类型搜索 overlay：只匹配 key/name，**不匹配值**（避免敏感数据在搜索结果中暴露）
- 复用现有 `internal/{env,text,config}` Manager 业务逻辑，不改动加密/存储层

## Non-goals

- **不删除** `senv interactive` 命令（暂保留共存，删除时机下次再议）
- **不实现** `senv env export` 在 TUI 中——该命令服务于 shell 启动注入（`eval $(...)`），TUI 作为子进程无法反向 eval 父 shell
- **不改动** 现有任何 CLI 命令、加密方案或数据模型
- **不给 config 引入 group 概念**——config 维持扁平结构，Tab 内采用单栏布局
- **不实现** tab 内编辑器（env 值用内联输入框，不调起 vim）

## Capabilities

### New Capabilities
- `tui-viewer`: 全屏 TUI 界面，统一浏览、搜索、编辑 env/text/config 三类加密数据，含敏感值遮蔽策略与全局搜索

### Modified Capabilities
<!-- 无。现有 env/text/config 的 spec 级行为不变，TUI 是新增的交互层，复用现有 Manager 接口 -->

## Impact

- **新增代码**：`cmd/tui.go`（命令注册）+ `internal/tui/`（TUI 模型/视图/消息，按 tab 拆分）
- **新依赖**：`github.com/charmbracelet/bubbletea` 及 charm 生态组件（`bubbles`、`lipgloss`），约 10 个间接依赖
- **复用层**：`internal/env.Manager`、`internal/text.Manager`、`internal/config.Manager` 现有方法，零改动
- **入口**：`senv tui` 取代日常浏览场景对 `interactive` 的依赖（但不删除后者）

## Security Analysis

本变更涉及敏感数据在终端界面的展示，新增以下安全考量：

- **肩窥防护**：env 值默认遮蔽（`sk-***`），需主动按键（`v`）单条切换明文，离开条目自动重新遮蔽；text/config 内容不在列表显示
- **搜索不泄漏**：全局/局部搜索仅匹配 key/name，不匹配值，避免结果列表批量暴露秘密
- **编辑流程不变**：vim 编辑复用现有「解密 → 临时文件(权限 600) → 编辑 → 重新加密 → 删除临时文件」闭环，无新攻击面
- **无新存储路径**：TUI 不写入任何新文件，所有持久化仍走现有加密存储
- **会话缓存复用**：TUI 复用现有 session 缓存机制（`session.Manager`），密码不长期驻留内存的行为保持一致
