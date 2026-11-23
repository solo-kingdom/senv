# Eval — 20260912-senv-darwin-session-recovery

## 回归(现有成功路径未被新规则打断)

- `diff -u 生产稿 候选稿`:仅两处新增(维护约定·事实来源补半句;会话节新增 1 条),零删改、零重排,frontmatter/description 未动。
- 既有规则原样保留:「不可判定不销毁」「--insecure-cache 仅用户明确要求」「agent 不替用户输密码」「tui 为交互命令 agent 不驱动」均未改。

## 模式(原失败/绕路场景按新指令能否避免)

- 场景 A(tui 报 tmpfs fail-closed):新条目报错文案可直接匹配 → 指令为"引导用户 `senv session start` 重建,不要修环境"。本会话实测 start 在该状态下确实回退弹密码(可用),避免"读源码侦探式排障"。✓
- 场景 B(start 仍异常,坏载体残留):新条目给出两份载体(keychain `senv.session.<uid>` + 槽文件)与清理命令,顺序与前提(先确认权威副本)与本会话实际修复操作一致。✓
- 场景 C(源码与二进制版本错位):新事实来源条款要求先 `go version -m` + `strings` 取证。本会话正是靠这一组合定案(旧 clone 显示 keychain、二进制无)。✓

## 契约

- senv-cli skill 无自带测试,做指令级对照(上两行),不编造分数。

## 副作用

- 触发范围:frontmatter/description 未动,无变化。
- 安全门禁:清理动作置于从属位置("start 仍异常再清")且带前提(确认 vault 有权威副本、无唯一钥匙风险),与「不可判定不销毁」主规则不冲突,未削弱任何既有门禁。
- 敏感信息:仅含 senv 自身 keychain service/account 固定命名(`senv.session.<uid>` / `senv.v1`),非凭据;无密钥、无内部 URL、无机器专属路径(示例用 `~/.cache/senv/` 通用路径)。
- 遗留观察(超出本轮 scope,未动):「非交互执行规则」中 `--insecure-cache` 条目写"无安全内存存储时需…仅在用户明确要求时使用",而 v0.1.26 darwin 实测为默认 0600 落盘(start 不带 flag 即写)。措辞张力留待后续真实经验再评估,本轮不夹带。

## 结论

pass
