## 1. MCP server 主体（高优先级）

- [x] 1.1 引入 `github.com/modelcontextprotocol/go-sdk` v1.3.1（兼容 go 1.24），`go mod tidy` 通过。验证：`go build ./...`。
- [x] 1.2 `cmd/mcp.go`：`senv mcp serve` 子命令；启动期 `resolveAuth`，无 session 报错退出；构建 server 并注册 16 个工具。验证：`go vet ./cmd` + 单测。
- [x] 1.3 抽出 `resolveValueWith`（`cmd/text.go`），让 handler 解引用用注入的 manager，避免二次鉴权。验证：`go test ./cmd -run MCPEnv -race`。

## 2. 工具 handler（高优先级）

- [x] 2.1 env: get（含 decode）/set/delete/list/export。验证：`TestMCPEnvSetGetDelete`、`TestMCPEnvListAndExport`。
- [x] 2.2 text: get（含 decode）/set/delete/list（跨组列出）。验证：`TestMCPTextSetGetListDelete`。
- [x] 2.3 config: list/get/export（导到临时文件再读回，避免写用户路径）。验证：handler 构建 + 集成。
- [x] 2.4 group: list（env|text）/add（kind 校验）/activate/deactivate。验证：`TestMCPGroupAddListActivate`。

## 3. 安装命令（高优先级）

- [x] 3.1 `cmd/mcp_agents.go`：注册表（7 个 agent）+ 配置路径解析（含平台差异）。验证：`TestInstallUnknownAgent`。
- [x] 3.2 `cmd/mcp_install.go`：JSON upsert（保留其它 server + 备份）、TOML upsert、`--scope/--print/--all`。验证：`TestInstallJSON_*`、`TestInstallTOML_*`、`TestInstallPrintDoesNotWrite`、`TestInstallAll`。
- [x] 3.3 `senv mcp list-tools`：打印工具清单，与注册表一致。验证：`TestMCPServerBuildsAndRegistersAllTools`。

## 4. 文档

- [x] 4.1 README：命令列表新增 `mcp serve/install/list-tools`；新增《MCP 集成》小节（session + install + 工具清单 + 安全提示）。验证：文档示例与实现一致。
- [x] 4.2 openspec 提案 `openspec/changes/mcp-interface/`（proposal/design/tasks）。验证：三文件齐全且符合既有规范。

## 5. 验收

- [ ] 5.1 `make check`（fmt + vet + lint + test）全绿。验证：CI 本地运行。
- [ ] 5.2 真实 agent 冒烟：`senv session start` → `senv mcp install cursor` → 重启 Cursor 调用 `senv_env_get`（手动）。
