# AGENTS.md

## Agent skills

### Issue tracker

Issues are tracked in the repo's GitHub Issues (solo-kingdom/senv), operated via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context layout: `CONTEXT.md` and `docs/adr/` at the repo root (created lazily by domain-modeling skills). See `docs/agents/domain.md`.

## Deployment

senv-server 生产镜像的构建与发布：**优先在 iship 上构建**（有仓 checkout、docker，Go 模块代理可达）；不要在 tcbj 上直接 docker build（Go 模块代理不可达，`go mod download` 必挂）。流程与踩坑记录见 `docs/senv-server.md` 的「构建与发布」一节。
