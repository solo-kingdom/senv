# Grill：tui-ux

## 决策记录

| # | 决策 | 结论 | 理由 | 状态 |
|---|------|------|------|------|
| D1 | 统一策略 | 共享组件收敛：在 `internal/tui` 建可复用列表组件（单选+多选+过滤+窗口化+双栏几何一体），Tab 逐批迁移 | 现状 filter 逻辑复制 4 份、1300 行 tab 文件；三能力同时铺开不能再造复制粘贴 | settled |
| D2 | 多选范围与风格 | config 条目、MCP servers、env vars、SSH hosts/keypairs、text blocks 全部支持多选；`space` 勾选 + `a` 全选/反选；AI 向导多选保留现状；scope 快捷键（config `i`/`I`/`u`/`U`、MCP `x`/`X`/`u`/`U`）保留共存，观察冗余再退役 | 「单项操作是批量安全写操作」的列表才多选；风格与 AI 向导既有范式一致；visual mode 本期不做 | settled |
| D3 | 分组边界 | 只做 UI 范式统一：config 侧栏（All 伪组 + 过滤感知计数）为标准范式，env/text 旧式组列表升级为同款侧栏（蕴含 env/text 也获得 All 伪组与计数）；不给 SSH/AI/MCP 新增数据分组字段 | 数据模型分组牵扯 storage/schema 且需求未证实；无组实体保持平铺+过滤 | settled |
| D4 | 搜索边界 | `/` 过滤补齐 SSH/AI/MCP，逻辑提取为共享过滤组件（audit preset filter 作为扩展点）；全局 `S` 保持子串匹配不做 fuzzy；匹配范围维持 identifier-only 不扩到 value | 标识符量级小不值 fuzzy 复杂度；value 是密钥值，搜索命中/高亮即泄密面 | settled |
| D5 | 按键统一方向 | 建中央 keymap/action 注册表，`?` help 与状态栏提示由注册表生成（消灭 `Help()` 字符串解析）；统一冲突键语义；迁移策略直接 break，不做 alias 过渡 | 按键与 help 不再可能漂移；个人工具 break 成本可由 help overlay + 状态栏提示覆盖 | settled |
| D6 | 补充易用性清单 | 必做：① `S` 搜索结果窗口化 ② history tab `q` 绕过 dirty-quit 守卫修复 ③ config All 伪组 spec/impl 不一致——**改 spec 承认现状**（允许 All 上组级 install/uninstall）④ PgUp/PgDn 全 Tab 补齐 ⑤ `g`/`G` 补齐 SSH/AI/MCP ⑥ config 新建 5 步向导、env/text 新建 flow 迁移 form 引擎 ⑦ `modalBox`/`cursorLine`/`clamp`/双栏几何等 helper 提升共享。顺带：`esc` 语义规则文档化、空状态覆盖检查、toast/audit 反馈一致性。不做：undo/redo | 修复类独立小改先行；一致性类依附组件落地；undo 复杂度与目标不匹配 | settled |
| D7 | 按键语义表 | 全局动词集 + 冲突裁决（完整表见下方附录）：`r`=rename 唯一（refresh→`Ctrl+R`，history restore→`R`）；`e`=edit 范式随实体；env 组激活/停用→`t`；AI model-only 切换→`M`；确认框统一 `y`/`enter` 确认、`esc`/`n` 取消，plan 确认收紧为仅 `esc`/`n` 取消其余键忽略；`esc`=回上一层 | 消灭四义键；plan「任意键取消」收紧后误按更安全；术语对齐 CONTEXT.md（撤回≠卸载、激活=env 组用词） | settled |
| D8 | 列表组件技术选型 | 自研薄层：基于现有 `windowedPane`/`clipLines` 等原语封装状态与按键，不引入 `bubbles/list` | `bubbles/list` 自带 keybinding/分页/preview 模型与双栏布局、D5 语义表、多选集语义全面冲突；仓库对它零依赖，改造成本高于自封装 | settled |
| D9 | 多选集语义 | ① `a` 全选=当前过滤可见集，选择集跨过滤持久（状态栏计数提示隐藏项）② 批量操作复用单项键（`d`/install/撤回/导出/激活停用），走既有 plan-preview / per-item confirm / drift `F` 语义；选择集为空回落游标项 ③ 单实体操作（`e`/`r`/`m`/detail）仅选择数 ≤1 可用 ④ 选择不跨栏（sidebar 焦点只是 scope 上下文）⑤ 批量操作提交后清空选择集 | 可见集全选是最低惊讶原则；复用单项键零新增按键学习；残留失效选择有害 | settled |
| D10 | 交付优先级 | ①修复批（search 窗口化/history guard/All spec 对齐）→ ②keymap 注册表+语义表落地 → ③共享列表组件+单 Tab 试点 → ④过滤组件+补齐 SSH/AI/MCP → ⑤多选集接入 → ⑥env/text 侧栏统一 → ⑦form 迁移 | ②先于③避免组件硬编码按键二次返工；修复批独立小改先行；侧栏复用③的双栏几何 | settled |
| D11 | mouse 支持边界 | 本期不纳入，列为非目标；help/README 明示键盘优先 | 与「统一键盘交互」目标正交，focus/selection 双轨复杂度不值 | settled |

