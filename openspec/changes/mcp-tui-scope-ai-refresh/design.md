# Design: mcp-tui-scope-ai-refresh

## Context

两处能力审计发现（B5/A4）的代码事实侦察结论：

**侦察 A —— `--scope project` 的完整语义（MCP 导出）**

- CLI 侧：`mcp export` 与 `mcp unexport` 均有 `--scope` flag（默认 `user`，`cmd/mcp_export.go:373,381`），经 `mcpExporter(scope)` 传入 `ExporterOptions.Scope`，`NewExporter` 用 `agentcfg.ResolveScope` 校验（仅接受 `user`/`project`，`internal/agentcfg/agents.go:294-301`），`Plan` / `PlanUnexport` 内以 `target.ResolveConfigPath(home, scope)` 解析每个 agent 的目标文件（`internal/mcp/export.go:144,541`）。
- **project 仅部分 agent 生效**：当前只有 cursor 区分 scope——`scope == "project"` 时目标为 CWD 相对的 `.cursor/mcp.json`，其余 agent（claude-code、codex 等）的 `ConfigPath` 忽略 scope 形参，两 scope 同路径（`internal/agentcfg/agents.go:149-154`）。CLI flag help 原文即 "project only honored by some agents"。
- **台账不区分 scope**：`mcp-exports.json` 以 `entries[agent][alias]` 两层 map 存储（`internal/mcp/ledger.go:26-29`），一条记录只含 fingerprint + exported_at。同一 alias 导出到同 agent 的 user 与 project 两个文件会共享/覆盖同一条台账记录；`unexport` 撤回任一 scope 时 `ledger.Delete` 会删掉这条共享记录，另一 scope 的文件副本即成为台账孤儿（之后 `PlanUnexport` 不再列出它，再导出会被当作外部条目判 drift）。这是 CLI 现状，TUI 加 scope 只是暴露同一语义。
- **撤回同样吃 scope**：CLI `mcp unexport --scope` 走同一个 `Exporter`（同一个 `opts.Scope`），TUI 的 `u/U` 亦然——因此 TUI 改造只要让 `exporter()` 不再硬编码，导出/撤回/状态列自动一致。
- TUI 侧改造点：`mcpTab.exporter()` 硬编码 `Scope: "user"`（`internal/tui/mcp_tab.go:266`），全部五个消费方都经它：状态列 `statusFor`（:226）、删除提示 `exportedAgents`（:841）、`startExport`（:876）、`startUnexport`（:904）、`replanForce`（:986）。右栏标题在 `viewBaseAt`（:1255，现文案 "Agents · export status"），计划弹窗每行已含 `Path` 列（:1320,:1328）。**`s` 键在 MCP Tab 空闲**：`updateKey`（:376-462）无 `s` case、Bindings（:127-135）未列出；全局层只绑定大写 `S`（跨类型搜索，`internal/tui/model.go:410`），小写 `s` 会落到 Tab。

**侦察 B —— `ai refresh` 实现位置与 AI Tab 键位**

- CLI `senv ai refresh`（`cmd/ai.go:30-58`）本体极薄：`llm.Fetch(source, nil)` → `Counts()` 校验 → `llm.Save(catalogCachePath(), cat)`；缓存路径 `<config>/cache/models-dev.json`（`cmd/ai.go:14-16`）。全部实现在 `internal/llm/catalog.go`（`Fetch`/`Parse`/`Save`/`Load`、`DefaultCatalogURL`、32MiB 上限、temp+rename 原子替换、0600）。**TUI 可直接调 `internal/llm`，无 cmd 层依赖**；且 TUI 已持有同一路径：`Managers.LLMCatalog`（`internal/tui/model.go:39-41`，由 `cmd/tui.go:104` 以同一个 `catalogCachePath()` 注入），provider 表单装配已在用（`ai_tab.go:892` → `internal/llm/provider.go:521,561` 的 `Load` + `LoadModelMetadata`）。
- AI Tab 键位占用（`updateKey`，`ai_tab.go:391-448`）：`k/j/h/l`、`/`（过滤）、`ctrl+r`（本地重载，:413-415）、`enter`、`n`、`e`、`d`、`s`（切换）、`M`（仅换默认模型）、`g/G`、`pgup/pgdown`。**`R` 空闲**：全文件无 `case "r"`/`case "R"`，Bindings（:147-155）未列出，全局层也未绑 `R`。旁证：`ai_tab.go:1325` 空态提示 "then r to refresh" 是残留错误文案（实际本地重载是 `ctrl+r`），本 change 顺带订正。

