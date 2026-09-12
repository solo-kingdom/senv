## Review 总结 · ssh-sync

范围：`senv` / `uncommitted` `None` → `working-tree`
结论：P0=0 P1=1 P2=0 P3=10
完整文档：`docs/reviews/2026-09-13/ssh-sync/review.md`

### P1

- [P1] Regression: the pre-diff code had `if cols < 1 { cols = 1 }` to force a single column when the terminal was too narrow f… — `internal/tui/help.go:141-146`

### P3

- [P3] The new doc comment on `Export` is mixed Chinese/English ("返回的 warnings 逐条指出 IdentityKey 引用..."). The Go doc convention … — `internal/ssh/host.go:152-155`
- [P3] The doc comment mixes English and Chinese, breaking the English-only exported-doc convention used elsewhere in this repo… — `internal/ssh/host.go:152-154`
- [P3] The new keypair lookup calls `m.storage.ListKeyPairs()` directly, bypassing the existing `m.listKeyPairs()` helper (mana… — `internal/ssh/host.go:171-179`
- [P3] The new keypair listing calls `m.storage.ListKeyPairs()` directly, while the surrounding code in `host.go` (e.g. `listHo… — `internal/ssh/host.go:171-179`
- [P3] The new code path treats a failure of `ListKeyPairs()` (e.g. corrupt keypair directory, lock contention during rekey) as… — `internal/ssh/host.go:171-175`
- [P3] The `if/else` chain around `m.storage.ListKeyPairs()` is awkward Go style — declaring `err` in the `if` initializer and … — `internal/ssh/host.go:173-179`
- [P3] The `if/else` chain around `m.storage.ListKeyPairs()` is awkward Go style — declaring `err` in the `if` initializer and … — `internal/ssh/host.go:173-179`
- [P3] First-line overflow when `colW < indent`. `wrapCell` builds the first wrapped line as `padRight(string(rs[:indent])+chun… — `internal/tui/help.go:288-295`
- [P3] Continuation-line overflow (parallel to the first-line issue flagged above). `wrapWords(string(rs[indent:]), width)` ret… — `internal/tui/help.go:292-301`
- [P3] Width accounting in `wrapWords` uses byte length, not display columns. The greedy pack test `if len(cur)+1+len(w) <= wid… — `internal/tui/help.go:320-331`
