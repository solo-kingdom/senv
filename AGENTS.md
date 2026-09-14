# AGENTS.md

## 产品形态

CLI 与 TUI 是 senv 的两个一等交互面。新增或变更用户可见能力（命令、flag、按键、交互流程）时默认两边同时评估：CLI 落地必须检查 TUI 入口是否同步，反之亦然。把 TUI 排除在范围外之前，先与用户确认使用模式——不得用「最小对齐」默认推迟 TUI（2026-09 教训：host export 应用化时推迟 TUI 入口，旧导出交互残留旧教学提示，且目录可填进 senv 自有树被幽灵清理误删）。

## Agent skills

### Issue tracker

Issues are tracked in the repo's GitHub Issues (solo-kingdom/senv), operated via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context layout: `CONTEXT.md` and `docs/adr/` at the repo root (created lazily by domain-modeling skills). See `docs/agents/domain.md`.

### senv client

Agent-facing senv client guidance lives in `.agents/skills/senv-cli/SKILL.md`. When a change adds or changes user-visible `cmd/` commands/flags, interaction or security behavior, or MCP tools, update that skill in the same change. Verify syntax with `go run . --help`, `go run . <command> --help`, and `go run . mcp list-tools`.

## Deployment

senv-server 生产镜像的构建与发布：**优先在 iship 上构建**（有仓 checkout、docker，Go 模块代理可达）；不要在 tcbj 上直接 docker build（Go 模块代理不可达，`go mod download` 必挂）。流程与踩坑记录见 `docs/senv-server.md` 的「构建与发布」一节。
