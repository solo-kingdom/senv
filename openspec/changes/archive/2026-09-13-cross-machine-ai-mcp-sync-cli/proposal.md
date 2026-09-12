## Why

配置源档案跨机同步后，`credential_ref` 与 `{{env:/text:}}` 模板引用指向的条目可能尚未在本机（凭据本体不出机，driver D4/D7）。`senv ai switch` 需要继续 fail-closed 但给出指明缺失条目的诊断；`senv mcp export` 按决策改为宽松模式——引用缺失不再终止该 agent 的写入，而是保留模板字面量并逐条 warning，让档案先落地、凭据补齐后重跑即可。

## What Changes

- `senv ai switch`：凭据引用解析失败（text/env 条目缺失）时的错误诊断指明缺失的完整引用名（如 `text:llm-keys/<alias>`）与修复指引；fail-closed 行为不变（零写入）
- `senv mcp export`：档案字段中 `{{env:...}}` / `{{text:...}}` 引用缺失时不再报错终止目标写入，改为保留模板原文写入，并把缺失的 env/text 名逐条列入 warning（stderr）；漂移/`--force`/台账语义不变
- 既有可解析引用的导出结果不变；`--print` / `--dry-run` 的计划展示能看出哪些条目带未解析引用

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `mcp-server-export`: "明文落盘提示" requirement 的严格模式分支改为宽松模式——任一引用无法解析时不再终止该 agent 的写入，改为保留模板原文写入并输出缺失引用 warning

## Impact

- `internal/ref/resolver.go`（warning 收集已有 `ResolveWithWarnings`）、`internal/mcp/export.go`（`resolveEntry` / `Plan` 的 fail 路径改宽松）、`cmd/mcp_export.go`（warning 打印改走 stderr）、`internal/llm/switch.go`（`resolveCredential` 错误文案）
- `senv ai switch` 行为面只有错误文案变化；`senv mcp export` 行为面为引用缺失场景的实质变化

## Non-goals

- 不动 `senv ai switch` 的 fail-closed 语义与事务回滚（`llm-provider-switch` spec 不变）
- 不动漂移判定、`--force`、台账记录与撤回导出
- 不引入凭据代理/转发；不做跨机凭据分发
- 不动 `senv ai provider add` / `senv mcp add` 的录入语义

## 安全性分析

- 宽松模式会把**未解析的模板字面量**（如 `{{text:llm-keys/anthropic}}`）写入 agent 配置文件——不泄露任何明文凭据；可解析引用仍走既有明文落盘路径（ADR-0008 事实不变，`明文落盘提示` requirement 的标注行为保留）
- `senv ai switch` 维持先解密后写盘的 fail-closed 顺序，无新增敏感面
