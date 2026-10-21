# Design：元数据模糊搜索

## Context

TUI 已有 `internal/tui/search.go` 的跨类型 inventory + `matchKey`（substring），但只服务 overlay；CLI/MCP 无对等入口。`senv_env_list` 经 `ListVarInfo` 返回 value。各 Manager 已能列出元数据（text/backup size、host list、provider list、mcp list）。见 proposal.md Why。

## Goals / Non-Goals

**Goals:**

- CLI + MCP 共享同一套 inventory / match / rank / limit。
- 匹配字段与安全边界满足 `metadata-search` spec；TUI overlay 对齐 description 等字段。
- 不引入新存储格式；不改现有 list 工具契约。

**Non-Goals:**

- 不引入外部模糊搜索库；不做倒排索引持久化。
- 不把 KeyPair 私钥纳入检索面（本 change 不搜 keypair 条目；与现 TUI 一致）。

## Decisions

### D1：共享包 `internal/search`

抽出 `Item`（kind/group/key/matchText/preview/description/...）、`Gather(deps)`、`Query(items, q, limit)`。  
CLI/MCP 调用 Gather+Query；TUI 逐步改用同一 match/字段表（可先双写测试对齐，再删 TUI 内联 `matchKey`）。

备选：仅在 `cmd` 内实现 → 拒绝（TUI 易再漂移）。

### D2：匹配算法

- trim 后空 query → CLI/MCP 错误；TUI 空输入仍展示全 inventory（人机）。
- tokens = 按空白分割；每 token 对 `matchText` 做大小写不敏感 `Contains`（AND）。
- 排序：exact key/name → prefix → substring；同档 kind → group → key。
- 默认 limit=20；`--limit 0` 视为非法或按「默认」处理（实现选非法更清晰）。

备选：编辑距离 → 推迟；substring 已覆盖主场景。

### D3：inventory 如何避带 value

| kind | 来源策略 |
|------|----------|
| env | 解密条目取 key/description；构造 Item 时丢弃 Value（或新增 ListMeta API） |
| text/backup | 既有 List 已含 size/description |
| config | List 元数据 |
| ssh | ListHosts 元数据 |
| llm | ListProviders（含 description，无凭据） |
| mcp | List（含 alias/command/description，无 env 值） |

某 kind Manager 为 nil 或单组失败 → 跳过该子集，不整次失败（与 TUI gather 一致）。

### D4：CLI / MCP 形状

```text
senv search <query> [--kind env,text] [--limit 20] [--json] [--refresh]
```

MCP：`senv_search`，args `{query, kind?, limit?}`，JSON 同 CLI `--json`：

```json
{"query":"...","total":N,"truncated":bool,"hits":[{"kind":"env","group":"svc","key":"K","description":"..."}]}
```

### D5：审计

只读：沿用 MCP `op_mcp_tool`（target=工具名）；CLI search 可不新增写审计，或记一条不含结果的 `op_search`（实现时二选一，推荐 MCP 已够、CLI 只读不记，与 `doctor`/`list` 一致）。

## 数据流

```text
query + kind + limit
    → authorize/session
    → Gather(managers) → []Item (元数据 only)
    → filter by kind
    → Query(match+rank+limit)
    → render (CLI text | JSON | MCP textResult)
```

## 错误处理

| 条件 | 行为 |
|------|------|
| 无 session / 鉴权失败 | 错误，无 hits |
| query 空/空白 | 参数错误 |
| 未知 `--kind` | 参数错误 |
| limit &lt; 1 | 参数错误 |
| 无匹配 | 成功 + 空 hits |
| 单 kind 装载失败 | 跳过该 kind，其余继续；可选 stderr warning |

## 使用示例

```bash
senv search litellm
senv search "payment gateway" --kind env --json
senv search web --kind ssh --limit 5
```

## Risks / Trade-offs

- [全量解密 inventory] → 与 TUI `S` 同级成本；大 vault 可接受；perf log 已可观测。
- [description 进匹配扩大命中面] → 仍无值；说明本身约定不当密钥用。
- [TUI 空 query 与 CLI 不同] → 文档写明：人机可浏览，agent 必须带 query。

## Migration Plan

纯增量：发版后新旧客户端并存；无数据迁移。回滚即移除命令/工具，无存储副作用。

## Open Questions

（无；范围与空 query 策略已由用户确认。）
