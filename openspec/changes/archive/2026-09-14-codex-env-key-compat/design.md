## Context

动机见 proposal.md。与本设计相关的现状约束：

- `internal/llm/switch.go` 的 `Switch()` 流程为：解析模型集/默认模型 → 校验 `api_shape` → 取凭据 → 适配器写回（事务）→ 更新指针。凭据在 `CredentialInline` 时解密，在 `CredentialEnvVar`（仅 codex）时只算出名字，**从不校验条目是否存在**。
- codex 的 `env_key` 名由 `senvEnvKeyName(alias)` 合成，与 `credential_ref` 无关。
- `senv env export` 的语义（`internal/env/manager.go`）：导出集合 = 默认组 ∪ 已激活组；同名 key 后写覆盖先写（last-wins）；引用 `{{env:...}}`/`{{text:...}}` 在导出时解析；输出直接进用户 shell rc 的 `eval`。
- env 条目的 key 名在写入时已被约束为 `[A-Za-z_][A-Za-z0-9_]*`（`internal/storage/validate.go`），`credential_ref` 里的 key 名天然是可用环境变量名。
- 组激活状态是本机状态（不同步），默认组恒在导出集合内。

## Goals / Non-Goals

**Goals:**

- codex 的 `env_key` 与「本机 shell 里已经/能够拿到的名字」对齐，使切换后无需任何手工步骤即可工作。
- 名字不可解析时给出确定、可复制的补救路径，而不是笼统提示。
- codex 复用既有的凭据缺失 fail-closed 诊断。

**Non-Goals:**

- 不注入进程环境、不做凭据代理（沿用既有边界）。
- 不改变 `senv env export` 的输出语义与默认组/激活组的定义。
- 不改动 `llm_providers` 档案结构或指针结构。

## Decisions

### D1：`env:` 引用直接复用引用 key 名

`credential_ref = env:<g>/<k>` 时 `env_key = <k>`。理由：这正是用户 shell 里 `senv env export` 已经提供的名字，零额外操作；且 key 名已受 shell 标识符约束。

被否方案：
- **保持 alias 派生名，由 `senv env export` 为 codex 指向的 provider 增补别名行**：会让 export 依赖本机指针与档案（export 是通用 vault 操作），并把「哪些别名要导出」变成隐式魔法，同一 shell 的输出依赖 last-wins 顺序。
- **新增 `--env-key` 覆盖**：多一个可配置面，且默认路径仍然错；本次不做（后续如有固定名需求再加）。

### D2：`text:` 引用保留派生名，由默认组引用条目兜底

`credential_ref = text:<g>/<k>`（含自有凭据 `text:llm-keys/<alias>`）没有环境变量名可用，`env_key` 仍为 `SENV_<alias 净化大写>_API_KEY`，并在**默认 env 组**写入同名条目，值为 `{{text:<g>/<k>}}`：导出时解析为同一凭据，无需复制明文。

选默认组的理由：`text:` 引用的组（如保留组 `llm-keys`）是 text 组，不是可导出的 env 组；默认组恒在导出集合内，是唯一无需额外激活动作的落点。

被否方案：
- **复制明文到 env 组**：密钥出现第二份，轮换后不同步。
- **fail-closed 要求用户先手工建条目**：TUI `[new own credential]` 是默认路径，等于让默认路径不可用。
- **写入引用所属的 text 组**：text 组不参与 env 导出，等于没写。

### D3：决议先于写回，失败即零写入

凭据决议 + 兜底写入发生在适配器写回之前：

1. 解析凭据引用得到名字与是否需要兜底；
2. 需要兜底时检查目标名是否已存在于任何 env 组：不存在 → 写入默认组引用条目；已存在 → 不动，解析其值并与本次凭据明文比较，不同则告警（可能取到错误凭据）；
3. 检查名字所属组是否在导出集合内，不在则产出 warning（`senv env group activate <g>`）；
4. 任一步失败 → 返回错误，`config.toml` 零写入（与凭据缺失的 fail-closed 一致）。

兜底写入是不可回滚的 vault 变更（配置事务管不到它）。允许这一残留的理由：写入的是引用条目，即使随后配置写回失败，它也只指向既有凭据、语义无害；反之若把它放到写回之后，就会出现「配置已写、名字却没落地」的静默坏状态，正是本次要消灭的失败模式。

### D4：导出集合判定复用 env.GroupInfo

用 `env.Manager.Snapshot()` 一次取回全部分组与变量及其 `GroupInfo`（`IsActive` 已含默认组），避免在 llm 包内重复实现导出集合语义，也避免两次遍历 vault。

已知局限（不额外处理）：同名 key 同时存在于未激活组与激活组时，导出按激活组合并，codex 取到的值可能与该档案的引用值不同——D3 的值比较只在「同名条目唯一」时给出精确告警。

