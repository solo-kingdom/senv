## Context

承接 ssh-tui-grouping（未归档）：SSH Tab 已是「分组侧栏 → Host 列表 → KeyPair 列表」三栏（`internal/tui/ssh_tab.go` 约 1758 行），KeyPair 栏有引用计数与过滤，但在窄终端被压至 22 列下限，host 与 keypair 同屏信息过载。Tab 注册在 `internal/tui/model.go:117` 的 `New()`，条件注入（`mgr.SSH != nil` → SSH Tab），数字键 1-9 按注册顺序直达，加 KeyPair Tab 后 server 模式共 9 个 Tab，仍全覆盖。KeyPair 管理动作（导入/重命名/删除/落盘）目前全部在 sshTab 上（`enterImportKeyPair`/`enterRenameKeyPair`/`doDeleteKey`/`doMaterialize`，ssh_tab.go 1085-1230 行）；引用关系统计 `hostRefs()`（839 行）。存储为 per-entry 加密 JSON（`internal/storage/ssh.go`），`KeyPairEntry`（`internal/storage/types.go:89`）加字段天然兼容。

## Goals / Non-Goals

**Goals:**
- KeyPair 获得与 Host 完全同构的分组能力（模型、侧栏、兜底组语义一致）
- SSH Tab 与 KeyPair Tab 各自两栏，宽度不再互相挤压
- keypair 全部动作平移，删除保护/落盘确认等安全语义原样保留

**Non-Goals:**
- 全局搜索纳入 keypair（现状不搜索，维持）
- KeyPair tags
- 跨 Tab 行渲染抽象

## Decisions

### D1: KeyPair 独立 Tab，而非加宽 SSH Tab 或 Tab 内切子视图

**选择**：新 `keypairTab`，`mgr.SSH != nil` 时紧随 SSH Tab 注册；SSH Tab 删去 KeyPair 栏与相关动作/focus 态，回到两栏。

**理由**：用户明确点名；KeyPair 有自己的分组维度和动作集，与 Host 的关联只剩「被引用计数」一个方向，独立后信息架构更顺（Host 管连接，KeyPair 管密钥）。Tab 注册机制现成，成本主要是 ssh_tab 瘦身。

**备选**：SSH Tab 内子视图切换（`[`/`]` 切 host/keypair 视图）——隐藏一种数据类型，违背「并列可见」的初衷，弃。

### D2: KeyPair 分组复用 Host 的 group 语义与侧栏组件

**选择**：`KeyPairEntry.Group string`（`group,omitempty`），侧栏复用 `renderSidebar`/`SidebarRow`，排序 All 置顶 → 字母序 → 「未分组」置底。

**理由**：与 Host 分组同构，用户心智与代码路径都只有一份；上一 change 已把组排序/兜底逻辑验证过一遍。group 是纯组织维度，不进 materialize/导出。

### D3: 动作平移而非复制

**选择**：keypair 导入/重命名/删除/落盘/详情从 sshTab 物理迁移到 keypairTab（删除 sshTab 中的对应代码），SSH Tab 保留 host CRUD 与 OpenSSH 导出。

**理由**：双份维护必然漂移；SSH Tab 的 Host 表单 keypair 选择器保留（那是 host 编辑的一部分）。`hostRefs()` 迁到 keypairTab；SSH Tab Host 行继续内联 `key:name(fp)`，不需要引用计算。

### D4: KeyPair 分组编辑入口

**选择**：TUI 提供 keypair 的 group 编辑（导入表单加 group 字段；列表上 `e` 打开仅含 group 的小表单）；CLI `keypair import --group`（rename 已有，edit 无——group 修改走 TUI 或重导入？）。

**理由**：最小闭环是导入时设组 + TUI 改组。CLI 不加 `keypair edit` 子命令（只有一个字段，不值一个命令；需要脚本化时用户可用既有路径重设）。如后续有需求再补。

**风险**：脚本化用户无法改 keypair 分组 → 缓解：TUI 编辑覆盖交互场景；真有呼声再加 CLI。

### D5: 全局搜索不动

**选择**：`S` 搜索维持只搜 host，不纳入 keypair。

**理由**：现状即如此（search.go 只收集 host）；纳入 keypair 需要 jump 到 keypair tab 的新链路，超出本次范围。

## Risks / Trade-offs

- [两个 Tab 的来回切换成本] → Host 行内联 `key:name(fp)` 保留关联可见性；KeyPair 行内引用计数反向可见，双向定位够用。
- [既有测试大面积依赖 tab 数量/顺序与 ssh_tab 三栏] → 适配集中在 model/ssh_tab 测试与 review_fixes 测试；tasks 专列。
- [ssh_tab 瘦身误删共享 helper] → keypair 相关 helper 随动作整体搬迁，编译器未使用即报错兜底；`make check` 全绿为准。

## Migration Plan

无需数据迁移（`group` 缺省 = 未分组）。行为迁移：TUI 中 keypair 操作位置变化（SSH Tab 右栏 → 独立 KeyPair Tab），README/SKILL.md 同步说明。回滚 = revert 本 change。注意归档顺序：ssh-tui-grouping 须先于本 change 归档（本 change 的 REMOVED 针对其 ADDED 的「KeyPair 栏引用计数与过滤」）。
