# Spec Delta

## ADDED Requirements

### Requirement: senv-cli skill 说明 skill 配置面

仓内 `.agents/skills/senv-cli/SKILL.md` SHALL 说明 `senv skill add/list/show/remove/apply` 的用法，并写明：与 dotf 的边界、存量软链不自动迁移、明文 1 MiB 与 skill 密文 1.5 MiB 上限、server provider 须先升级 senv-server 再 `skill add`/push、大批量 push 可能触达 64 MB 请求体上限。

#### Scenario: 读者查 server 升级顺序
- **WHEN** agent 阅读「私有 Agent Skill」一节
- **THEN** 能读到「先升级 senv-server 镜像，再 `skill add` / `senv sync`」及原因

#### Scenario: 命令与 help 一致
- **WHEN** 对照 `go run . skill <sub> --help`
- **THEN** 文档中的子命令与 flag 名称、选择器规则一致

### Requirement: README 双语覆盖 skills

`README.md` 与 `README_EN.md` SHALL 各含 skills 功能条目与使用小节，内容与 SKILL.md 的约束一致。

#### Scenario: 双语都有小节
- **WHEN** 搜索两份 README 中的 `senv skill apply`
- **THEN** 均有命中

### Requirement: 工作区镜像与真源一致

`~/code/.agents/skills/senv-cli` SHALL 是仓内 `.agents/skills/senv-cli/` 的整目录拷贝（非符号链接）。

#### Scenario: 镜像无差异
- **WHEN** 对两个目录执行 `diff -r`
- **THEN** 无差异
