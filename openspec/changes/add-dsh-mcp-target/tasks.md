## 1. agentcfg：cordis patch 原语与 dsh 目标

- [x] 1.1 `internal/agentcfg/agents.go` 新增 `FormatCordisPatch`；`Supported()` 加入 `dsh` 目标（别名 `deepseek-harness`、`$DSH_HOME/cordis.patch.yml`、user/project 同路径、remote 仅 stdio + http、sse plan-error、无前置依赖、Note 说明 HMR）
- [x] 1.2 新增 `internal/agentcfg/cordis_patch.go`：`DshHomeDir`/`DshServerName`/`DshEntryID`、标记块定位与解析、`insert` 行渲染、`serverName` 归一、transport 映射、空层 `[]`、顶层结构与重复 id/serverName 校验
- [x] 1.3 `internal/agentcfg/config_test.go` 远程能力矩阵加入 dsh；新增 `cordis_patch_test.go` 覆盖路径/别名/归一、stdio+http 往返与幂等、块外字节保留、`[]` 处理、删空、非法块/重复 serverName fail-closed、`--print` 片段

## 2. mcp 导出/撤回与安装接线

- [x] 2.1 `internal/mcp/export.go` 的 `agentFile` 增加 cordis 分支（entry/set/remove/encode），export/unexport/drift 复用既有语义
- [x] 2.2 `cmd/mcp_install.go` 增加 `installCordisPatch` 与 `--print`；`cmd/mcp_export.go --print` 输出受管块片段；`cmd/mcp_agents.go` 增加格式别名
- [x] 2.3 测试：`internal/mcp/export_test.go`（create/skip/update/drift/--force/sse plan-error/unexport）、`cmd/mcp_install_test.go`、`cmd/mcp_export_test.go`、`internal/tui/mcp_tab_test.go`；所有 `--all`/dsh 测试钉住 `DSH_HOME`
- [x] 2.4 `DSH_HOME` 隔离修复：`newMCPExportProject`、`installWithHome`、`TestInstallAll`、TUI `newMCPTestTab` 显式钉住测试内 home，避免开发机真实 `~/.dsh` 被写

## 3. 文档与验证

- [x] 3.1 `docs/adr/0034-dsh-mcp-target-cordis-patch.md`
- [x] 3.2 `.agents/skills/senv-cli/SKILL.md` 与 `README.md` 的 agent 枚举/导出目标/remote 矩阵同步 dsh（含 home patch 语义、insert 行、HMR、无 sse）
- [x] 3.3 `go vet ./...`；`go test ./internal/agentcfg ./internal/mcp ./internal/tui -count=1` 全绿（本机 macOS 需 `TMPDIR=/private/tmp` 规避 securefs 对 `/var` 符号链接的既有拒绝）
- [x] 3.6 独立验证（dsh-verifier）后修复：P2-1 只替换解析到的顶层 `[]`（多行标量/嵌套序列里的 `[]` 保留）；P3-1 有受管块时块外 senv id 也在读取时 fail closed；P3-2 文档改为「共用一条 insert 行」；P3-3 首字符换行的值强制双引号标量（`!!str`）；P3-4 覆盖已有宽松权限文件收紧 0600；P3-5 同 id 不同别名 fail closed；P3-6 删除死字段、坏 transport 判 drift；补对应回归测试
- [ ] 3.4 `go test ./cmd -count=1`：session 相关用例在本机 macOS 受既有 secure session store 限制（非本改动引入），留待 Linux CI 验证；已编译通过并跑通 install/非 session 用例
- [x] 3.5 commit（跟随仓库 log 风格，说明写原因）；OpenSpec archive 走单独流程
