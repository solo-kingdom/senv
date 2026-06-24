## Context

Senv 当前的交互入口是 `senv interactive`——一个基于 `bufio.Reader` 的**数字菜单**模式（1/2/3 选择 + 回车 + 子菜单）。浏览 env/text/config 需要多步选择，且查看具体值还要再走子命令。用户反馈"通过命令查看数据太麻烦"。

与此同时，业务逻辑层已成熟：
- `internal/env.Manager` — 完整的 env CRUD + 分组激活/导出/解引用
- `internal/text.Manager` — 文本块 CRUD + `SetViaEditor`（解密→临时文件→vim→加密闭环已实现）
- `internal/config.Manager` — 配置文件 CRUD + `Edit`（同样的 vim 编辑闭环）

vim 编辑闭环（解密 → 临时文件权限 600 → 调 `$VISUAL/$EDITOR/vim` → 读回比对 → 重新加密 → 删除临时文件）是关键资产，TUI 可直接复用，无需重新实现任何加密逻辑。

约束：
- 项目零 TUI 依赖，需引入第一个 TUI 库
- 三种数据模型不对称（env/text 有 group 两层，config 扁平；env 有激活态；值敏感度不同）
- TUI 是子进程，无法反向 `eval` 注入父 shell

## Goals / Non-Goals

**Goals:**
- 提供 `senv tui` 全屏界面，统一浏览/搜索/编辑 env/text/config
- 复用现有 Manager 业务逻辑，零改动加密/存储层
- 敏感值默认遮蔽，防肩窥
- 支持跨类型全局搜索（只搜 key/name）
- text/config 编辑复用现有 vim 闭环

**Non-Goals:**
- 不删除 `senv interactive`（暂共存，删除时机后续再议）
- 不在 TUI 实现 `env export`（无法 eval 注入父 shell）
- 不改动现有 CLI 命令、加密方案、数据模型
- 不给 config 引入 group 概念

## Decisions

### 决策 1：形态选择 — Tab 分页（形态 A）而非统一扁平列表

**选择**：三个 Tab（Env / Text / Config），每个 Tab 有专属布局和动作栏。

**理由**：本变更要求"全功能"——每种数据类型有专属动作集（env 的激活/停用/解引用，text 的 vim 编辑/导出文件，config 的 export 到 target）。统一扁平列表（形态 C）的动作栏无法优雅承载类型专属动作，要么拥挤要么动态变化导致复杂度上升。Tab 分页让每个类型自治，动作栏专属于该类型。

**备选方案**：
- 统一扁平列表（形态 C）：一眼看全貌好，但承载全功能动作栏困难，否决
- 三栏递进（形态 B，类 ranger）：config 没有 group 层导致三栏"缺一格"布局尴尬，否决

### 决策 2：TUI 库 — bubbletea

**选择**：[github.com/charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) + `bubbles`（组件）+ `lipgloss`（样式）。

**理由**：
- Go 生态 TUI 标杆，Model-Update-View（Elm 架构）清晰
- `tea.ExecProcess` 优雅挂起 TUI 跑外部进程（vim），退出后自动恢复，完美匹配复用现有 vim 闭环的需求
- `bubbles/textinput`、`bubbles/list`、`bubbles/viewport` 提供现成的内联输入框/列表/滚动预览组件

**代价**：约 10 个间接依赖，项目首次引入 TUI 依赖栈。可接受（一次性成本）。

**备选**：
- `tview`：更传统，表单友好，但 vim 挂起处理不如 bubbletea 优雅
- `gocui`：已进入维护模式，不推荐新项目使用

### 决策 3：敏感值遮蔽策略 — 单条切换 + 类型区分

**选择**：
- **env 值**：列表中永远显示为 `sk-***`，按 `v` 仅解开**当前选中**那一条，光标移开自动重新遮蔽
- **text/config**：列表只显示元信息（key/size/time/target），内容本就不在列表，进 vim 或详情面板才可见

**理由**：env 值短而脆（API key、密码），肩窥风险最高，遮蔽价值最大；text/config 是长内容，列表本就只显示元信息，遮蔽无意义。单条切换（非全局开关）最小化明文暴露窗口。

