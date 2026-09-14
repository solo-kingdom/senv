## Why

能力审计（CLI↔TUI 矩阵）发现两处反向缺口：TUI 有、CLI 无。KeyPair Tab `e` 可就地改 keypair 分组，CLI `keypair` 子命令（add/import/list/materialize/rename/prune/delete）无 edit，改组只能删了重导；Text Tab `i`/`x` 有文件导入导出，CLI `text` 只有 set/get/delete/list/group，agent 与脚本用户没有显式 import/export 动词可用。

## What Changes

- 新增 `senv keypair edit <name> --group <group>`：`--group` 显式变更时单字段更新、不启编辑器（对齐 `host edit --group` 免编辑器范式与 TUI `e` 语义）；空值 = 清除分组
- 新增 `senv text import <key|group:key> --file <path>`：文件内容加密入库，key 已存在时覆盖（TUI `i` 的 upsert 语义），源文件不动，`--file` 必填
- 新增 `senv text export <key|group:key> --path <path>`：明文值经安全原子写落盘，固定 0600，不经引用解析（TUI `x` 单条导出同路径），`--path` 必填

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `ssh-assets`: 「KeyPair 分组字段」requirement 增加 CLI 编辑入口（`keypair edit --group`）及配套 scenario
- `text-storage`: 「Text CRUD with group support」增加 `import` 子命令及配套 scenario；「Text 明文文件导出安全」增加 `export` 子命令及配套 scenario

## Impact

- `cmd/ssh.go`（keypairEditCmd）、`cmd/text.go`（textImportCmd/textExportCmd）；复用 `internal/ssh.Manager.UpdateKeyPair` 与 `internal/text` 既有 `SetFromFile`/`GetToFile`，无 manager 新能力、无存储格式变更
- `.agents/skills/senv-cli/SKILL.md` 同步新增命令说明（AGENTS.md 要求用户可见命令同变更更新）
- TUI 无需改动（已有 `e`/`i`/`x`）；MCP 不新增工具、保持只读

## Non-goals

- `keypair edit` 的编辑器流程：group 是唯一可就地编辑的 keypair 元数据，key 材料不可改（与 TUI 一致），edit 仅 `--group` 单字段
- `text export` 的 `--mode` 选项：固定 0600；显式放宽共享沿用 `text get -o --mode`
- `text import` 的防覆盖确认：upsert 即 TUI `i` 语义，靠审计与 `updated_at` 可追溯
- TUI 批量导出（多选 `x` 写目录）的 CLI 对等命令；`text` 顶层 shorthand 行为变更

## 安全性分析

- 导出明文写文件复用 `exportfile.WriteFile`：`~` 展开、父目录按需创建、原子写、固定 0600、覆盖既有文件收紧权限、拒绝符号链接——与 `text get -o`/TUI `x` 同一攻击面，零新增
- `keypair edit --group` 只触元数据：私钥/公钥材料不变；group 经 `validateGroup` 校验拒绝 `/`，防路径逃逸出 `keys/<组>/`
- 导入加密入库后源文件不动；改组与导入写审计（operation-audit 同类事件），导出属读取面、与现状一致不新增审计事件
- 三个命令输出均不回显敏感内容（export 只打印路径）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 单仓变更 |

## 验收标准

- [ ] `senv keypair edit web-key --group prod` 改组生效且 list 可见；`--group a/b` 报错且原值不变；`--group ""` 清除分组；缺 `--group` 报错零副作用
- [ ] `senv text import g:k --file f` 新键创建、重复键覆盖（updated_at 刷新）、文件缺失报错零副作用
- [ ] `senv text export g:k --path p` 写出 0600 明文且与 vault 解密值逐字节一致；符号链接目标被拒绝；不打印明文
- [ ] `go test ./cmd/ -race` 全绿；SKILL.md 已更新；`go run . keypair edit --help`、`go run . text import --help`、`go run . text export --help` 正常
