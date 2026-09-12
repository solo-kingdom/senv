---
repo: senv
mode: uncommitted
date: 2026-09-13
change_name: ssh-sync
branch: ssh-sync
from: None
to: working-tree
mr: 
ocr_session: 8de2d411-d4fc-44bc-aa48-f8180b7fd7ca
ocr_status: complete
---

# Code Review · ssh-sync

## Meta

- 仓库：`senv`
- 范围：`uncommitted` `None` → `working-tree`
- 分支：`ssh-sync`（默认 `main`）
- OCR：files=11 comments=11 elapsed=12m44s session=`8de2d411-d4fc-44bc-aa48-f8180b7fd7ca`
- OCR message：Review complete: 11 finding(s) across 11 selected item(s).

## 统计

P0=0 / P1=1 / P2=0 / P3=10

## Findings（完整）

### P1

#### 1. Regression: the pre-diff code had `if cols < 1 { cols = 1 }` to force a single column when the terminal was too narrow f…

- 位置：`internal/tui/help.go:141-146`
- 优先级：P1

Regression: the pre-diff code had `if cols < 1 { cols = 1 }` to force a single column when the terminal was too narrow for the multi-column target. That guard was dropped here, so for any terminal where `contentW+helpColDividerW < helpColTarget` (i.e. contentW ≤ 32, roughly h.width ≤ 42 after subtracting overlay cols + margins), `(contentW+helpColDividerW)/helpColTarget` is 0, `minInt(0, 2)` is 0, the second guard only fires when `maxCols > len(items)` so `maxCols` stays 0, and the `for c := 1; c <= maxCols` loop never executes — leaving `rows = nil`. The overlay renders an empty body and `maxScroll()` returns 0, so on a narrow terminal the user sees a blank help box with only the "Global" heading missing. The pre-diff layout still drew the bindings in a single column on the same width. Fix: force `maxCols` to at least 1 when there are items, e.g. `if maxCols < 1 && len(items) > 0 { maxCols = 1 }` after the existing length clamp.

**现有代码**

```
maxCols := minInt((contentW+helpColDividerW)/helpColTarget, 2)
	if maxCols > len(items) {
		maxCols = len(items)
	}
	visible := h.visibleRows()
	for c := 1; c <= maxCols; c++ {
```

**建议改法**

```
maxCols := minInt((contentW+helpColDividerW)/helpColTarget, 2)
	if maxCols > len(items) {
		maxCols = len(items)
	}
	if maxCols < 1 && len(items) > 0 {
		maxCols = 1
	}
	visible := h.visibleRows()
	for c := 1; c <= maxCols; c++ {
```

### P3

#### 1. The new doc comment on `Export` is mixed Chinese/English ("返回的 warnings 逐条指出 IdentityKey 引用..."). The Go doc convention …

- 位置：`internal/ssh/host.go:152-155`
- 优先级：P3

