# 修正 TUI 数字键与全部注册时的 Tab 顺序

- target: .agents/skills/senv-cli
- mode: update
- patch: 20261125-234731-tui-number-key-order
- risk: low
- status: proposed

## Intent

把「TUI 键位」里 `1`–`9` 的全部注册顺序改成与 `internal/tui/model.go` 一致：Env、Text、Providers、Skill、Config、SSH、KeyPair、MCP、Ledger、Tree、History、Audit、Backup、Settings。数字键只到 Ledger；Tree、History、Audit、Backup、Settings 用 Tab。History 仅 server 模式注册，缺席不改变前 9 个 Tab 的数字键。

非目标：不改键位实现、不改其他 Tab 的操作说明、不改 frontmatter。

## Conflict check

none。Ledger / Tree 两节已写明 Tab 存在，只是全局数字键那句漏了它们，把 History 错放进前 9 个。

## Rationale

`cmd/tui.go` 在 TUI 能启动时总会装上 Ledger 与 Tree，History 仅 server 模式注入。按现在的句子回答「按 9 进 History」会进 Ledger。改动可验证：应用后该句包含 Ledger、Tree，且写明数字键止于 Ledger。

## Files

- `.agents/skills/senv-cli/SKILL.md` — 全局键位 bullet 中的数字键顺序从句。

## Validation

- 应用前：`git apply --check --recount` 通过。
- 应用后：`git diff --check` 无空白错误；该句含 `Ledger、Tree` 且写明数字键到 Ledger 为止；frontmatter `name` 仍为 `senv-cli`。
