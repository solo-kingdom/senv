## Context

- `senv ai switch`：`internal/llm/switch.go` `resolveCredential`（:819-842）在 Switch 任何写盘之前执行（:899-904），失败零写入——fail-closed 已成立。现状错误文本 `decrypt credential text:llm-keys/<alias>: <底层错误>`，不区分"条目不存在"与其他解密失败
- `senv mcp export`：`cmd/mcp_export.go:64-86` 注入 `resolveValueWith(value, false, ...)`（严格模式）；`internal/mcp/export.go` `resolveEntry`（:340-373）解析失败包装错误，`Plan`（:103-168）把失败记为 `ActionError` 并令整个 target 终止（:146-149 注释即此语义）
- 宽松模式基础设施已存在：`internal/ref/resolver.go` `ResolveWithWarnings` + `PrintWarnings`（:226-248，`Loose` 模式保留原文并返回 warning）；但 `PrintWarnings` 当前打 stdout
- driver 决策 D7 的前提修正：grill 记录称"沿用既有 export 行为（向后兼容）"，实际既有行为是严格报错终止（`mcp-server-export` spec `明文落盘提示` requirement 白纸黑字）。决策结论（宽松写入 + warning）在三处记录一致且明确，按结论实施，spec delta 如实写成 Modified

## Goals / Non-Goals

- Goals：ai switch 诊断指明缺失引用全名 + 修复指引；mcp export 引用缺失时宽松写入 + stderr warning；计划展示标注未解析引用条目
- Non-Goals：漂移/`--force`/台账语义、凭据分发、ai switch 的 fail-closed 结构

## Decisions

- D-a ai switch 诊断：`resolveCredential` 区分两类失败——引用条目不存在（`pm.textManager().Get` 返回 not-found 类错误）时报 `凭据引用 text:llm-keys/<alias> 在本机 vault 中不存在：请先 senv text add llm-keys <alias>（或从已有机器同步）后重试`；其余解密失败保留既有 `decrypt credential ...` 包装。判定方式：用 `errors.Is(err, storage.ErrNotFound)`（以实际 storage 层错误哨兵为准，实现时核对），不靠字符串匹配
- D-b export 宽松化落点：`cmd/mcp_export.go` 把注入的 resolve 回调改为 `loose=true` 语义（`resolveValueWith(value, true, ...)`）；`export.go` `resolveEntry` 收到 loose 回调返回的原文+warning 后不再报错，把 warnings 挂到 `ExportItem`（新增 `Warnings []string` 字段）；`Plan` 对这类条目不再置 `ActionError`，目标正常写入；`executeTarget` 不变
- D-c warning 输出通道：新增统一打印函数，写 `cmd.ErrOrStderr()`（对齐 ai switch 的 warnings 通道）；`printLedgerWarnings` 的 stdout 行为不动（不在本切片范围）。`--dry-run` / `--print` 同样输出 warnings，让计划可预览未解析引用
- D-d 台账指纹：宽松写入的条目按实际写入内容（含模板原文）计算指纹，天然与后续重跑一致；补齐凭据后重跑会产生新指纹 → 漂移判定按既有 `--force` 语义处理，不特判

## Risks / Trade-offs

- 宽松模式让 agent 在凭据未同步时拿到含 `{{...}}` 字面量的配置，agent 侧可能把字面量当真实值使用（例如 header 值原样发出）——这是 D7 明确接受的取舍（档案先落地），warning 与计划标注让用户可见
- `Plan` 不再因解析失败终止 target 后，`部分失败与台账更新` requirement 的既有语义（其他条目失败不阻断）不受影响

## Migration Plan

无数据迁移。既有可解析引用行为不变；仅引用缺失场景从"报错终止"变"宽松写入 + warning"。

## Open Questions

无