The new doc comment on `Export` is mixed Chinese/English ("返回的 warnings 逐条指出 IdentityKey 引用..."). The Go doc convention across this repo is English-only for exported API, and prior reviews (e.g. `docs/reviews/2026-09-11/senv-tui-perf/review.md` issue #1057) flagged mixed-language exported comments as a blocker for grep audits and translation tooling. The Chinese warning string emitted to stderr is spec-mandated and stays; the godoc above the signature should be English.

Suggested rewrite:

```go
// Export renders an OpenSSH config fragment in a deterministic alias order.
// The returned warnings list each IdentityKey reference whose target keypair
// is not present in the local vault (e.g. blob has not synced to this machine
// per ADR-0020 D4); dangling references do not abort the export.
```

The inline `// keypair 名单来自目录列表...` comment is fine as internal commentary.

**现有代码**

```
// Export renders an OpenSSH config fragment in a deterministic alias order.
// 返回的 warnings 逐条指出 IdentityKey 引用、但不在本机 vault 的 keypair
// （如档案尚未同步到本机，ADR-0020 D4）；悬空引用不阻断导出。
func (m *Manager) Export(alias string) (string, []string, error) {
```

#### 2. The doc comment mixes English and Chinese, breaking the English-only exported-doc convention used elsewhere in this repo…

- 位置：`internal/ssh/host.go:152-154`
- 优先级：P3

The doc comment mixes English and Chinese, breaking the English-only exported-doc convention used elsewhere in this repo (see prior review at `docs/reviews/2026-09-11/senv-tui-perf/review.md` issue #1057). Reword in English and drop the ADR reference (the body code is self-explanatory):

```go
// Export renders an OpenSSH config fragment in a deterministic alias order.
// The returned warnings list IdentityKey references whose target keypair is
// not present in the local vault (per ADR-0020 D4, when sync has not yet
// delivered the keypair); dangling references do not abort the export.
```

**现有代码**

```
// Export renders an OpenSSH config fragment in a deterministic alias order.
// 返回的 warnings 逐条指出 IdentityKey 引用、但不在本机 vault 的 keypair
// （如档案尚未同步到本机，ADR-0020 D4）；悬空引用不阻断导出。
```

**建议改法**

```
// Export renders an OpenSSH config fragment in a deterministic alias order.
// The returned warnings list IdentityKey references whose target keypair is
// not present in the local vault (per ADR-0020 D4, when sync has not yet
// delivered the keypair); dangling references do not abort the export.
```

#### 3. The new keypair lookup calls `m.storage.ListKeyPairs()` directly, bypassing the existing `m.listKeyPairs()` helper (mana…

- 位置：`internal/ssh/host.go:171-179`
- 优先级：P3

The new keypair lookup calls `m.storage.ListKeyPairs()` directly, bypassing the existing `m.listKeyPairs()` helper (manager.go:364-375) that the rest of `internal/ssh/host.go` patterns through (cf. `listHostsEntries` at line 49). The storage path still acquires `withVaultRead` correctly via `listSSHEntries` (storage/ssh.go:227), so this is functionally safe but breaks the manager/storage abstraction. The existing helper also documents the `mutationLocked` branch. For consistency, route through `m.listKeyPairs()` (or a new public `ListKeyPairNames()` if the helper stays private) and add `perflog.Start("ssh.list-keypair-names")` to match the `ListHosts`/`ListKeyPairs` telemetry shape.

Suggested refactor:

```go
keypairNames := make(map[string]bool)
if err := m.collectKeyPairNames(keypairNames); err != nil {
    return "", nil, err
}
```

where `collectKeyPairNames` wraps `perflog` and either delegates to `m.listKeyPairs()` or takes the storage read-lock directly.

**现有代码**

```
// keypair 名单来自目录列表，不逐 host 解密 keypair。
	keypairNames := make(map[string]bool)
	if names, err := m.storage.ListKeyPairs(); err != nil {
		return "", nil, err
	} else {
		for _, name := range names {
			keypairNames[name] = true
		}
	}
```

#### 4. The new keypair listing calls `m.storage.ListKeyPairs()` directly, while the surrounding code in `host.go` (e.g. `listHo…

- 位置：`internal/ssh/host.go:171-179`
- 优先级：P3

The new keypair listing calls `m.storage.ListKeyPairs()` directly, while the surrounding code in `host.go` (e.g. `listHostsEntries` at line 49) routes through the manager helper layer. The existing `m.listKeyPairs()` helper at `manager.go:364-375` already implements the same lock acquisition pattern (`WithVaultMutation` for the read), so the behavior is functionally equivalent — but going through the helper keeps the abstraction layer consistent with the rest of the file and ensures future changes to the manager-side lock contract (e.g. adding a `perflog` boundary, switching to `withVaultRead`) propagate automatically. Use the helper:

```go
names, err := m.listKeyPairs()
if err != nil {
    return "", nil, err
}
keypairNames := make(map[string]bool, len(names))
for _, name := range names {
    keypairNames[name] = true
}
```

Note: this is a style/consistency issue, not a correctness defect — the direct call still acquires the vault mutation lock via `listSSHEntries → withVaultRead`.

**现有代码**

```
	// keypair 名单来自目录列表，不逐 host 解密 keypair。
	keypairNames := make(map[string]bool)
	if names, err := m.storage.ListKeyPairs(); err != nil {
		return "", nil, err
	} else {
		for _, name := range names {
			keypairNames[name] = true
		}
	}
```

**建议改法**

```
	// keypair 名单来自目录列表，不逐 host 解密 keypair。
	names, err := m.listKeyPairs()
	if err != nil {
		return "", nil, err
	}
	keypairNames := make(map[string]bool, len(names))
	for _, name := range names {
		keypairNames[name] = true
	}
```

#### 5. The new code path treats a failure of `ListKeyPairs()` (e.g. corrupt keypair directory, lock contention during rekey) as…

- 位置：`internal/ssh/host.go:171-175`
- 优先级：P3

The new code path treats a failure of `ListKeyPairs()` (e.g. corrupt keypair directory, lock contention during rekey) as a hard export error rather than degrading to "warn and continue". That contradicts the loose-mode spirit of this change (ADR-0020 D4, mirrored on `mcp export`) — a sync mid-flight could legitimately leave the keypair listing in a transient bad state, and the user's intent is "show what we can render". Either:

1. Wrap the listing in a `best-effort` block and add a synthetic warning like `could not list keypairs in local vault: <err>` so the caller still gets a partial fragment, or
2. Document explicitly (in the `Export` doc comment) that storage-level failures of the keypair listing still abort the export, distinct from missing references.

This is a design decision rather than a defect — flagging because it diverges from the "宽松写入" framing in `openspec/changes/ssh-sync-export-warning/design.md` D1 without an explicit exception.

**现有代码**

```
	// keypair 名单来自目录列表，不逐 host 解密 keypair。
	keypairNames := make(map[string]bool)
	if names, err := m.storage.ListKeyPairs(); err != nil {
		return "", nil, err
	} else {
```

#### 6. The `if/else` chain around `m.storage.ListKeyPairs()` is awkward Go style — declaring `err` in the `if` initializer and …

- 位置：`internal/ssh/host.go:173-179`
- 优先级：P3

The `if/else` chain around `m.storage.ListKeyPairs()` is awkward Go style — declaring `err` in the `if` initializer and then assigning into an already-declared `names` from the same scope is unusual and reads as a typo. A plain early-return reads cleaner:

```go
names, err := m.listKeyPairNames()
if err != nil {
    return "", nil, err
}
for _, name := range names {
    keypairNames[name] = true
}
```

This also gives you a single chokepoint to add `perflog` and to route through the manager abstraction in one place.

**现有代码**

```
if names, err := m.storage.ListKeyPairs(); err != nil {
		return "", nil, err
	} else {
		for _, name := range names {
			keypairNames[name] = true
		}
	}
```

#### 7. The `if/else` chain around `m.storage.ListKeyPairs()` is awkward Go style — declaring `err` in the `if` initializer and …

- 位置：`internal/ssh/host.go:173-179`
- 优先级：P3

The `if/else` chain around `m.storage.ListKeyPairs()` is awkward Go style — declaring `err` in the `if` initializer and then assigning into a pre-declared `names` from the same scope reads as a typo, and `if { return } else { ... }` for what is just an early-return on error is non-idiomatic. A plain early-return is cleaner; see the combined suggested fix in the previous comment.

**现有代码**

```
	if names, err := m.storage.ListKeyPairs(); err != nil {
		return "", nil, err
	} else {
		for _, name := range names {
			keypairNames[name] = true
		}
	}
```

**建议改法**

```
	names, err := m.listKeyPairs()
	if err != nil {
		return "", nil, err
	}
	keypairNames := make(map[string]bool, len(names))
	for _, name := range names {
		keypairNames[name] = true
	}
```

#### 8. First-line overflow when `colW < indent`. `wrapCell` builds the first wrapped line as `padRight(string(rs[:indent])+chun…

- 位置：`internal/tui/help.go:288-295`
- 优先级：P3

First-line overflow when `colW < indent`. `wrapCell` builds the first wrapped line as `padRight(string(rs[:indent])+chunks[0], colW)`; `padRight` only right-pads and never truncates (verified at line 337), so when `colW < indent` the resulting line is `indent + len(chunks[0])` columns wide while the column slot is `colW` columns. The single-column branch in `layout` clamps `colW` to `maxInt(colW, 8)` but not to `indent`, and `indent` reaches 18 once `keyWidth()` returns the new cap of 15 (binding format is `"  %-*s %s"` so `indent = kw+3`). The two-column branch bails out at `colW < 14` for c=2, but the single-column branch unconditionally proceeds. Concrete trigger: a terminal wide enough that `contentW` reaches its 10-column floor (h.width ≈ 20) combined with any binding whose key is the new 15-wide maximum — the first wrapped row visibly exceeds the column budget and the `│` gutter separator no longer aligns with the column boundary. Either truncate the prefix when it does not fit (`if colW < indent { return []string{padRight(text, colW)} }`), or document that `helpTab` requires `h.width ≥ overlayCols+2+helpMarginX+keyWidth()+3` (≈ 31 cols) and refuse to open on narrower terminals.

**现有代码**

```
rs := []rune(text)
	if len(rs) <= indent {
		return []string{padRight(text, colW)}
	}
	width := colW - indent
	if width < 4 {
		width = 4
	}
```

**建议改法**

```
rs := []rune(text)
	if len(rs) <= indent || colW <= indent {
		return []string{padRight(text, colW)}
	}
	width := colW - indent
	if width < 4 {
		width = 4
	}
```

#### 9. Continuation-line overflow (parallel to the first-line issue flagged above). `wrapWords(string(rs[indent:]), width)` ret…

- 位置：`internal/tui/help.go:292-301`
- 优先级：P3

Continuation-line overflow (parallel to the first-line issue flagged above). `wrapWords(string(rs[indent:]), width)` returns chunks whose length is at most `width = max(colW-indent, 4)`, so `len(c) <= 4` when the width floor kicks in. The continuation `padRight(strings.Repeat(" ", indent)+c, colW)` is therefore `indent + len(c)` columns wide — up to `indent + 4` — but `padRight` only right-pads and never truncates. When `colW < indent + 4` the line spills past the column budget (e.g., `indent=18, width=4, colW=18` → 22 cols rendered into an 18-col column), the row joins the gutter and the rendered help box exceeds the pane width. The same root cause as the first-line overflow: either drop the `if width < 4 { width = 4 }` floor (use `max(width, 1)`) or truncate the chunk before padding so each line is clamped to `colW`.

**现有代码**

```
	width := colW - indent
	if width < 4 {
		width = 4
	}
	chunks := wrapWords(string(rs[indent:]), width)
	lines := make([]string, 0, len(chunks))
	lines = append(lines, padRight(string(rs[:indent])+chunks[0], colW))
	for _, c := range chunks[1:] {
		lines = append(lines, padRight(strings.Repeat(" ", indent)+c, colW))
	}
```

#### 10. Width accounting in `wrapWords` uses byte length, not display columns. The greedy pack test `if len(cur)+1+len(w) <= wid…

- 位置：`internal/tui/help.go:320-331`
- 优先级：P3

Width accounting in `wrapWords` uses byte length, not display columns. The greedy pack test `if len(cur)+1+len(w) <= width` is `len(string)`, which equals byte count for ASCII but under-counts runes for any non-ASCII word — packing will overrun `width` whenever a word contains multi-byte characters. The pre-existing comment "键位描述均为 ASCII" plus `KeyAction.Desc`'s docstring "英文说明" make this safe today, but `wrapWords` has no defensive rune-length comparison and `bindingCell`/`wrapCell` only re-measure via `lipgloss.Width` after the fact, so a CJK description would silently push the rendered row past `colW` and break column alignment the same way as the `colW < indent` overflow above. If you want to harden this, switch the pack test to `lipgloss.Width(cur)+1+lipgloss.Width(w) <= width` (and the hard-cut in `flush` to rune-width chunks); if you want to keep the ASCII fast path, add an explicit guard at the top of `wrapWords` (`if !isASCII(s) { ... }`) so a future non-ASCII binding surfaces a deterministic misrender instead of an alignment glitch.

**现有代码**

```
for _, w := range strings.Fields(s) {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) <= width {
			cur += " " + w
			continue
		}
		flush()
		cur = w
	}
```

**建议改法**

```
for _, w := range strings.Fields(s) {
		if cur == "" {
			cur = w
			continue
		}
		if lipgloss.Width(cur)+1+lipgloss.Width(w) <= width {
			cur += " " + w
			continue
		}
		flush()
		cur = w
	}
```
