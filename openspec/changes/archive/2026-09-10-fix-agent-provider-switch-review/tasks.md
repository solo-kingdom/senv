## 1. Provider 校验与凭据语义 [高优先级]

- [x] 1.1 实现共享 provider URL validator：默认 HTTPS、显式允许 HTTP、拒绝空 host 与 userinfo；补 table-driven 单测覆盖合法 HTTPS、HTTP 放行、HTTP 拒绝、userinfo、空 host。验证：`go test ./internal/llm ./internal/storage`。
- [x] 1.2 修改 `ai provider add`：移除 `--api-key`，新增不回显 TTY prompt 与 `--api-key-stdin`，确保 secret 不进入 flag/输出/审计；补交互与非交互 stdin 测试。验证：`go test ./cmd` 并人工检查 `ps` 参数与输出不含 key。
- [x] 1.3 实现 force 凭据语义：未提供凭据保留旧自有凭据，owned→external 成功后清理旧凭据，补偿失败保留可回退状态；补新增/覆盖/切换引用/补偿失败测试。验证：`go test ./internal/llm`。
- [x] 1.4 实现 remove 两阶段删除与补偿：凭据缺失继续删档案，凭据删除失败保留档案，档案删除失败恢复凭据；补四种路径测试。验证：`go test ./internal/llm ./cmd`。

## 2. Vault 生命周期 [高优先级]

- [x] 2.1 在 `ValidateName`/`securefs.ValidateSegment` 拒绝控制字符，并让 provider add/remove/审计共用已验证 alias；补换行、回车、tab、NUL 与非法身份测试。验证：`go test ./internal/securefs ./internal/storage ./cmd`。
- [x] 2.2 将 `llm_providers` 加入 rekey 分类、迁移计数与 manifest 处理；补含 provider 的 password change/rekey 测试。验证：`go test ./internal/storage -run 'Rekey|Password'`。
- [x] 2.3 将 `llm_providers` 加入 `HasOrphanedData` 与 `CheckConsistency`，扩展报告计数；补 provider-only orphan 与坏密文一致性测试。验证：`go test ./internal/storage -run 'Orphan|Consistency'`。
- [x] 2.4 在 provider load 后复验 URL、模型集、默认模型和凭据引用；补解密后坏档案被切换/TUI/MCP manager 拒绝的测试。验证：`go test ./internal/storage ./internal/llm ./cmd`。

## 3. 本地路径安全 [高优先级]

- [x] 3.1 让 home 解析显式失败，移除 agent/指针路径的 `"."` fallback；补无 HOME 环境下不写 cwd 的测试。验证：`env -u HOME go test ./cmd ./internal/llm`。
- [x] 3.2 为 pointer 与本功能创建/使用的 agent 配置目录增加 stat 后收敛 0700、文件 0600 的写路径；补已存在 0755 目录和 0644 文件测试。验证：`go test ./internal/llm` 并断言权限位。

## 4. Switch 事务 [高优先级]

- [x] 4.1 实现按规范化路径的进程内 mutex registry；补同一 agent 并发 switch 的 race 测试，断言最终状态一致。验证：`go test -race ./internal/llm`。
- [x] 4.2 引入 per-file 独立快照、temp+fsync+rename、安全恢复和目录 fsync；确保恢复不覆盖唯一好快照，成功后清理备份；补写失败、恢复失败、成功清理测试。验证：`go test ./internal/llm`。
- [x] 4.3 将单文件 adapter 接入事务，pointer 保存失败时回滚配置并保留/清除快照语义正确；补 adapter apply 失败与 pointer save 失败测试。验证：`go test ./internal/llm`。
- [x] 4.4 将 Pi `models.json` 与 `settings.json` 放入同一事务，第二文件失败时恢复第一文件；补集成测试。验证：`go test ./internal/llm -run Pi`。

## 5. TOML 安全合并

- [x] 5.1 新增 TOML 解析依赖并替换 Codex/Kimi 行级编辑为 decode-modify-encode；保持只修改本功能拥有的 path。验证：`go test ./internal/llm -run 'Codex|Kimi'`。
- [x] 5.2 补多行数组、多行字符串、array of tables、无关键保留与重复 `model_provider` 回归测试。验证：`go test ./internal/llm`。

## 6. 文档与整体验证

- [x] 6.1 更新 README/help 中 provider add 示例：prompt/stdin、HTTPS 默认、`--allow-http` 与 `--api-key` 移除说明。验证：`senv ai provider add --help` 与文档 grep 不再引导 `--api-key`。
- [x] 6.2 运行 `make check`，修复 vet/lint/race 测试问题。验证：`make check` 全部通过。
