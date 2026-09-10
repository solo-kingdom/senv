## 1. storage 层：档案条目

- [x] 1.1 新增 `MCPServerEntry` 类型与 `mcp_servers/` 加密集合（save/load/delete/list），复用既有条目读写原语
- [x] 1.2 测试：档案 CRUD 往返、别名冲突、非法传输类型拒绝、目录/文件权限 0700/0600
- [x] 1.3 【高优先】把 `mcp_servers` 登记进 `rekey.go` 路径判定、`consistency.go` 完整性校验、`repair.go` 与 `ssh.go` 的 path→type 映射
- [x] 1.4 测试：rekey 后档案仍可读、一致性检查覆盖新目录（先写失败用例再补登记）、repair 不误删档案

## 2. 档案管理 CLI

- [x] 2.1 `internal/mcp` manager：add（校验 command/alias/transport）、get、edit、list、delete；值按模板原样存储
- [x] 2.2 `cmd/mcp_server.go`：`senv mcp add/get/edit/list/delete` 子命令与输出格式（list 不输出 env 值）
- [x] 2.3 测试：add 校验分支、edit 不改别名、list 不泄漏值、delete 不触碰任何 agent 配置文件

## 3. 导出器

- [x] 3.1 把 `cmd/mcp_install.go` 的 JSON/TOML merge 原语与 `cmd/mcp_agents.go` 的 agent 注册表下沉为共用实现；`senv mcp install` 行为与测试保持不变
- [x] 3.2 导出入参解析：`--agent a,b` / `--all` / `--dry-run` / `--print` / `--force`；未给目标与未知 agent 的报错
- [x] 3.3 【高优先】计划生成：逐条（agent、目标路径、别名、动作、原因）并标注哪些条目会把明文写入哪个文件
- [x] 3.4 执行：env 模板按 ref-system 严格模式解引用；写前 `.bak` 备份、0600 写入、父目录递归创建；保留无关键与其它 server；`command` 原样写入
- [x] 3.5 【高优先】本机台账 `~/.config/senv/mcp-exports.json`（0600）：读取/写入/损坏降级为空台账；漂移三态判定（senv 写 / 漂移 / 外部同名）与 `--force` 分支
- [x] 3.6 部分失败语义：单 agent 失败不中止其余，逐条报告，台账只记成功项，非零退出码汇总
- [x] 3.7 测试：JSON/TOML 合并保真、幂等（重复导出为 skip）、漂移三态、`--force`、引用解析失败不改文件、`--dry-run`/`--print` 不落盘、明文不进台账、文件权限
- [x] 3.8 测试：多 agent 部分失败（其中一个目标不可写）时其余成功且台账一致

## 4. 撤回

- [x] 4.1 `senv mcp unexport`：内容一致直接删除，被本地修改需确认；不自动触发于 `mcp delete`
- [x] 4.2 测试：一致删除、被改过需确认、撤回后配置中其它内容保留、台账同步清理

## 5. MCP 只读工具

- [x] 5.1 `mcp_server_list` 与响应白名单视图（alias/传输类型/描述，不含值与参数）；`registerMCPTools` 与 `toolCatalogue` 同步注册
- [x] 5.2 测试：白名单字段、空数组、无写侧工具（`list-tools` 清单）、无 session 时拒绝

## 6. 文档与收尾

- [x] 6.1 `.agents/skills/senv-cli/SKILL.md` 更新至 1.3：新命令/flag、明文落盘提示、台账位置、MCP 工具数由 20 → 21
- [x] 6.2 `make check` 全绿；`openspec validate add-mcp-server-export --strict` 通过
- [x] 6.3 回填 proposal「验证记录」（命令、结果、日期）
