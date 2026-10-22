# Tasks

## 1. senv-cli skill

- [x] 1.1 `.agents/skills/senv-cli/SKILL.md` 新增「私有 Agent Skill」节（用法、dotf 边界、软链不迁移、1 MiB / 1.5 MiB、server 先升级、64 MB 提示、agent 先 dry-run 再 --yes）
- [x] 1.2 同步 server 模式段、同步白名单提醒、常用命令速查、简介；版本 1.28 → 1.29

## 2. README 与 server 文档

- [x] 2.1 `README.md`：功能条目 + 「管理私有 Agent Skill」小节
- [x] 2.2 `README_EN.md`：同上英文
- [x] 2.3 `docs/senv-server.md`：发布顺序提示补 `skill`/`skill_index`

## 3. 镜像

- [x] 3.1 删除旧目录后 `cp -a` 同步到 `~/code/.agents/skills/senv-cli`，确认非软链

## 4. 验证

- [x] 4.1 `go run . skill --help` 及五个子命令 `--help` 与文档逐项对照
- [x] 4.2 `openspec validate senv-skills-config-docs --strict`
- [x] 4.3 `diff -r` 仓内真源与工作区镜像无差异
- [x] 4.4 worktree 内提交，不 push
