# 设计：MCP Server 档案与导出

## 模块划分

- `internal/storage`：新增加密集合 `mcp_servers/`（每档案一文件），存 `storage.MCPServerEntry`（别名、传输类型、`command`/`args`/`env`、描述、时间戳）。与 `hosts/`、`llm_providers/` 同构；取值必须登记进 `rekey.go` 的路径判定、`consistency.go` 的完整性校验、`ssh.go` 的 path→type 映射与 `repair.go`，否则 rekey / 一致性检查会漏掉这类文件（D2/D3）。
- `internal/mcp`（新包）：档案 manager（CRUD）+ 导出器（计划、执行、台账、撤回）；`env` 值解析复用既有 ref-system 解引用。
- `cmd/mcp*.go`：把 `cmd/mcp_agents.go` 的 agent 注册表与 `cmd/mcp_install.go` 的 JSON/TOML merge 原语**下沉**到可复用位置，供 `senv mcp install` 与新 `senv mcp export` 共用，避免两套合并逻辑漂移（D4/D7）。
- `cmd/mcp_mcp.go`：只读 MCP 工具 `mcp_server_list` 与视图白名单（D14）。

## 数据模型

```go
type MCPServerEntry struct {
    Alias       string            // 唯一标识
    Transport   string            // V1 仅 "stdio"
    Command     string
    Args        []string
    Env         map[string]string // 值为模板原文，导出时才解析
    Description string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

本机台账 `~/.config/senv/mcp-exports.json`（明文、0600，不进 vault，D8/D10）：

```json
{
  "version": 1,
  "entries": {
    "codex": { "github": { "fingerprint": "sha256:…", "exported_at": "…" } }
  }
}
```

指纹 = 写入该 agent 配置时落盘条目的规范化 JSON/TOML 序列化摘要。判定规则：目标条目实际内容指纹 == 台账指纹 → senv 所写，可覆盖；内容指纹 != 台账指纹 → 漂移，默认拒绝；台账无该条目 → 外部条目，默认拒绝（`--force` 覆盖）。

## 数据流

```
senv mcp add/edit ──▶ vault(mcp_servers/<alias>.enc)
                                  │
senv mcp export --all ────────────┤
        │                         │
        ▼                         ▼
  1) 读取档案 ──▶ 2) 解引用 env 模板（严格模式，失败→该 agent 报错终止）
        │
        ▼
  3) 逐 agent 变更既有配置：解析现文件（JSON/TOML）
        │  ├─ target 条目不存在 ────────────▶ create
        │  ├─ 指纹==台账 且内容相同 ────────▶ skip
        │  ├─ 指纹==台账 且内容不同 ────────▶ update（备份 .bak）
        │  └─ 指纹!=台账 或无台账 ──────────▶ drift（--force 才有 update）
        ▼
  4) 输出计划（标注明文条目/目标路径）──▶ 用户确认 ──▶ 写盘 + 更新台账

senv mcp unexport ──▶ 期望内容 vs 实际：一致→删除；不一致→要求确认
```

## 关键取舍

- 复用 `mcp install` 的 agent 注册表（7 个）与 JSON/TOML 合并原语，两处写入路径单一实现（D4）。
- `command` 原样写入，只有 senv 自身二进制在 `install` 路径继续绝对化（D17）。
- 只落 `command`/`args`/`env` 公共子集，不透传 agent 特有键（D18）。
- 逐 agent 独立成败，不做跨文件事务回滚；台账只记成功项（D19）。
- 导出计划即确认点，明文条目必须显式列出（D20）。

## 存储与向后兼容

新增 `mcp_servers/` 目录是纯增量：旧版本 senv 不认识该目录，等价于「没有档案」，不需要迁移；新版本读旧 vault 也自然得到空集合。反向约束是**新目录必须登记进所有按路径枚举的机制**（rekey、consistency、repair、path→type 映射），否则会出现「rekey 后档案读不出来」「一致性检查漏报」这类只在特定操作下暴露的问题。这一点是本次改动最容易被漏掉的回归面，测试必须覆盖：rekey 后档案可读、一致性检查纳入该目录。

实现期发现的具体登记点（比计划多一处）：`rekey.go` 的 `classifyRekeyEntry`、**`rekey_manifest.go` 的 kind 映射**（漏了会 panic）、`consistency.go` 的探测与 `HasOrphanedData`、`ssh.go` 的 `entryKindForDir`、`cmd/doctor.go` 的输出。`repair.go` 只处理 config index 的隔离条目，没有按集合枚举的逻辑，无需登记。

**已知边界（沿用现状，不属本次范围）**：server provider 的同步清单只覆盖 env/text/config（`internal/provider/server_state.go`），ssh 资产与 LLM Provider 同样不在其中；MCP Server 档案沿用这一现状——git provider 下随数据目录全量同步，server provider 下待后续扩展到这些集合时一并处理。

## 错误处理策略

| 场景 | 行为 |
|------|------|
| 别名冲突（add） | 报错，不改既有档案 |
| 非 stdio 传输 / 缺 command | 参数校验失败，不落库 |
| 引用解析失败（导出） | 该 agent 报错终止，其目标文件不改；其余 agent 继续 |
| 目标文件解析失败 | 该 agent 报错，文件原样保留（绝不写出半解析结果） |
| 漂移 / 外部同名条目 | 计划中标注，默认跳过；`--force` 才覆盖 |
| 目标文件不可写 | 该 agent 报错，其余继续；非零退出码汇总 |
| 台账损坏 | 视为空台账（全部按外部条目处理，需 `--force`），并提示 |

## CLI 示例

```bash
senv mcp add github --command npx --args "-y,@modelcontextprotocol/server-github" \
  --env GITHUB_TOKEN={{env:secrets:GH_TOKEN}} --description "GitHub 官方 server"
senv mcp list
senv mcp export --all --dry-run          # 只看计划与明文落盘点
senv mcp export --agent codex,cursor     # 写前确认，写后更新台账
senv mcp export --all --force            # 覆盖漂移条目
senv mcp unexport --agent codex          # 撤回
```

## 测试策略

- storage：新目录纳入 rekey / consistency / repair 的回归测试（先写失败用例，再补登记）。
- 导出：JSON 与 TOML 两族的合并保真（其它键、其它 server、既有 `.bak` 不丢）、幂等（重复导出为 `skip`）、漂移三态（senv 写 / 漂移 / 外部同名）、`--force` 分支。
- 安全：`--print`/`--dry-run` 不落盘；目标文件 0600；明文只出现在计划标注与目标文件中，不进入台账；`mcp_server_list` 响应不含值。
- CLI：未给目标、未知 agent、引用解析失败的退出码与错误信息。
