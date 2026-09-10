## Why

Provider 档案只有 `add/list/show/remove`（`internal/llm/provider.go` 无 edit），改一个字段要「删了重建」，而重建必须重新录入凭据。AI Tab 因此被限定为「浏览 + 切换」——上一个 change 明确把档案增删改列为非目标——但界面上仍然难用：三栏在窄终端互相挤压导致长行硬换行，`←→/hl` 改的 `focusLeft` 从没被 `View` 读取，中/右栏没有键位可达，switch 之后也没有换模型入口。接入形态（chat/responses/anthropic）无法在档案上声明，只能靠 agent 协议族隐式推断。

## What Changes

- 后端新增 `EditProvider`：改 base_url、模型集、default_model、目录来源、凭据引用与凭据轮换；alias 不可改（它是各 agent 配置里的 `senv-<alias>` 标识，改名等价于重新切换）。
- 新增 CLI `senv ai provider edit` 与 TUI 编辑入口，校验与轮换语义与 `add` 一致。
- 档案新增可选 `api_shape`（`openai-chat` | `openai-responses` | `anthropic`）：留空沿用「按 agent 协议族归一接入地址」，非空时以其为准并作为 switch 的兼容判据（见 ADR-0006）。
- AI Tab 重构为两栏：左 Providers（`n/e/d` 写操作、`enter` 详情弹层），右 Agents（`↑↓` 选 agent、`s` 用当前 provider 切换、`m` 给已指向 agent 换模型、`enter` 看配置路径与凭据提示）；焦点左右切换真正生效。
- 凭据录入：默认从既有 env/text 条目挑选引用，另提供遮蔽输入（`EchoMode=password`）新建凭据路径；明文不进渲染状态。

## Non-goals

- 不引入「一次切换多个 agent」的批量语义，也不新增 provider 级「默认应用」字段（指针仍是 per-agent 本机状态，ADR-0003）。
- 不做从 base URL 推断形态（ADR-0006 已否决猜测）。
- 不改切换的内容写回格式与回滚机制。

## Capabilities

### Modified Capabilities

- `llm-provider`: 新增编辑档案能力与 `api_shape` 字段。
- `llm-provider-tui`: 两栏重构、档案写操作、凭据录入、仅换模型入口。
- `llm-provider-switch`: 形态兼容判定与仅换模型语义。

## Impact

`internal/storage`（`LLMProviderEntry` 加字段与校验）、`internal/llm`（`EditProvider`、归一覆盖、切换兼容判定）、`cmd/ai_provider.go`、`internal/tui/ai_tab.go` 重写布局与动作。存量档案不迁移：字段缺省即旧行为。
