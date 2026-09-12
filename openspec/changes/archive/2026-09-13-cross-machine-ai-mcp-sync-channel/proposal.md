## Why

LLM Provider 档案（`dataPath/llm_providers/<alias>.enc`）与 MCP Server 档案（`dataPath/mcp_servers/<alias>.enc`）是人工添加进 vault 的"配置源"，与 env/text 同形态（SSH-style 加密 blob），但不在同步通道内，多机之间必须各自重配。本切片把这两个 kind 接入现有 sync 通道：schema、双向扫描、冲突诊断一次打通（driver D2/D3/D5/D6）。

## What Changes

- `internal/syncschema`：新增 `KindLLMProvider = "llm_provider"` 与 `KindMCPServer = "mcp_server"`；`ValidateIdentity` 各加一个分支（grp 必须为空、key 为安全单路径段=alias）
- `internal/provider/server_state.go`：`entryLocation` 加两个写盘 case（pull 方向）；`collectEntriesDiff` 加 `llm_providers/`、`mcp_servers/` 两个目录遍历段（push 方向）；新 kind 的 blob 走既有 `AtomicWrite` 落盘与 revision 状态跟踪，无新存储路径
- 冲突：沿用现有 revision 乐观锁 + 冲突解决器，不新增裁决逻辑；冲突条目 kind 为 `llm_provider`/`mcp_server` 时，stderr 冲突报告与 TUI audit 的同步失败消息额外给出"本地 vs 远端 alias/revision"对照 warning，提示人工核对双端修改
- 首次 bootstrap 不加 opt-in：新机器首次 pull 即拉到全部新 kind 档案；`--accept-remote` 行为不变
- server 端零改动：`internal/server/store` 复用 `syncschema.ValidateIdentity`（store.go:342），schema 更新后自动接受新 kind

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `server-sync`: kind 白名单从五种扩到七种（新增 `llm_provider`/`mcp_server` 及其身份 schema）；冲突报告要求新增"配置源 kind 冲突时输出 alias/revision 对照 warning"；bootstrap 语义显式覆盖新 kind（默认随 pull 拉取）

## Impact

- `internal/syncschema/schema.go`、`internal/provider/server_state.go`、`internal/provider/server.go`（冲突报告/kind 渲染）、`cmd/sync.go`（writeSyncConflictReport）；server 端与 git remote provider 主流程不动
- 新增测试：schema 校验、双向扫描、pull 落盘、冲突报告渲染；e2e 覆盖新机器 bootstrap

## Non-goals

- 不动凭据引用语义与模板解析（`llm-provider` / `mcp-server-export` 等 capability 由兄弟切片处理）
- 不动 agent 切换指针、MCP export ledger 的存储与不同步状态
- 不把 `ssh_host` / `ssh_keypair` 接入通道（driver D9：已识别但延后）
- 不改 auto-sync 触发策略与同步协议 wire format（仅 kind 枚举扩容）

## 安全性分析

- 新 kind 同步的是既有 SSH-style 加密 blob（AES-256-GCM，服务端只见密文），密文路径、权限（0600/0700）、零知识不变式与 env/text 完全一致；server 端仅多接受两种身份字符串，且复用同一 `ValidateIdentity` 路径段校验，无新增攻击面
- 凭据本体不出机：档案只含 `credential_ref` 与 `{{env:/text:}}` 模板引用（ADR-0008），解密仍只发生在本机
