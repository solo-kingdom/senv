# Design: tui-ux-driver

## Context

现状盘点见 proposal「Why」与 `grill.md`（11 项 settled 决策）。代码事实：8 个 Tab 各自手写列表渲染与按键处理，共享面仅 `windowedPane`/styles/form/detail overlay；多选只在 AI 向导；`/` 过滤复制 4 份且 SSH/AI/MCP 缺失；分组只有 env/text/config 且范式不一；按键 `r` 四义、`e`/`x`/`u`/`m`/`i` 多义；help 靠解析 `Help()` 字符串。

本 driver 不直接改代码，只编排 7 个子 change；设计决策已由 grill 收敛，本文记录跨子 change 的技术方案与拆分依据。

## Goals / Non-Goals

**Goals:**

- 建立两层基础设施：中央 keymap 注册表（按键→动作，help/状态栏文案由其生成）与共享列表组件（单选+多选+过滤+窗口化+双栏几何）
- 三能力全 Tab 统一：多选集（D2/D9）、过滤补齐（D4）、侧栏范式推广（D3）
- 按键语义表落地（D7 附录），help 与实际按键不再可能漂移
- 修复批：`S` 搜索结果窗口化、history `q` 绕过 dirty-quit 守卫、两处 spec/impl 不一致对齐
- 手搓流程收敛到 form 引擎（config 创建向导、env 新建）

**Non-Goals:**

- 不新增数据分组字段（SSH/AI/MCP 保持无组实体，平铺+过滤）
- 不做 fuzzy 搜索、不扩大匹配范围到 value、不做 mouse 支持、不做 undo/redo
- 不替换 bubbletea/lipgloss 技术栈，不引入 `bubbles/list`
- 不改变各 Manager 层接口与存储格式（纯 TUI 层改造）

## Decisions

### D1 分层架构：keymap 注册表 + 列表组件（ADR 候选 adr-self-built-list-component）

```
model.go（Tab 编排、全局键路由、overlay）
  ├─ keymap.go   中央注册表：Tab 声明 []KeyBinding{Key, Action, Desc}，
  │              Help() 字符串由注册表渲染；`?` overlay 与状态栏提示同源
  ├─ list.go     共享列表组件：封装游标、窗口化、多选集、过滤挂点、
  │              双栏几何（内部收敛 windowedPane/clipLines/clamp 等 helper）
  ├─ filter.go   过滤组件：输入框状态机（append/backspace/esc/enter），挂接列表组件
  └─ 各 Tab      只保留数据装载、实体动作与 Tab 特有布局，渲染与导航交给组件
```

- **为什么自研不引入 `bubbles/list`**：库自带 keybinding/分页/preview 模型，与双栏布局、D7 语义表、多选集语义全面冲突；仓库对它零使用；封装自己的窗口化原语成本更低且迁移节奏可控。（三门槛满足，随 change 归档晋升 ADR）
- **为什么注册表先行于组件**（D10 ②先于③）：组件若先硬编码按键，注册表落地时要二次返工；注册表是组件按键消费的依赖。
- **为什么 config 侧栏是范式源**：config-tui spec 已定义「All 伪组 + 过滤感知计数 + 双栏焦点」，是唯一被 spec 固化的分组范式；env/text 升级为同款，零新概念。

### D7 按键语义统一（完整表见 grill.md 附录）

关键裁决：`r`=rename 全局唯一（refresh→`Ctrl+R`，history restore→`R`）；env 组激活/停用统一 `t`；text 导出 `o`→`x`；AI model-only `m`→`M`；确认框统一 `y`/`enter` 确认、`esc`/`n` 取消，plan 页收紧为仅 `esc`/`n` 取消、其余键忽略；`esc`=回上一层。直接 break 不做 alias，靠 `?` overlay 与状态栏提示覆盖学习成本。

与既有 spec 的冲突处置：tui-viewer 的 env 场景（`a` 激活/`x` 停用）、text 场景（`o` 导出）、llm-provider-tui 的 `m` 由 keymap 子 change 以 MODIFIED delta 改写；空状态文案「按 r 刷新」同步改 `Ctrl+R`。

### D9 多选集语义

`space` 勾选、`a` 全选=当前过滤可见集；选择集跨过滤持久（状态栏计数提示隐藏项）；批量安全动词（`d`/`x`/`i`/`u`/`t`）复用单项键作用于选择集，走既有 plan-preview/per-item confirm/drift `F` 流；选择集为空回落游标项（纯单选行为不变）；单实体操作（`e`/`r`/`m`/detail）仅选择数 ≤1 可用；选择不跨栏；批量提交后清空选择集。scope 快捷键（config `I`/`U`、MCP `X`/`U`）保留共存。

### Spec delta 策略（防子 change 间冲突）

- 能新增（ADDED）不修改（MODIFIED）：批量安装/撤回等新需求一律 ADD，不动既有需求文本
- 必须修改时（env `t`、text `x`、AI `M`、Config Tab 布局对齐、过滤范围澄清、env 新建表单化），MODIFIED 携带完整需求文本，且 MUST 保留现存全部 scenario 标题（`validate --strict` 按 title 严格核对，遗漏即拒）；仅改 scenario 内容、不改标题
- 需要废弃某个 scenario（如 All 伪组禁止条款）时 MODIFIED 做不到——以 REMOVED（带 Reason/Migration）+ ADDED 新名需求整块替换（fixes 子 change 已按此落地）
- 归档顺序 = D10 交付顺序（fixes → keymap → list → filter → multiselect → sidebar → forms），后归档的 delta 以已归档后的 spec 为基线

### 错误处理与数据流

- 列表组件不持有业务数据，只持有视图状态（游标/选择/窗口/过滤词）；数据仍由 Tab 从共享快照装载（tui-viewer「env 数据单趟加载与共享快照」语义不变），组件 MUST NOT 触发额外数据遍历
- 组件动作回调返回错误时走既有统一提示条（错误>警告>成功，成功超时消失）；批量操作沿用逐条执行、单条失败不中止其余（与 MCP `X` 导出语义一致），结束后汇总提示

## Risks / Trade-offs

- [8 Tab 迁移回归面大] → 组件先行 + audit/history 两个简单 Tab 试点（③），后续子 change 每次只迁移自己触及的 Tab；每子 change 收尾跑 `make check` 与手工冒烟
- [按键 break 影响既有用户] → `?` overlay 与状态栏即时提示新键位；`.agents/skills/senv-cli/SKILL.md` TUI 键位小节随各子 change 同步更新
- [MODIFIED delta 与归档基线漂移] → 严格按 D10 顺序逐个归档；每子 change `validate --strict` 通过后才允许下一个开始
- [多选批量写操作放大误操作后果] → 批量动词全部走既有确认流（plan 预览、per-item confirm、删除二次确认），不存在免确认批量写
- [组件抽象过度] → 只收敛已验证的共性（游标/窗口/选择/过滤/几何）；Tab 特有渲染以回调注入，不为特例扩 API

## Migration Plan

无存储格式变更、无 CLI 接口变更（`senv tui` 入口不变）。实施顺序即回滚单元：每个子 change 独立可回退（git 分支上逐个提交），①②先行落地后即使后续中止，修复与按键统一也已独立生效。

## Open Questions

无——grill 两轮已清空 frontier；试点 Tab 选择（audit/history）与 text 空分组显示策略（统一为计数 0 也显示，与 config 过滤行为一致）属实施细节，记录于此不另开决策。
