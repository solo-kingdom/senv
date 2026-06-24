## Context

`senv tui` 已实现完整功能（38/39 任务完成），但顶部 chrome 未做完：

- 顶部 tab 栏（`Env`/`Text`/`Config`）只是一行漂浮文字，active 仅靠粗体粉色、inactive 靠灰色区分，没有容器、边框或分隔符——不像可切换的 tab。
- 整个 TUI 没有外边框，顶部与底部都是裸文字，看起来未完成。
- `styles.go` 里的 `titleBarStyle` 注释写明是「tab strip 容器」，但实际被三个 tab 当作**栏内 pane 标题**用（`renderGroups` 里 `titleBarStyle.Render("Groups (n)")`），从未包裹过 tab strip——注释与实现错位。

当前渲染流水线（`internal/tui/model.go::View()`）：

```
WindowSizeMsg ──▶ Update()
                   │  m.width, m.height = msg.Width, msg.Height
                   │  contentH = height - 2          // tab strip 1 + status bar 1
                   └─▶ tabs[*].SetSize(width, contentH)

View()
  │  tabStrip = JoinHorizontal(activeTabStyle|tabStyle 渲染的 labels...)
  │  content  = tabs[active].View()
  │  bottom   = errorBarStyle | statusBarStyle
  └─▶ JoinVertical(tabStrip, content, bottom)        // 无外框，直接输出
```

约束：
- 纯视觉层改动，**不触碰**任何 Manager、加密、存储、session 逻辑。
- 现有交互行为（Tab/数字键切换、`S` 搜索 overlay、modal、`q` 退出）保持字节级不变。
- 不引入新依赖（lipgloss 已有 `RoundedBorder`、`BorderForeground`、`Background`、`Width`/`Height` 全部够用）。

## Goals / Non-Goals

**Goals:**
- 整个 TUI 被一圈圆角外框包裹，形成明确的「应用窗口」边界（解决「顶部没边框」）。
- 三个 tab 在视觉上像 tab：active tab 有背景色块，inactive tab 弱化，tab 间有 `│` 分隔（解决「不像可切换 tab」）。
- 底部状态/错误栏纳入外框内部，与顶部对称。
- 修正尺寸计算，保证内容不溢出、不塌陷。
- 清理 `titleBarStyle` 的注释/命名误导。

**Non-Goals:**
- 不重做配色（粉/紫/灰调色板保留，仅 active tab 加背景色块）。
- 不引入动画、焦点边框动画、tab 拖拽。
- 不改 Tab 接口签名、Model 字段结构、消息类型。
- 不改任何 tab 的内容渲染逻辑（pane 边框、列表样式保持）。

## Decisions

### 决策 1：外框策略 —— 整体包裹 vs 仅顶部横线

**选择：整体包裹（方案 C）**

```
╭─ senv ────────────────────────────────────╮
│  ▌Env▐ │ Text │ Config                    │  ← active tab 反色块
│ ────────────────────────────────────────  │  ← tab strip 底线
│ ┌─────────┐ ┌──────────────────────────┐  │
│ │ Groups  │ │ prod                     │  │
│ │ ● prod  │ │  DATABASE_URL = postgres │  │
│ └─────────┘ └──────────────────────────┘  │
│ j/k move · e edit · v reveal              │  ← status bar 在框内
╰───────────────────────────────────────────╯
```

**理由**：用户的「顶部没边框」抱怨直指整体边界缺失；外框同时给顶部、底部、左右一致的视觉收口，把零散的 strip + content + bar 统一成「一个窗口」。仅加顶部横线（方案 A）解决不了底部裸文字的不对称感。

**备选（方案 A，已否决）**：只在 tab strip 下加 `BorderBottom`。改动更小但底部仍裸，半成品观感残留。

**备选（tab 嵌入边框线，已否决）**：lipgloss 的 `BorderStyle.Title()` 只支持单段无格式文本，无法渲染多个独立着色的 tab 段；要 hack 则脆弱。tab 放在框内首行更简单且语义清晰。

### 决策 2：active tab 视觉 —— 反色背景块

**选择**：active tab 用 `Background(accent) + Foreground(白) + Bold` 渲染色块；inactive 维持 `Foreground(muted)`；tab 之间插 `│`（muted 色）。

```
 active:   ▌ Env ▐      ← 紫底白字，色块包住
 inactive:   Text        ← 灰字，透明底
 separator:  │           ← 灰色竖线
```

**理由**：反色块是 lazygit/k9s/kubectl 等主流 TUI 的通用 tab 模式，辨识度远高于「粗体下划线」。在 256 色终端上 `colorAccent=99`（紫）+ 白字对比足够，不依赖真彩色。

**备选（下划线，已否决）**：`BorderBottom` 彩色下划线更轻量，但在窄 tab、短标签（"Env"/"Text"/"Config" 都 3-6 字符）下色块比下划线醒目。