**备选方案**：
- 全局开关 `V`：方便批量看，但明文暴露面大，否决
- 全部类型统一遮蔽：text/config 列表无内容可遮蔽，规则冗余，否决

### 决策 4：env 值编辑 — 内联输入框，不调起 vim

**选择**：env 值用 `bubbles/textinput` 弹出 modal 内联编辑（新建/修改），不调起 vim。

**理由**：env 值通常单行短文本（连接串、key），内联输入框体验最佳，无需 vim 的多行编辑能力。这也避免给 env 新增 `SetViaEditor` 方法（保持 env.Manager 接口稳定）。

**对比**：text/config 内容是长文本/整份文件，vim 编辑合理且闭环已存在，复用即可。

### 决策 5：text/config 编辑 — 复用现有 Manager + tea.ExecProcess

**选择**：选中条目按 `e` → 调用现有 `text.Manager.SetViaEditor` 或 `config.Manager.Edit` → 通过 `tea.ExecProcess` 挂起 TUI → vim 接管终端 → 退出后恢复 TUI。

**理由**：vim 编辑闭环（解密→临时文件→编辑→加密）已实现并测试，TUI 只需触发，零重复逻辑。

```
┌─ TUI (bubbletea) ─────────────────────────────┐
│  选中 text/config 条目，按 e                    │
│       │                                        │
│       ▼ tea.ExecProcess(cmd)                   │
│  ┌─────────────────────────────────────────┐  │
│  │ TUI 挂起，vim 接管终端                    │  │
│  │  └─ Manager.SetViaEditor/Edit 已完成:     │  │
│  │     解密 → 临时文件(600) → exec editor    │  │
│  │  vim 退出 → 读回 → 比对 → 重新加密         │  │
│  └─────────────────────────────────────────┘  │
│       │                                        │
│       ▼ tea.Resume                             │
│  TUI 恢复，刷新列表                             │
└────────────────────────────────────────────────┘
```

> 实现注记：现有 `SetViaEditor`/`Edit` 内部用 `exec.Command(editor,tmp).Run()` 直接接管 stdio，与 `tea.ExecProcess` 配合需确认挂起/恢复衔接，可能需要小重构（把"准备临时文件"和"运行编辑器"拆分），在 tasks 阶段细化。

### 决策 6：全局搜索 — 只搜 key/name，不搜值

**选择**：全局 overlay（触发键 `S`）和 tab 内过滤（触发键 `/`）都**只匹配 key/name**，不匹配值。

**理由**：
- **安全**：值是敏感的，搜索结果列表若匹配值，等于把所有秘密铺出来，违背遮蔽原则
- **降噪**：值匹配产生大量噪音（如搜 `postgres` 命中所有带该连接串的 env）
- key/name 搜索已覆盖 99% 的"找东西"需求

**结果呈现**：搜索结果按类型标识（🔵env / 📄text / 📁cfg）+ key/name + 遮蔽的值预览（env 显示 `***`，text 显示 size，config 显示 target）。`enter` 跳转到对应 Tab + group + 条目并选中。

### 决策 7：Config Tab 布局 — 单栏，无左栏

**选择**：Config Tab 不设左侧 group 列表栏，主区域单栏铺满 config 列表。

**理由**：config 数据模型是扁平的（name → file），没有 group 层。强行加左栏（如"All"占位）是装饰性冗余。诚实对待数据模型不对称，不一致就不一致。

### 决策 8：解引用切换 — 默认原始，D 切换

**选择**：env/text 列表默认显示**原始存储值**（含 `{{env:...}}`/`{{text:...}}` 引用语法），按 `D` 切换到**解引用后**的视图。

**理由**：原始值是存储的真相，解引用是派生视图且可能失败（引用不存在）。默认原始更稳健，解引用作为可选切换。

## 数据流