### D5：输出契约沿用 SwitchOutput

`SwitchOutput.CredentialEnv` 继续承载最终写入的确切名字（含 `env:` 引用场景），可操作提示进入既有 `Warnings`；CLI 打印一行 info（名字 + 由 `senv env export` 提供）与全部 warning，TUI 提示拼接**全部** warning（当前只拼接第一条）。不新增同义结构字段。

### D6：文档与 ADR

更新 `.agents/skills/senv-cli/SKILL.md`：写清 codex 的 `env_key` 命名规则（`env:` 复用、`text:` 派生 + 默认组兜底）与两条补救命令。新增 ADR 记录「codex 凭据暴露名优先复用被引用 env 名 + text 引用由默认组引用条目兜底」，替代此前无决策记录的隐式约定。

## 数据流

```
senv ai switch codex <alias>
  ├─ GetProvider(alias)                         # 档案
  ├─ resolveCredential(entry)                   # env:/text: 解密（校验存在，fail-closed）
  ├─ codexEnvName(entry)                        # 名字决议
  │    ├─ env:<g>/<k>  → <k>
  │    └─ text:<g>/<k> → SENV_<ALIAS>_API_KEY，并标记需要兜底
  ├─ ensureEnvNameExportable(name, entry)       # 兜底写入 / 值比较告警
  │    ├─ ListGroups() → 名字在默认组或激活组？否则 warning
  │    └─ 名字不存在 → env.Set(默认组, name, "{{text:<g>/<k>}}")
  ├─ adapter.Apply(env_key = name)              # 事务写 config.toml + catalog
  └─ SavePointers()                             # 指针
输出：名字（info）+ warnings（组未激活 / 同名值不匹配 / 元数据缺失）
```

## 错误处理策略

| 情形 | 行为 |
|------|------|
| 凭据引用在本机不存在 | 失败，零写入，错误含引用全名与 `senv env set`/`senv text add` 或跨机同步指引（codex 现在也走这条路径） |
| 兜底写入失败（锁/权限/加密） | 失败，零写入，错误含失败原因与手工命令 |
| 同名条目已存在且值不同 | 不覆盖、不失败；warning 指出该名字当前解析到其它凭据，可能认证失败，并给出手工核对命令 |
| 名字所在组未激活 | 不失败；warning 指出导出不会包含该名字，给出 `senv env group activate <g>` |
| 导出集合判定失败（读取组信息出错） | 失败，零写入（判定是前置条件，不做乐观假设） |

## 迁移与向后兼容

- vault 无格式变更：env 组仍是既有 `EnvGroup` JSON，只是新增一条值为引用的条目；旧版本 senv 能正常读取与导出。
- alias 派生名仍存在（`text:` 路径），存量 `SENV_<ALIAS>_API_KEY` 条目与已切换的 codex 配置不被破坏。
- `env:` 引用的档案首次重跑切换后 `env_key` 由 `SENV_<ALIAS>_API_KEY` 变为 `<k>`；指针与档案无需迁移，`senv ai status` 不展示 `env_key`，无额外对齐动作。
- 回滚：重跑旧版本 senv 的 `senv ai switch codex` 会写回派生名；默认组多出的引用条目可 `senv env delete` 删除，不影响配置正确性（该名字只被 codex 使用）。

## 使用示例

```bash
# env: 引用：切换后即用，无需额外操作
senv ai switch codex deepseek
#   ⚠ 输出：codex 从环境变量 DEEPSEEK_API_KEY 读取凭据（由 senv env export 提供）

# 自有凭据：senv 在默认组写入引用条目
senv ai switch codex my-provider
#   默认组新增 SENV_MY_PROVIDER_API_KEY
senv env list | grep SENV_MY_PROVIDER_API_KEY   # 值为 {{text:llm-keys/my-provider}}

# 引用组未激活
senv ai switch codex stag-provider
#   ⚠ 组 dev 未激活：senv env export 不会包含 APP_KEY；senv env group activate dev
```

## Risks / Trade-offs

- **默认组被写入条目** → 仅在 `text:` 引用场景发生，条目为引用、无明文，且用 `senv env list` 可见、可删；文档与切换输出会说明。
- **同名条目值不匹配时 codex 认证失败** → 不覆盖用户值，改为 warning + 手工核对命令；不做静默覆盖（避免破坏用户既有 shell 环境）。
- **引用组未激活导致 codex 仍报 missing env** → warning 前置给出激活命令；这是本机状态，无法由 senv 自动改变。
- **已运行的 `codex app-server` 不刷新环境** → 属 codex 侧行为，非本变更可解；在 skill 文档中记录（`pkill -f 'codex app-server'` 后重开）。
- **`text:` 兜底写入在配置写回失败时残留** → 见 D3，残留语义无害且幂等。
