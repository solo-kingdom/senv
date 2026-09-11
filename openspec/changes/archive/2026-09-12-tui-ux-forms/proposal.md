## Why

config 创建是 5 步手搓 `textinput` 向导、env 新建是手搓连续弹窗，均偏离 `tui-forms` 结构化表单契约（内联校验、`tab` 导航、`esc` 无副作用、secret 遮蔽）；其余编辑界面（SSH/AI/MCP/config meta）早已在表单引擎上。grill D6-⑥ 已定迁移。

## What Changes

- config `n` 创建迁移为结构化表单：name、源文件路径、target 路径、分组、描述一次收集，必填内联校验，`esc` 取消零副作用，后端失败经 reopen 模式回填表单
- env `n` 新建迁移为结构化表单：key（组内冲突内联校验）+ value（secret 遮蔽输入，明文不进任何渲染文本）
- 移除两处手搓 modal 状态机代码

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `tui-viewer`: 「Env Tab 浏览与操作」新建场景改为表单+遮蔽；「Config Tab 浏览与操作」创建场景改为表单（基于 fixes/侧栏之后的基线）
- `config-tui`: ADDED「创建配置走结构化表单」

## Impact

- 代码：`internal/tui/config_tab.go`、`env_tab.go`、`form.go`（复用既有字段类型，无新类型）
- 文档：`.agents/skills/senv-cli/SKILL.md`

## Non-goals

- text 新建保持 vim 闭环（自由长文本不适合表单）；不改 form 引擎契约（tui-forms spec 不动）；SSH/AI/MCP 表单已在引擎上，不动

## 验证记录
- 2026-09-11（分支 tui-ux）：env 新建迁移结构化表单（key 必填+组内冲突校验、value=formSecret 遮蔽、All 视图要求先选具体分组）；config 创建迁移结构化表单（name 重名校验/source/target 必填/分组预填当前组/描述），提交失败经 reopen 模式回填；手搓状态机整段移除（envModeNewKey/NewValue、configModeCreate*、pending* 暂存字段及对应 submitModal/renderModal 分支）；测试重写为表单流（API_KEY 持久化、All 视图拒绝无组新建）；SKILL.md 补表单化新建描述；`make check` 全部通过。
