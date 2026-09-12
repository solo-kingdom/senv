## 1. CONTEXT.md

- [x] 1.1 新增"配置源"术语条目（含与本机状态的相对关系）
- [x] 1.2 "当前指向""导出状态"条目标注"本机状态（同步边界语境）：不随 vault 同步"，与"本地状态"分组显式区分
- 验证：通读无实现细节混入、无与既有条目冲突

## 2. ADR 0019

- [x] 2.1 落地 `docs/adr/0019-sync-ai-mcp-source-of-truth.md`（proposed；编号按落地时扫描取下一空位），Considered Options / Consequences 覆盖 ssh 延后项
- 验证：格式对齐既有 ADR

## 3. senv-cli skill

- [x] 3.1 `.agents/skills/senv-cli/SKILL.md` 补新 kind 同步行为、宽松 export + warning、ai switch 凭据缺失诊断
- [x] 3.2 `go run . --help` / `go run . mcp export --help` / `go run . ai switch --help` 语法核对（文档不涉新 flag，确认无漂移）
