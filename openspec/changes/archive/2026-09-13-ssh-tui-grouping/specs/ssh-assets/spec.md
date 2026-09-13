## MODIFIED Requirements

### Requirement: Host CRUD 与字段模型
系统 SHALL 提供 `senv host` 命令组（`add`、`get`、`edit`、`list`、`delete`）。alias SHALL 全局唯一并作为主键；核心字段为 hostname、user、port、proxyJump、identityKey、group、tags；除核心字段外 SHALL 接受任意额外 KV 并原样保存。`group` 为单值归属字段，空值表示未分组，不参与 ssh config 导出与 materialize；`tags` 为多值自由标注，与 group 正交。

#### Scenario: Add host with core fields
- **WHEN** 用户执行 `senv host add web --hostname 10.0.0.1 --user deploy --port 2222`
- **THEN** 系统 SHALL 以 alias `web` 存储 host 记录

#### Scenario: Add host with group
- **WHEN** `senv host add web ... --group prod`
- **THEN** 记录 SHALL 保存 `group: prod`

#### Scenario: Add host with extra attributes
- **WHEN** `senv host add web ... --attr forwardAgent=yes --attr serverAliveInterval=30`
- **THEN** 系统 SHALL 原样保存这两个 KV

#### Scenario: Duplicate alias
- **WHEN** 要添加的 alias 已存在
- **THEN** 系统 SHALL 报错并拒绝

#### Scenario: 既有数据无 group 字段
- **WHEN** 读取旧版本写入的 host（无 `group` 字段）
- **THEN** 系统 SHALL 按空 group（未分组）处理，读写均正常

## ADDED Requirements

### Requirement: TUI SSH 分组侧栏
TUI SSH Tab SHALL 以三栏呈现：分组侧栏 → 组内 Host 列表 → KeyPair 列表，与 Env/Text/Config 的分组侧栏交互一致。侧栏 SHALL 包含「All」伪组置顶、各组按组名字母序展示条目数、空 group 的 Host 归入「未分组」组置底且仅在有未归类 Host 时出现。组内 Host 按别名字典序排列。选中组决定中间栏展示的 Host 集合；「All」展示全部。

#### Scenario: 按组浏览 Host
- **WHEN** 用户在侧栏选中组 `prod`
- **THEN** 中间栏 SHALL 仅展示 `group: prod` 的 Host，按别名排序

#### Scenario: All 伪组
- **WHEN** 用户在侧栏选中「All」
- **THEN** 中间栏 SHALL 展示全部 Host，分组状态不被改变

#### Scenario: 未分组兜底
- **WHEN** 存在未设置 group 的 Host
- **THEN** 侧栏 SHALL 在字母序组之后显示「未分组」组；不存在未归类 Host 时该组 MUST NOT 出现

#### Scenario: 全部 Host 均有分组
- **WHEN** 所有 Host 均设置了非空 group
- **THEN** 侧栏 SHALL 不显示「未分组」

### Requirement: TUI Host 行内 tags 展示
Host 列表行 SHALL 在行尾展示 tags，格式为 `#tag` 前缀，最多 2 个，超出以 `+n` 汇总；行宽不足时 MUST 截断且不得折行。行内 MUST NOT 展示 group（group 由侧栏表达）。

#### Scenario: 有 tags 的 Host
- **WHEN** host `web` 的 tags 为 `[gpu, p0]`
- **THEN** 列表行 SHALL 在行尾展示 `#gpu #p0`

#### Scenario: tags 超过 2 个
- **WHEN** host `web` 的 tags 为 `[a, b, c, d]`
- **THEN** 行 SHALL 展示 `#a #b +2`

#### Scenario: 无 tags
- **WHEN** host `web` 未设置 tags
- **THEN** 行 SHALL 不渲染 tags 片段

### Requirement: KeyPair 栏引用计数与过滤
TUI KeyPair 列表 SHALL 支持 `/` 过滤（按 keypair 名称匹配）。每行 SHALL 内联展示被 Host 引用的计数（`被 N 个 Host 引用`）；零引用的 KeyPair SHALL 以「未被引用」灰显提示。KeyPair 列表 MUST 保持按名字典序排列，引用计数 MUST NOT 改变排序。

#### Scenario: 有过滤输入时缩小 KeyPair 列表
- **WHEN** 焦点在 KeyPair 栏且用户输入 `/web`
- **THEN** 列表 SHALL 仅保留名称匹配的 KeyPair

#### Scenario: 引用计数展示
- **WHEN** keypair `web-key` 被 3 个 Host 引用
- **THEN** 行 SHALL 展示 `被 3 个 Host 引用`

#### Scenario: 零引用提示
- **WHEN** keypair `old-key` 未被任何 Host 引用
- **THEN** 行 SHALL 灰显「未被引用」，且该行位置仍按名字序排列

### Requirement: SSH 过滤与全局搜索匹配范围
SSH Tab 的 `/` 过滤 SHALL 匹配 alias、hostname、tags 与 group 四个维度。全局搜索（`S`）在 SSH 类目下 SHALL 匹配 alias、hostname、tags 与 group。

#### Scenario: 按 tag 过滤
- **WHEN** 用户在 Host 栏输入 `/gpu`
- **THEN** tags 含 `gpu` 的 Host SHALL 保留，其余过滤掉

#### Scenario: 按 group 过滤
- **WHEN** 用户在 Host 栏输入 `/prod`
- **THEN** `group: prod` 的 Host SHALL 保留

#### Scenario: 全局搜索命中 group
- **WHEN** 用户按 `S` 搜索 `prod`
- **THEN** SSH 类目的结果 SHALL 包含 `group: prod` 的 Host

#### Scenario: 全局搜索命中 tag
- **WHEN** 用户按 `S` 搜索 `gpu`
- **THEN** SSH 类目的结果 SHALL 包含 tags 含 `gpu` 的 Host