```
                    senv tui (cmd/tui.go)
                          │
                          ▼ 校验密码 / 复用 session 缓存
                   ┌──────────────┐
                   │  TUI Model   │  (internal/tui/)
                   │  ┌────────┐  │
                   │  │ Env Tab│──┼──→ env.Manager (List/Get/Set/Delete/
                   │  ├────────┤  │     ActivateGroup/DeactivateGroup/
                   │  │Text Tab│──┼──→ text.Manager (List/Get/Set/
                   │  ├────────┤  │     SetViaEditor/Delete)
                   │  │Cfg Tab │──┼──→ config.Manager (List/Get/Create/
                   │  └────────┘  │     Edit/Export/Delete)
                   │   + Search   │
                   └──────┬───────┘
                          │  所有 Manager 现有方法，零改动
                          ▼
                   ┌──────────────┐
                   │   storage    │  (AES-256-GCM 加密文件)
                   └──────────────┘

  vim 编辑路径（仅 text/config）:
    TUI ──e──→ tea.ExecProcess ──→ Manager.SetViaEditor/Edit
                                    → 解密 → tmpfile(600) → vim → 加密
```

## 错误处理策略

| 错误场景 | 处理 |
|---------|------|
| 项目未初始化 | TUI 启动前检查，提示 `senv init` 后退出（不进 TUI） |
| 密码错误 | 启动时校验失败，提示后退出（复用现有校验逻辑） |
| Manager 返回错误（解密失败/未找到/key 不存在） | TUI 底部错误条（error banner）显示，不崩溃，保留当前视图 |
| vim 编辑失败（编辑器不存在/退出码非 0） | 错误条提示，临时文件仍被清理（defer os.Remove） |
| 内容未改动 | 复用现有 "No changes detected" 逻辑，不重新加密 |
| 全局搜索无结果 | overlay 内显示 "无匹配"，不跳转 |
| 空分组/空列表 | 列表区显示空状态提示（如"该分组无环境变量"） |

## 向后兼容

本变更**不改动任何存储格式**：
- 加密方案、文件布局、settings.json、config_index.json 全部不变
- 现有所有 CLI 命令行为不变
- `senv interactive` 保留，行为不变

TUI 是纯增量交互层，与现有系统完全向后兼容。无需迁移、无需回滚策略。

## 使用示例

```bash
# 启动 TUI
senv tui

# TUI 内操作（快捷键）：
#   Tab / 1 2 3   切换 Env/Text/Config 标签
#   ↑ ↓ / j k     列表导航
#   enter         查看详情（text/config）/ 解开 env 明文
#   v             单条切换 env 值明文/遮蔽
#   e             编辑（env=内联输入框，text/config=vim）
#   n             新建条目
#   d             删除（需确认）
#   a / x         激活/停用 env 分组（仅 Env Tab）
#   +             新建分组（Env/Text Tab）
#   D             切换解引用视图（Env/Text Tab）
#   y             复制值到剪贴板
#   o             导出 text 到文件 / config 到 target 路径
#   /             当前 Tab 内过滤
#   S             全局跨类型搜索 overlay
#   esc           关闭 overlay/取消操作
#   q             退出 TUI
```

## Risks / Trade-offs

- **[新依赖引入]** bubbletea 及 charm 生态约 10 个间接依赖 → 一次性成本，go.mod 锁定版本，可接受。go.sum 保证可复现构建。
- **[vim 挂起衔接]** 现有 `SetViaEditor`/`Edit` 用 `exec.Command.Run()` 直接接管 stdio，与 `tea.ExecProcess` 的挂起/恢复模型需确认衔接 → 可能需小重构拆分"准备临时文件"与"运行编辑器"两步；如衔接顺滑则零改动。在实现第一个 Tab（Text）时验证。
- **[敏感值肩窥]** TUI 全屏显示更多上下文，肩窥窗口比单条命令输出大 → 通过默认遮蔽 + 单条切换 + 搜索不搜值缓解。
- **[数据模型不对称]** config 无 group 导致 Tab 布局不完全一致 → 接受不一致，Config Tab 单栏设计已规避。
- **[env export 缺席]** 用户可能期望 TUI 能 export → 文档明确说明 export 走命令行（`eval $(senv env export)`），TUI 内不重复实现。
- **[旧 interactive 共存]** 两套交互入口可能让用户困惑 → 文档引导 TUI 为推荐入口，interactive 标注为遗留；删除时机后续单独立项。

## Open Questions

- vim 编辑衔接是否需要重构 `SetViaEditor`/`Edit`？→ 在实现 Text Tab 时 spike 验证，若 `tea.ExecProcess` 能直接包装现有 `exec.Command.Run()` 则零改动，否则拆分。tasks 中已列为验证步骤。