### 决策 3：尺寸计算 —— 外框固定铺满终端

外框样式固定 `Width(m.width).Height(m.height)` 铺满终端，内部可用区为 `(m.width-2, m.height-2)`（边框左右各 1、上下各 1）。内部再分：tab strip 1 行 + status bar 1 行 = 2 行 chrome，content 区为：

```
contentW = m.width  - 2          // 减左右边框
contentH = m.height - 2 - 2      // 减外框上下 + tab strip/status bar 各 1
        = m.height - 4
```

`SetSize(contentW, contentH)` 下发给各 tab。tab 内部已有的 `leftW/rightW` 计算不变（它们基于传入的 width）。

`errorBarStyle` 的 `truncateRunes` 上限从 `m.width-6` 调整为 `m.width-2-6`（减去左右边框）。

### 决策 4：最小尺寸守卫

当终端过小（`m.height < 6` 或 `m.width < 30`）时，正常布局会塌成乱码。新增守卫：此时 `View()` 直接返回居中的 `"terminal too small (need ≥30×6)"` 提示，不渲染 chrome。

**触发条件**：`height < 6`（外框 2 + tab 1 + status 1 + content 至少 2 行 = 6）或 `width < 30`（容纳不下最短 tab strip）。

### 决策 5：清理 `titleBarStyle` 命名误导

`titleBarStyle` 实际是 pane 标题样式（被三个 tab 的 `renderGroups`/`renderItems` 当栏内小标题用），不是 tab strip 容器。处理：

- **重命名** `titleBarStyle` → `paneTitleStyle`，注释改为「栏内 pane 的小标题（如 "Groups (n)"）」。
- 新增 `frameStyle`（外框）、`tabStripStyle`（tab 行容器，含底线），重做 `tabStyle`/`activeTabStyle`。
- 全量替换三处 `titleBarStyle.Render(...)` 引用为 `paneTitleStyle.Render(...)`。

### 决策 6：底部对称

status bar / error bar 维持现有文本与触发逻辑，仅将其 `Padding(0,1)` 样式包进外框即可（外框本身提供左右内缩，故 bar 内部 padding 可减为 0 或保留 1，视觉调参时决定）。错误条优先级高于状态条的逻辑（`View()` 里已有的 `if m.err != ""` 分支）不变。

## Risks / Trade-offs

- **[内容区缩小 2 行 2 列]** → 在小终端上可能少显示一行列表项。缓解：决策 4 的最小尺寸守卫；env/text tab 的 `leftW` 上限 26 已为窄宽留余地。
- **[现有渲染测试断言外框字符]** → 现有 `model_test.go`/`env_tab_test.go` 等若断言了精确输出字符串（如检查 "Env" 出现），加外框后字符位置变化可能误伤。缓解：tasks 中专列一步「更新受影响测试断言」，优先用 `strings.Contains` 而非精确匹配。
- **[active tab 背景色在极简终端（2 色）下退化]** → 退化后表现为反色（白底黑字），仍可辨识；不依赖真彩色，可接受。
- **[外框 + tab strip 双层视觉冗余]** → tab strip 自身底线 + 外框底边可能堆叠显挤。缓解：tab strip 底线用浅灰 `colorMuted`，与外框圆角视觉分层；实测后可去掉 tab strip 底线只靠外框分隔。

## Migration Plan

无数据迁移（纯视觉）。回滚策略：保留改动前的 `View()` 与 `SetSize` 数学注释快照（在 git 历史），回滚即 `git revert`。无配置开关——外框是无条件启用的视觉默认。

## 错误处理策略

- **终端过小**：决策 4 的守卫直接返回提示字符串，不进入正常渲染分支，不触发任何 Manager 调用。
- **样式渲染失败**：lipgloss 在不支持 unicode box-drawing 的终端会回退为 ASCII（`+-|`），布局仍可用，仅美观下降——不额外处理。
- **尺寸为 0（启动瞬间）**：现有 `if m.height == 0 { return "starting..." }` 守卫保留，先于外框逻辑生效。
- **Manager 错误**：完全不走渲染层改动，`errMsg` 机制与 `errorBarStyle` 行为不变，只是错误条现在画在外框内部。

## 数据流（改动后）

```
WindowSizeMsg ──▶ Update()
                   │  m.width, m.height = msg.Width, msg.Height
                   │  contentW = width - 2
                   │  contentH = height - 4           // 外框2 + tab/status各1
                   └─▶ tabs[*].SetSize(contentW, contentH)

View()
  │  guard: height<6 || width<30 → "terminal too small"
  │  tabStrip = tabStripStyle( JoinHorizontal( tab[0] │ tab[1] │ tab[2] ) )
  │  content  = tabs[active].View()
  │  bottom   = errorBar | statusBar
  │  inner    = JoinVertical(tabStrip, content, bottom)     // height-2 行
  └─▶ frameStyle.Width(w).Height(h).Render(inner)           // 铺满终端输出
```
