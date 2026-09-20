# Tasks

## 1. pi 适配器投影 compat

- [x] 1.1 `internal/llm/switch.go`：`piAdapter().Apply` 的 provider 对象新增 `"compat": map[string]any{"supportsDeveloperRole": false}`，并加注释说明「这是 provider 级兼容开关而非模型元数据投影，不受缺失即省略规则约束；senv 整体拥有该 provider 对象，用户手工补丁会被下次切换覆盖」，同时点出 failure mode（自建网关不在 pi 的 base URL 特征名单内 → reasoning 模型发 `developer` 角色 → 上游 400）。验证：`go build ./...` 通过；`grep -n "supportsDeveloperRole" internal/llm/switch.go` 命中该常量
- [x] 1.2 `internal/llm/switch_test.go`：扩展既有 `piAdapter` 用例，断言 `providers.<id>.compat.supportsDeveloperRole == false`、该键写在 provider 级（不在任何 `models[]` 条目内）、`compat` 中不含 `supportsReasoningEffort`。验证：`go test ./internal/llm/ -run 'Pi|pi' -v` 通过（修复前应先红）

## 2. 回归与边界

- [x] 2.1 `internal/llm/modelset_projection_test.go`：覆盖两条易回归路径——(a) 模型条目带推理档位时 `reasoning: true` 与 provider 级 `compat` 并存；(b) 以 `api_shape: openai-responses` 切换 pi 时 `api` 写 `openai-responses` 且 `compat` 仍在。验证：`go test ./internal/llm/ -v` 通过
- [x] 2.2 确认无溢出：无元数据的模型（不写 `reasoning`）条目仍未新增任何字段，`compat` 只在 provider 级出现；`settings.json`（`defaultProvider`/`defaultModel`/`enabledModels`）输出与变更前逐字节一致。验证：既有 pi settings 用例 + `enabledModels` 重排用例全绿

## 3. 验证

- [x] 3.1 实际门禁：`openspec validate --strict --type change pi-compat-developer-role` 通过；`golangci-lint run --new-from-rev=origin/main ./...` = 0 issues、`go vet ./...` 干净、`go test ./...` + `go test -race ./internal/llm/` 全绿。说明：仓库 `make lint` 跑全量 `golangci-lint run ./...` 时报 88 条存量 issue（unused/errcheck/staticcheck），均在 HEAD 预先存在（含 `internal/llm/switch.go:266 restoreFromBackup` / `282 tomlEdit` / `1368 sortedContains`、`internal/llm/transaction.go:168 contains`），不是本次改动引入；与本仓既有惯例（`tasks/archive/2026-09-19/provider-per-shape-urls/` 使用 `--new-from-rev=origin/main`）一致
- [ ] 3.2 本机端到端：`go run . ai switch pi api-itn` 后核对 `~/.pi/agent/models.json` 的 `providers.senv-api-itn.compat`，并在 pi 里用 `kimi-for-coding` 发一条消息确认不再出现 `role 'developer' is not allowed`（同时确认 `k3`/`k3-256k` 亦不报错）。验证：命令输出与 pi 会话记录留档。此任务需人工解锁 vault + 重启 pi，agent 不执行
