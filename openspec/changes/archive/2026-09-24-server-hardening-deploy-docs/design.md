## Context

见 proposal.md。本切片最后实现，内容依赖前三个切片的实际 flag/env 名，交叉引用需以最终实现为准。

## Decisions

1. **落点 `docs/senv-server.md` 既有文件**：与「构建与发布」同文档，管理员单入口。备选独立 `docs/hardening.md` 被否：多分一文件增加检索成本。
2. **GRANT 模板不复制**：引用 audit-db-roles 落位处，避免双份漂移。

## Risks / Trade-offs

- [文档与实现 drift] → driver 收尾任务要求逐项核对 flag/env 名；验收标准已列
