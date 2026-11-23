# Decision — 20260912-senv-darwin-session-recovery

**promote**（2026-09-12，用户两度确认：Proposal 确认 + 候选 diff 确认）

## 原因

- 本会话（macOS 笔电接入自建 senv-server 后的 tui Unverifiable 排障）提供了稳定、可复用、可验证的模式：
  darwin 上 senv 会话 tmpfs fail-closed 报错时，tui/status/session clear 硬失败而唯 session start
  回退弹密码，恢复套路为「引导 start 重建；确认无唯一钥匙风险后再清两份残留载体」；
  行为判定须以已装二进制取证（go version -m / strings / 实测），不以源码 checkout 为准。
- 本会话内三个真实场景（tui 硬失败引导、坏载体清理、源码版本错位取证）按新指令推演均可避免原绕路
  （原排障 ~30 分钟，新路径两步收敛）。
- Evaluate pass：生产稿零删改（两处纯新增）、触发范围不变、未削弱「不可判定不销毁」等既有门禁、
  无敏感信息。
- fleet 已有多台 darwin（公司 MacBook、Mac mini 等）共用同一 senv 二进制通道，该模式会复现，
  具备进生产稿的价值。

## 落地

- 候选 SKILL.md（version 1.5 → 1.6）已覆盖生产稿 `.agents/skills/senv-cli/SKILL.md`。
- 遗留观察（未夹带本轮）：「非交互执行规则」的 `--insecure-cache` 措辞与 v0.1.26 darwin
  默认落盘行为存在张力，留待后续真实经验再评估。
