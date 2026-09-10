## Why

TUI 把「编辑」当成少数几个动作的集合，而不是数据的通用能力：env 与 text 的 key、config 的条目名都不能重命名；env 连删除分组都没有（`internal/env` 没有 `DeleteGroup`），text 有后端与 CLI 却无键位；config 的分组与描述只在创建时能写（`config.Manager.SetMeta` 无生产调用方）；text 不能从文件导入（`SetFromFile` 存在但未接）。同时每个 Tab 各写一套内联模态，继续按此扩展会让键位与校验漂移。

## What Changes

- 新增通用表单组件（`internal/tui`）：字段列表、`Tab`/方向键切换、内联校验错误、`enter` 提交、`esc` 取消；`InputMode` 期间不劫持全局键。短字段走表单，自由属性/多行内容继续走既有 `$EDITOR` 闭环。
- 存储层新增原子化的重命名与分组管理：`env.Manager.RenameKey`/`RenameGroup`/`DeleteGroup`、`text.Manager.RenameKey`/`RenameGroup`、`config.Manager.Rename`；复用 `mutate` 加锁与原子写。
- Env Tab：key 重命名、分组重命名、分组删除（default 分组不可删）。
- Text Tab：key 重命名、分组重命名、分组删除、从文件导入（复用 `SetFromFile`）。
- Config Tab：条目重命名、元信息编辑（分组、描述，启用既有 `SetMeta`）。

## Non-goals

- 不新增 CLI 命令（能力先由存储层提供并只在 TUI 暴露；CLI 对等不在本次范围）。
- 不做批量/多选编辑、undo、跨分组移动条目。
- 不改存储格式、不迁移存量数据，重命名不得改变条目内容与权限。
- 不为 env 引入新的分组语义（激活态与 default 规则不变）。

## Capabilities

### New Capabilities

- `tui-forms`: TUI 结构化编辑的通用表单契约（字段类型、键位、校验与取消）。

### Modified Capabilities

- `tui-viewer`: Env/Text/Config 三个 Tab 的编辑动作扩展（重命名、分组管理、元信息、从文件导入）。

## Impact

`internal/env`、`internal/text`、`internal/config` 各新增 rename/分组方法（含单测）；`internal/tui` 新增表单组件并替换已有手写模态的重复逻辑。无 vault 格式变更，无新增依赖。
