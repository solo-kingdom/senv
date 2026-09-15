## Why

`eval "$(senv env export)"` 是 shell 注入的主路径；单个 env 值含无法解析的 `{{text:llm-keys:TokenApi}}` 时整命令非 0 退出，其余变量也无法注入。常见触发是 `senv ai provider rename` 已迁移 `llm-keys/<old>` 但未级联改写 default 组里 codex 写入的 SeedRef 条目。MCP export 已在跨机同步场景改为 loose，env export 仍严格，行为不一致。

## What Changes

- `senv env export` 与 MCP `senv_env_export`：引用目标缺失时保留 `{{...}}` 模板、stderr 打 warning、命令 exit 0；循环引用与超深度仍 hard fail。
- 逐条 env 变量解引用，warning 标注 env key 名，其余条目正常导出。
- `RenameProvider` 级联：扫描 env 全部分组，将值等于 `{{text:llm-keys/<old>}}` 的条目改写为 `{{text:llm-keys/<new>}}`；输出已更新条数。
- 更新 agent skill 与相关测试。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `ref-system`：`env export` 自动解引用在目标缺失时的语义（loose + warning，非整命令失败）。
- `llm-provider`：`RenameProvider` 增加 env SeedRef 级联改写要求。

## Impact

- `cmd/env.go`、`cmd/mcp.go`（export 路径）
- `internal/ref`（复用既有 loose 语义，可能加 per-key warning 辅助）
- `internal/llm/provider.go`（rename 级联）
- `openspec/specs/ref-system`、`openspec/specs/llm-provider`
- `.agents/skills/senv-cli/SKILL.md`

## 非目标

- text/env 通用 `RenameKey` 的全 vault 引用重写（另开 change）。
- MCP server profile env 模板中的引用级联。
- 新增 `doctor` 扫描 dangling ref（可选后续）。
- export 对坏引用「跳过该行」而非保留模板（与既有 `--loose` 语义一致，保留字面量）。

## 安全性分析

- export loose 仅影响缺失目标的条目：stdout 可能含未解析模板字面量，不会泄露其它条目明文；与 MCP export 已有暴露面一致。
- rename 级联只改引用模板字符串，不读写信凭据明文；仍在 vault mutation 锁内原子完成。
- 循环引用/超深度仍 fail-closed，避免 eval 注入畸形或无限展开。
