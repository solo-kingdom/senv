## 1. 名字决议与凭据校验（internal/llm）

- [x] 1.1 新增 codex 凭据暴露名决议：`env:<g>/<k>` → `<k>`；`text:<g>/<k>` → `SENV_<alias 净化大写>_API_KEY` 并标记需要兜底。替换 `reqCredential` 在 codex 上的取名字路径。验证：`go build ./...`；`go test ./internal/llm/ -run 'TestProviderIDEscaping|TestCodexAdapterNoSecretOnDisk' -race`
- [x] 1.2 （高优先级）codex 切换也解密凭据以校验本机存在：引用缺失时 fail-closed（错误含引用全名与补齐指引），明文不落盘、不进输出。验证：新增测试「codex + 缺失引用 → 非零错误、`config.toml` 与 `.senv-bak` 零写入」
- [x] 1.3 导出集合适配：用 `env.Manager.Snapshot()` 的 `GroupInfo` 判定（与导出集合同语义，单趟取数）名字所属组是否在默认组 ∪ 已激活组内，不在则产出可操作 warning（含 `senv env group activate <g>`）；组信息读取失败即失败且零写入。验证：新增测试覆盖「未激活组告警」「默认组/激活组不告警」「读取失败报错」
- [x] 1.4 （高优先级）`text:` 引用兜底写入：在默认 env 组写入 `SENV_<ALIAS>_API_KEY = {{text:<g>/<k>}}`；同名条目已存在则不改值、不新增重复条目，值解析后与本次凭据明文不同则告警。验证：新增测试覆盖「默认组出现引用条目」「重跑幂等」「用户既有同名值不变且产出 warning」
- [x] 1.5 （高优先级）兜底写入/决议失败即零写入：错误路径不触碰 agent 配置与指针。验证：新增测试以只读 vault 或注入失败使 `env.Set` 报错，断言错误返回且无 `config.toml`/`.senv-bak`
- [x] 1.6 更新既有 codex 相关断言以匹配新契约（`TestSwitchCodexGuidesEnvVar` 断言名字来自档案引用，并断言兜底条目）。验证：`go test ./internal/llm/... -race` 全绿

## 2. CLI 输出（cmd）

- [x] 2.1 `cmd/ai_switch.go`：codex 分支改为打印**确切**名字与「由 `senv env export` 提供」的说明（不再要求用户手工设置）；warning 全部打印，不合并。验证：新增/更新 `cmd/ai_switch_test.go` 断言 stdout/stderr 文本；`go run . ai switch --help` 文案仍准确
- [x] 2.2 补 CLI 集成测试：`env:` 引用场景输出含引用 key 名、未激活组场景输出含激活命令。验证：`go test ./cmd/... -run 'AISwitch' -race`

## 3. TUI 对等（internal/tui）

- [x] 3.1 `internal/tui/ai_tab.go` 切换成功提示使用本次结果的 `CredentialEnv`，并拼接**全部** warning（当前只取第一条）；失败路径不展示凭据暴露提示。验证：新增 TUI 测试覆盖「env: 引用提示引用 key 名」「多条 warning 全部出现」「pi 切换无凭据提示」
- [x] 3.2 TUI 测试与实现配对：`go test ./internal/tui/... -race` 全绿，且既有 codex 提示断言同步更新

## 4. 文档与决策记录

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md`：写明 codex `env_key` 命名规则（`env:` 复用被引用 key 名；`text:` 派生名 + 默认组引用条目兜底）、两条补救命令（`senv env group activate <g>`、同名条目冲突时核对）、以及已运行的 `codex app-server` 不刷新环境的坑。验证：对照 `senv env export`/`env group activate` 的 `--help` 文案逐条核对
- [x] 4.2 新增 `docs/adr/0024-codex-credential-env-name.md`：记录「优先复用被引用 env 名 + text 引用由默认组引用条目兜底」及其被否方案。验证：与 design.md D1/D2/D3 结论一致，无未记录取舍

## 5. 验收

- [x] 5.1 真机验证 `env:` 引用路径：以本机 `deepseek` 档案（`credential_ref=env:ai/DEEPSEEK_API_KEY`）执行 `go run . ai switch codex deepseek`，确认 `~/.codex/config.toml` 的 `env_key` 为 `DEEPSEEK_API_KEY`、vault 无新增条目；`eval "$(senv env export)"` 后重启 app-server，`codex exec -m deepseek-flash "ok"` 成功。验证：命令输出与 `codex exec` 结果
- [ ] 5.2 全量回归：`make check`（fmt + vet + lint + test）与 `openspec validate codex-env-key-compat --strict` 全绿。验证：命令退出码为 0