## Goals / Non-Goals

**Goals:**

- MCP Tab 提供与 CLI 等价的 scope 选择（user/project，默认 user），导出、撤回、状态列、drift 判定全部按当前 scope
- AI Tab 提供与 `senv ai refresh` 同语义的联网刷新入口，失败时旧缓存原样保留
- 键位选择零冲突（侦察证据见 Context），help/keyactions 同步

**Non-Goals:**

- 台账结构改造、scope 持久化、`--source` 覆盖、CLI 行为变更（proposal 已列）
- 刷新 AI Tab 的模型候选展示（向导候选来自档案 `Models` 字段，刷新作用于下一次表单装配，属既有数据流，不在本 change 加展示）

## Decisions

### D1 MCP scope：Tab 级状态 + `s` 切换

`mcpTab` 增加 `scope string` 字段（零值即 `user`），`s` 键在 `user`/`project` 间切换；`exporter()` 改读 `t.scope`；切换后立即 `syncStatus()` 重算右栏。右栏标题改为 `Agents · export status (scope: <scope>)`，`viewBaseAt` 拼接。

- 备选：在计划确认弹窗里加 scope 切换——计划已在 `startExport` 时按 scope 生成，弹窗内切换需整体 replan，复杂且违背「弹窗只确认」的现有语义（mcpModePlan 其余按键一律忽略，grill D7）。
- 备选：导出时先弹 scope 表单——每次导出多一步；scope 是低频偏好而非每次导出的决策，Tab 级切换更贴近 CLI「一次调用一个 scope」的心智。
- 键位：`s` 空闲（侦察 A 证据），mnemonic = scope；大写 `S` 被全局搜索占用，用小写正好避开。

### D2 撤回继承同一 scope

`u/U` 与 `x/X` 共用 `exporter()`，scope 天然继承，与 CLI `mcp unexport --scope` 语义对齐，不单开键位。台账无 scope 维度的后果（双 scope 孤儿）原样继承 CLI 行为，见 Risks。

### D3 台账不动

不给 `mcp-exports.json` 加 scope 维度。理由：解析内容（`resolveEntry`）与 target 相关、与 scope 无关，同 alias 两 scope 写出的条目 fingerprint 相同，台账共享记录在「判定谁写的」层面仍然正确；不正确的只是「撤回后遗忘另一 scope 副本」。修它需要 ledger v2 + 迁移，超出本 change 边界，记入 Risks 作为已知限制。

### D4 AI 刷新键位：`R`

`R` 空闲（侦察 B 证据）。语义分层：`ctrl+r` = 本地档案重载（轻、离线、全 Tab 通用，keymap.go:71 归 grpFilter），`R` = 联网刷新公开目录（重、写本地缓存，归 grpItem）。大写呼应既有「大写 = 变体/加重」先例（`M`之于 `s`、`X`之于 `x`）。`ai_tab.go:1325` 空态提示改为指向 `R`（顺带修掉残留错误文案）。

- 备选 `r`：同样空闲，但小写留给未来行内动作更灵活，且 `r` 与 `ctrl+r` 同名近形，help 里两行列「refresh/refresh」比 `R` 更易误读。

### D5 AI 刷新实现：异步 Cmd 直调 internal/llm

`R` 触发 `tea.Cmd`：`llm.Fetch(llm.DefaultCatalogURL, nil)` → `cat.Counts()` 校验 → `llm.Save(t.mgr.LLMCatalog, cat)`，结果包成 `aiCatalogRefreshedMsg{providers, models, err}` 回主循环：成功 `okToast("catalog refreshed: N providers, M models")` + `t.load()`；失败红色 toast。`t.mgr.LLMCatalog == ""` 时按 model.go 注释的防御语义处理：`R` 给 warn toast 指路 CLI `senv ai refresh`。不加确认框——公开数据、幂等、CLI 同样不确认。

- 数据流图：

