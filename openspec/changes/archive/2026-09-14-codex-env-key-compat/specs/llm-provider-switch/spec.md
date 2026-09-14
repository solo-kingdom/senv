## MODIFIED Requirements

### Requirement: 凭据按 agent 格式落地
切换时 SHALL 从 vault 解密凭据引用（`text:` 经 text manager、`env:` 经 env manager）并按 agent 原生格式写入配置：支持文件内凭据字段的 agent（claude-code、zcode、kimi、pi、opencode）直接写入并收敛文件权限为 0600；codex 的 TOML 配置只写 `model`/`model_provider`/`base_url`/`env_key`，凭据 MUST NOT 写入文件。codex 的切换 SHALL 同样解密凭据引用以校验其在本机存在：引用缺失时 MUST 拒绝切换且零写入（诊断含引用的完整名字与修复指引），明文 MUST NOT 落盘、MUST NOT 进入命令输出。成功输出 SHALL 含写进 `env_key` 的确切环境变量名。

#### Scenario: 文件内凭据 agent
- **WHEN** 切换 pi 至某 provider
- **THEN** `~/.pi/agent/models.json` 中对应 provider 含解密后的 apiKey，文件权限为 0600

#### Scenario: codex 凭据不落盘
- **WHEN** 切换 codex 至某 provider
- **THEN** `config.toml` 含模型与 provider 定义及 `env_key` 名，不含任何密钥明文

#### Scenario: codex 凭据引用缺失
- **WHEN** 档案的 `credential_ref` 指向本机 vault 中不存在的条目且切换 codex
- **THEN** 切换以非零退出、错误含该引用的完整名字与补齐指引、`config.toml` 零写入

## ADDED Requirements

### Requirement: codex 凭据暴露名可被 senv env 导出
`senv ai switch codex` 写入 `env_key` 的环境变量名 SHALL 保证可由 `senv env export` 提供，使 codex 无需任何额外手工步骤即可取到凭据：

- `credential_ref` 为 `env:<g>/<k>` 时，`env_key` SHALL 为 `<k>`（即该条目在 shell 中的既有名字），且切换 MUST NOT 新增或修改任何 env 条目。
- `credential_ref` 为 `text:<g>/<k>`（含 alias 规范引用 `text:llm-keys/<alias>`）时，`env_key` SHALL 为 `SENV_<alias 净化并大写>_API_KEY`，且切换 SHALL 在**默认 env 组**写入一条同名条目，其值为对 `text:<g>/<k>` 的引用（`{{text:<g>/<k>}}`），使该名字随导出解析为同一凭据。
- 该兜底写入 SHALL 幂等：目标名字已存在于任何 env 组时 MUST NOT 覆盖其值、MUST NOT 新增重复条目。
- 名字所处 env 组不在导出集合（默认组 ∪ 已激活组）内时，切换 SHALL 输出可操作 warning，指明该组与激活命令（如 `senv env group activate <g>`）。
- 命令 MUST NOT 注入进程环境、MUST NOT 引入凭据代理或启动包装。

#### Scenario: env 引用沿用既有变量名
- **WHEN** 档案 `credential_ref` 为 `env:ai/DEEPSEEK_API_KEY`（`ai` 组已激活）且切换 codex
- **THEN** `config.toml` 的 `env_key` 为 `DEEPSEEK_API_KEY`，vault 中无新增 env 条目，输出含该名字

#### Scenario: 自有凭据由默认组引用条目兜底
- **WHEN** 档案 `credential_ref` 为 `text:llm-keys/main` 且切换 codex
- **THEN** `env_key` 为 `SENV_MAIN_API_KEY`，默认 env 组出现 `SENV_MAIN_API_KEY` 且其值为 `{{text:llm-keys/main}}`，`senv env export` 输出该变量

#### Scenario: 不覆盖既有同名条目
- **WHEN** 默认 env 组已存在用户自己设置的 `SENV_MAIN_API_KEY` 且档案 `credential_ref` 为 `text:llm-keys/main` 时切换 codex
- **THEN** 该条目值与数量均不变，切换不报错，输出仍含最终 `env_key` 名

#### Scenario: 引用组未激活时提示
- **WHEN** 档案 `credential_ref` 为 `env:dev/APP_KEY` 且 `dev` 组未激活时切换 codex
- **THEN** 切换成功且输出 warning，指出导出不会包含该名字并给出 `senv env group activate dev`
