# codex 凭据暴露名：优先复用被引用 env 名，text: 引用由默认组引用条目兜底

`senv ai switch codex` 只往 `~/.codex/config.toml` 写 `env_key` 名（明文不落盘，见 [ADR-0002](./0002-plaintext-key-after-switch.md)）。此前该名无条件由 alias 派生为 `SENV_<ALIAS>_API_KEY`，但没有任何 senv 命令会产生这个名字——`senv env export` 只导出 vault 里存的名字，于是 spec 承诺的补救路径（「用 senv env 能力把被引用 key 暴露给 codex」）不可执行：用户完全合理的 `credential_ref = env:ai/DEEPSEEK_API_KEY`（组已激活、shell 已在导出）下，codex 仍报 `Missing environment variable`；TUI 默认的 `[new own credential]`（`text:llm-keys/<alias>`）更是必然不可满足。同时 codex 是唯一不解密凭据的适配器，引用条目缺失时切换照样成功、静默产出坏配置（[ADR-0019](./0019-sync-ai-mcp-source-of-truth.md) 的 fail-closed 没覆盖它）。

现在改为：`env:<g>/<k>` → `env_key = <k>`（shell 里既有的名字，零额外操作）；`text:<g>/<k>`（含 alias 规范引用与自有凭据）没有 env 名可复用 → 保留派生名，并由切换在默认 env 组补一条 `SENV_<ALIAS>_API_KEY = {{text:<g>/<k>}}` 引用条目；名字只存在于未激活组时只提示 `senv env group activate <g>`，不补写重复条目。凭据决议（含解密校验）先于适配器写回，失败即零写入。

## 决策

1. **`env:` 引用复用引用 key 名**。「被引用 key 名」正是用户 shell 里 `senv env export` 已提供的名字，切换不需要写任何 vault 条目；env key 名在写入时已被约束为合法 shell 标识符（`internal/storage/validate.go`），跨机语义也稳定——`credential_ref` 随档案同步（ADR-0019），所以不存在「只有 alias 派生名才跨机稳定」的问题。
2. **`text:` 引用保留派生名 + 默认组引用条目兜底**。`text:` 没有环境变量名可复用；落点是默认 env 组，因为引用所在的 text 组（如保留组 `llm-keys`）不参与 env 导出，而默认组恒在导出集合内（`env group activate --help`：默认组总是激活）。
3. **兜底写入幂等且不覆盖**。目标名已存在于任何 env 组时一律不改值、不新增重复条目；同名条目解析后与本次凭据明文不同则告警（可能取到别的凭据），不静默覆盖用户既有的 shell 环境。
4. **决议先于写回，失败即零写入**。名字无法落地（读取分组失败、兜底写入失败）时切换失败且不触碰 agent 配置与指针。兜底写入无法被配置事务回滚，但残留的是一条引用条目、语义无害且幂等；反过来把它放到写回之后，就会出现「配置已写、名字却没落地」的静默坏状态。
5. **codex 也解密凭据以校验存在**。ADR-0019 的 fail-closed 统一适用于两个凭据族：明文只用不落盘，缺失时错误含引用全名与补齐指引。
6. **输出契约**：确切名字走 CLI stdout 信息行 / TUI 提示，需要用户动作的情形进 `Warnings`；CLI 逐条打印，TUI 拼接**全部** warning（此前只显示第一条，等于把补救提示截断）。
7. **同 change 内更新 `.agents/skills/senv-cli/SKILL.md`**，写清命名规则、两条补救命令与 codex app-server 只在启动时拷贝环境的坑。

## Considered Options

- **保持 alias 派生名，由 `senv env export` 为 codex 指向的 provider 增补别名行**：会让通用的 vault 导出操作依赖本机指针与档案，并把「哪些别名要导出」变成隐式魔法，同一 shell 的输出还会受 last-wins 顺序影响。
- **新增 `--env-key` 覆盖**：默认路径仍然错，还多一个可配置面；留待真正需要固定名的场景再加。
- **`text:` 引用复制明文进 env 组**：密钥出现第二份，轮换后不同步。
- **`text:` 引用 fail-closed 要求用户先手工建条目**：TUI 自有凭据是默认路径，等于让默认路径不可用。
- **兜底写入放在配置写回之后**：见决策 4，会产生本次变更要消灭的静默坏状态。

## Consequences

- codex 的 `env_key` 取值随 `credential_ref` 变化：`env:` 档案重跑切换后由 `SENV_<ALIAS>_API_KEY` 变为 `<key>`。指针与档案无需迁移（`senv ai status` 不展示 `env_key`）；回滚只需重跑旧版本切换。
- 默认组可能多出一条 senv 写入的引用条目，`senv env list` 可见、`senv env delete` 可删；它是 vault 变更，CLI/TUI 都会提示已写入。
- 明文暴露面不变：`env:` 引用只是改用既有名字；`text:` 引用落盘的是模板，明文仅在 `senv env export` 解析时进入 shell（与既有 env 组一致，仍属 ADR-0002 的取舍）。
- 已知局限：同名 key 并存于未激活组与激活组时，导出按激活组合并，codex 取到的值可能与该档案引用值不同；只在同名条目唯一时给出精确告警。
- codex 侧行为不可控：已运行的 `codex app-server`（Codex Desktop / SSH 远程）只在启动时拷贝环境，export 后需重启才能在 TUI 内生效；文档记录这一坑。

## Status

accepted（2026-09-14 随 change `codex-env-key-compat` 实现落地）
