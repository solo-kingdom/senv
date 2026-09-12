## 1. export 缺失引用 warning

- [x] 1.1 `internal/ssh/host.go`：导出前一次收集本机 keypair 名集合；`IdentityKey` 不在集合内的 host 逐条输出 warning 到 stderr（文案：`host <alias> 引用的 keypair <name> 不在本机 vault（可能尚未同步）`），片段照常生成；不逐 host 重复解密
- [x] 1.2 单测：host 引用缺失 keypair → stderr 含 warning 且片段完整、仍含 `IdentityFile` 路径；keypair 全部在位 → 无 warning；`proxyJump` 悬空仍报错（回归）
- 验证：`go test ./internal/ssh/ -race` 全绿

## 2. 文档与收尾

- [x] 2.1 `.agents/skills/senv-cli/SKILL.md`：同步范围更新（`ssh_host`/`ssh_keypair` 档案随 vault 同步、档案含私钥本体、新机器凭口令取用全量 SSH 资产）；host export 的 warning 行为补一句；核对 `go run . --help`、`go run . ssh --help` 输出与文档一致
- [x] 2.2 `make check` 通过
- 验证：`make check` 全绿；`go run . ssh host export --help` 正常

## 3. Review 修复（docs/reviews/2026-09-13/ssh-sync）

- [x] 3.1 P3×4：`Export` 的 keypair 名单收集改走 manager 助手 `m.listKeyPairs()`（保持 manager/storage 抽象一致），`if/else` 改平铺 early-return
- [x] 3.2 P3×3：`Export` 导出注释改纯英文（仓库导出 API 惯例），并显式写明「列举 keypair 失败按硬错误处理，与 hosts 列举失败一致」（review #9 的文档化选项）
- [x] 3.3 P1（carry-over `internal/tui/help.go` WIP，不属本 change 范围，修复并记录）：极窄终端 `maxCols` 归零致空内容体——恢复单列保底 `if maxCols < 1 { maxCols = 1 }`，新增回归测试 `TestHelpOverlayNarrowStillShowsBindings`；该文件其余 P3（折行宽度溢出 #4/#5/#10）为用户在改代码，记录不修
- 验证：`go test ./internal/ssh/ ./internal/tui/ -race` 全绿；`openspec validate --strict --type change ssh-sync-export-warning` 通过