## 附录：按键语义表（D7 settled，propose 直接吸收）

### 全局动词（所有列表 Tab 同义）

`n` 新建 · `e` 编辑 · `r` 重命名 · `d` 删除（栏焦点决定删组/删条目）· `x` 导出 · `u` 卸载（config）/撤回（MCP）· `i` 安装（config）或导入（text/ssh）· `enter` 详情/确认 · `space` 勾选 · `a` 全选/反选 · `g`/`G` 顶/底 · PgUp/PgDn 翻页 · `/` 过滤 · `S` 全局搜索 · `?` 帮助 · `Ctrl+R` 刷新 · `1-9`/Tab 切 Tab · `q` 退出（过 dirty-quit 守卫）

### 冲突裁决

| 键 | 现状 | 裁决后 |
|---|------|--------|
| `r` | rename / refresh / restore / keypair-rename 四义 | `r`=rename 全局唯一（含 keypair）；refresh=`Ctrl+R`；history restore=`R` |
| `e` | vim edit / form edit | `e`=edit 唯一；值型实体走 vim 闭环、结构型走 form，键义不分家 |
| env 组 `x` | deactivate group | 组激活/停用=`t`（toggle）；`x` 导出语义全局唯一 |
| `m` | meta / materialize / model-only 三义 | config `m`=meta、ssh `m`=materialize 保留（域内唯一）；AI model-only=`M`（与 `s`=全量切换成对） |
| `u` | uninstall（config）/ unexport（MCP） | 保留域内唯一；行文按 CONTEXT.md 区分「卸载/撤回」 |
| 确认框 | `y`/`enter` 确认；config/MCP plan「任意其他键=取消」 | `y`/`enter`=确认、`esc`/`n`=取消；plan 收紧为仅 `esc`/`n` 取消、其余键忽略 |
| `esc` | 清 filter / 关 modal / 向导回退 | 统一规则写入 help：`esc`=回上一层 |
| `U`/`I` | 各 Tab scope 变体 | 保留现状（scope 操作与多选集正交，见 D2/D9） |

## 术语表

| 术语 | 本任务语境下的定义 | 与既有用词的关系 |
|------|--------------------|------------------|
| 过滤（filter） | Tab 内 `/` 增量输入、对当前列表做即时子串筛选 | 沿用现有 `/` 行为，仅统一实现 |
| 全局搜索（search） | `S` 跨六类实体（Env/Text/Cfg/SSH/AI/MCP）的标识符子串搜索 | 与「过滤」区分；不做 fuzzy |
| 跳转（jump） | 从搜索结果定位并聚焦目标 Tab 的具体条目（`searchJumpMsg`） | 沿用 |
| 侧栏（sidebar) | config 式左栏：All 伪组置顶 + 各组过滤感知计数，双栏浏览的组维 | 由 config 范式推广为标准 |
| All 伪组 | 侧栏首个固定项，聚合全部条目，行前缀 `group/name` | 沿用 config 既有概念，推广到 env/text |
| 多选集（selection set） | `space` 勾选形成的任意条目子集，跨过滤状态持久 | 新术语，区别于游标单选 |
| scope 操作 | 作用于整组/全部范围的快捷键（如 config `I`/`U`、MCP `X`），与多选集正交 | 键沿用，术语新造 |
| keymap 注册表 | Tab 按键→动作的中央登记表，help/状态栏文案由其生成 | 新造，替代 `Help()` 字符串解析 |

## ADR 候选

<!-- 仅记录满足三门槛的；由 design.md 吸收或随 change 归档晋升 -->

- [ ] adr-self-built-list-component: 自研共享列表组件而非引入 `bubbles/list`——三门槛满足：8 Tab 全部迁移后换库代价大（难逆转）、官方库在手却不用会让后人费解、与 `bubbles/list` 的取舍真实（D1/D8）

（D5「直接 break 不做 alias」不记 ADR：后补 alias 即可逆，不满足三门槛。）

## 未决问题

无（frontier 已清空，两轮 11 项决策全部经用户确认）。
