## Context

行为已在 core / apply-e2e 两个切片落地，本片只写文档。事实来源是 `go run . skill --help` 及子命令 help，以及 `internal/storage`、`internal/server/store`、`internal/server/handler` 里的常量。

## Decisions

1. **SKILL.md 单独成节**：放在「MCP Server 档案与导出」之后、「server 模式」之前，不塞进已经很长的「关键行为」。server 模式段只补一句指向与白名单提醒。
2. **agent 行为写成规则**：`apply` 写本机家目录，所以写明「先 `--dry-run` 给用户看，同意后再 `--yes`」，不替用户 `--force`。
3. **64 MB 的说法**：server 默认 `--max-body-bytes` 64 MB，client 的 `senv sync` 不分批（只有 `migrate` 分批），满额 skill 约 1.5 MiB，所以「约 40 条以上」是估算，文档写「可能触达」，不承诺具体数。
4. **README 编号**：新小节插在原第 11 节前，原「Agent Skill」顺延为第 12 节；README 无内部锚点引用这个编号。
5. **镜像**：删除旧目录后 `cp -a` 整目录同步到 `~/code/.agents/skills/senv-cli`，保证不遗留旧文件（此前副本缺 `examples/`）。

## Risks / Trade-offs

- 文档里的上限数字来自当前常量；常量变了要回改。以 help 与代码为准。
- 镜像是拷贝，仓内真源再改后需重跑同步。

## Migration Plan

纯文档。回滚：`git revert` 该提交，并重新镜像。