```
按 R ──► tea.Cmd: llm.Fetch(DefaultCatalogURL, nil)
              │ 非 200 / 网络错 / 超 32MiB / Counts() 校验失败
              ▼
   aiCatalogRefreshedMsg{err} ──► 红色 toast（旧缓存不动）
              │ 成功
              ▼
   llm.Save(LLMCatalog, cat)   ← temp+rename 原子替换，0600
              ▼
   aiCatalogRefreshedMsg{N,M} ──► okToast + t.load() 重载
              │
              ▼（下一次 provider 表单提交时）
   llm.Load(LLMCatalog) + LoadModelMetadata ──► 模型候选/元数据来自新缓存
```

- MCP 数据流图：

```
按 s ──► t.scope 切换 user↔project ──► syncStatus()
              │                          │
              │ exporter(Force) 构造     ▼
              │  mcp.ExporterOptions{Scope: t.scope, ...}
              ▼                          ▼
   agentcfg.ResolveScope（校验 user|project）
              │
              ▼ Plan / PlanUnexport: target.ResolveConfigPath(home, scope)
     ┌────────┴─────────────┐
     ▼                      ▼
 cursor+project          其余 agent / user scope
 → .cursor/mcp.json      → 全局配置路径（不变）
     │
     ▼ 计划弹窗（Path 列逐条展示实际目标文件）→ y 确认 → Execute/ExecuteUnexport
              │
              ▼
   写盘（0600 + .bak）+ ledger.Set/Delete（原子写，无 scope 维度）
```

### D6 错误处理策略

- **scope 切换**：纯内存状态变更 + 重算状态列，零 I/O 副作用；scope 来自内部枚举，`ResolveScope` 不可能失败。
- **MCP 计划/执行**：`exporter` 构造失败、`Plan`/`PlanUnexport` 失败沿用现有 `errMsg` toast；执行部分失败沿用现有 toast + 计数语义，本 change 不改动。
- **AI 刷新失败**：`Fetch` 网络错误 / 非 200 / 超 32MiB、`Counts()` 校验失败、`Save` 磁盘失败——全部红色 toast 展示错误消息；`llm.Fetch` 失败自然到不了 `Save`，`llm.Save` 内部失败自行清理临时文件，**旧缓存任何失败路径下都字节不变**。
- **LLMCatalog 为空**：warn toast 提示用 CLI 刷新，不 panic、不写盘。
- **刷新进行中**：异步 Cmd 不阻塞 UI；重复按 `R` 允许并发（与连按两次 CLI 等价），原子落盘保证缓存永不半写（见 Risks）。
- ** toast/banner 吞键**：model.go 的 hadErr/hadWarn 吞一键逻辑是全局既有行为，新键位无需特殊处理。

## Risks / Trade-offs

- **台账无 scope 维度 → 双 scope 孤儿**：同 alias 同时导出 user+project 后，撤回一个 scope 会删除共享台账记录，另一 scope 副本成为孤儿（再导出判 drift，需 `F`）。→ 缓解：与 CLI 现状完全一致的语义，不在本 change 改存储；文档与 help 不承诺双 scope 撤回闭环，未来如需 scope 感知的台账另立 change（ledger v2 迁移）。
- **project 仅 cursor 生效，用户可能误以为「切换无效」**：其余 agent 路径不变。→ 缓解：右栏标题常显当前 scope；计划弹窗 Path 列天然展示实际文件（`.cursor/mcp.json` vs `~/.cursor/mcp.json`）；help 沿用 CLI flag 的 "project only honored by some agents" 措辞。
- **`R` 与 `ctrl+r` 相邻误按**：→ help 面板分两行不同 group（grpItem vs grpFilter），描述文案区分 "refresh catalog (network)" / "reload (local)"；误按 `R` 也无害（幂等、失败保旧）。
- **刷新并发**：连按 `R` 产生并发 Fetch/Save。→ 原子 temp+rename 保证缓存文件永不半写，后完成者覆盖先完成者（与并发跑两个 CLI 相同）；不引入刷新中锁（复杂度不值）。
- **scope 不持久化**：重启回 user。→ 与 CLI 每次调用默认 user 一致；会话内切换成本一次按键，低风险。

## Migration Plan

无存储/格式变更（台账 v1、目录缓存 v1 均不动）。发布时随版本更新 SKILL.md 与 README TUI 按键说明；无回滚步骤（纯增量键位，旧行为默认不变）。

## Open Questions

无。
