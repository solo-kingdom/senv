---
repo: senv
mode: default-branch
date: 2026-09-13
change_name: cross-machine-ai-mcp-sync
branch: cross-machine-ai-mcp-sync
from: origin/main
to: HEAD
mr: 
ocr_session: 3af11677-18f8-4643-ba32-3f26c41d3160
ocr_status: complete
---

# Code Review · cross-machine-ai-mcp-sync

## Meta

- 仓库：`senv`
- 范围：`default-branch` `origin/main` → `HEAD`
- 分支：`cross-machine-ai-mcp-sync`（默认 `main`）
- OCR：files=17 comments=9 elapsed=5m42s session=`3af11677-18f8-4643-ba32-3f26c41d3160`
- OCR message：Review complete: 9 finding(s) across 17 selected item(s).

## 统计

P0=0 / P1=2 / P2=2 / P3=5

## Findings（完整）

### P1

#### 1. Warnings are aggregated once per alias (line 124-137) and then the same slice reference is assigned to every (target, al…

- 位置：`internal/mcp/export.go:124-137`
- 优先级：P1

Warnings are aggregated once per alias (line 124-137) and then the same slice reference is assigned to every (target, alias) item on line 150. When the plan contains multiple targets (e.g. `--all`), `printExportItemWarnings` (cmd/mcp_export.go:251) prints the same warning once per target even though `ResolveLoose` is deterministic and the warning text already carries the alias ("MCP server %q ..."). For 5 targets and 2 unresolved fields the user gets 10 identical lines. Consider either (a) computing `resolveWarn` per alias and emitting it once outside the target loop, or (b) deduplicating in the printer (e.g. group by warning text and suffix the list of affected agents).

**现有代码**

```
	resolveWarn := make(map[string][]string, len(names))
	for _, alias := range names {
		entry, err := e.mgr.Get(alias)
		if err != nil {
			return nil, err
		}
		server, warnings, err := e.resolveEntry(entry)
		if err != nil {
			resolveErr[alias] = err
			continue
		}
		desired[alias] = server
		resolveWarn[alias] = warnings
	}
```

#### 2. When `ResolveLoose` returns multiple unresolved references for a single field, the current `fmt.Sprintf("...: %s", warn)…

- 位置：`internal/mcp/export.go:362-364`
- 优先级：P1

When `ResolveLoose` returns multiple unresolved references for a single field, the current `fmt.Sprintf("...: %s", warn)` formats the `[]string` with Go's default `[a b]` slice syntax, e.g. `MCP server "x" env FOO: [missing env BAR missing env BAZ]`. Emit one warning per unresolved reference (or join with `, `) so each entry is independently readable and greppable.

**现有代码**

```
			if len(warn) > 0 {
				warnings = append(warnings, fmt.Sprintf("MCP server %q %s: %s", entry.Alias, field, warn))
			}
```

### P2

#### 1. Line 121 wraps the raw `securefs.PathError` (with `Path: "env/<group>/<key>.enc"`), so the user-visible error becomes `v…

- 位置：`internal/env/manager.go:119-122`
- 优先级：P2

Line 121 wraps the raw `securefs.PathError` (with `Path: "env/<group>/<key>.enc"`), so the user-visible error becomes `variable <KEY> not found in group <GROUP>: securefs read "env/<group>/<key>.enc": no such file or directory`. This leaks the internal storage layout convention in the user-facing message and is inconsistent with the cleaner `os.ErrNotExist` wrap at line 130. For parity and a less noisy user message, mirror line 130 and wrap `os.ErrNotExist` directly here — the `errors.Is(err, os.ErrNotExist)` contract is satisfied by either form, since `securefs.PathError` unwraps to `syscall.ENOENT` which implements `Is(os.ErrNotExist)`.

**现有代码**

```
if os.IsNotExist(err) {
			// 保留哨兵错误（%w），调用方可用 errors.Is 判定"条目缺失"
			return "", fmt.Errorf("variable %s not found in group %s: %w", key, group, err)
		}
```

**建议改法**

```
if os.IsNotExist(err) {
			return "", fmt.Errorf("variable %s not found in group %s: %w", key, group, os.ErrNotExist)
		}
```

#### 2. executeTarget re-resolves entries (lines 295, 329) and discards the freshly-generated warnings, so the final `item.Warni…

- 位置：`internal/mcp/export.go:295-301`
- 优先级：P2

executeTarget re-resolves entries (lines 295, 329) and discards the freshly-generated warnings, so the final `item.Warnings` only reflects Plan-time state. For env-template resolution this is currently benign (deterministic), but it creates a silent invariant: if `ResolveLoose` ever becomes stateful or the stored entry changes between Plan and executeTarget, the user-visible warnings would not match what was actually written. Consider either reusing the Plan-time resolved server (e.g., attach it to the item or stash it on a shared map) or updating `item.Warnings` from the second resolve call.

**现有代码**

```
		server, _, err := e.resolveEntry(entry)
		if err != nil {
			item.Action = ActionError
			item.Reason = err.Error()
			continue
		}
		file.set(item.Alias, server)
```

### P3

#### 1. The `combinedGetter` instantiation pattern is now duplicated between `cmd/text.go:351-352` and the new code in `mcpExpor…

- 位置：`cmd/mcp_export.go:82-85`
- 优先级：P3

The `combinedGetter` instantiation pattern is now duplicated between `cmd/text.go:351-352` and the new code in `mcpExporter` (this file). Since both call sites use the identical construction and pass it to `ref.ResolveWithWarnings`, consider exposing a thin helper (e.g., `resolveValueWithLoose(value, envMgr, textMgr)`) in `cmd/text.go` to consolidate. Not blocking.

Source pattern in `cmd/text.go:351`:
```go
getter := &combinedGetter{envManager: envMgr, textManager: textMgr}
opts := ref.ResolveOptions{Loose: loose, CurrentGroup: currentGroup}
result, warnings, err := ref.ResolveWithWarnings(value, getter, opts)
```

**现有代码**

```
getter := &combinedGetter{envManager: envMgr, textManager: textMgr}
	resolveLoose := func(value string) (string, []string, error) {
		return ref.ResolveWithWarnings(value, getter, ref.ResolveOptions{Loose: true})
	}
```

**建议改法**

```
resolveLoose := func(value string) (string, []string, error) {
		return resolveValueWith(value, true, "", envMgr, textMgr)
	}
```

#### 2. Minor UX consistency: between `printExportPlan` (which surfaces `[未解析引用]` markers) and `printExportSnippets` (the `--pri…

- 位置：`cmd/mcp_export.go:259-269`
- 优先级：P3

Minor UX consistency: between `printExportPlan` (which surfaces `[未解析引用]` markers) and `printExportSnippets` (the `--print` path), the snippets don't carry any visual marker for items that still contain template literals. Users reading only the snippet output might miss the unresolved-ref status. The stderr warning already covers this, but consider adding a comment line for affected items so the snippet is self-describing.

The `# %s — add to %s` header is the natural place; e.g. `# warning: ...` lines before each affected snippet.

**现有代码**

```
func printExportSnippets(plan *mcp.ExportPlan) {
	for _, item := range plan.Items {
		server := item.Server()
		if item.Action != mcp.ActionCreate && item.Action != mcp.ActionUpdate {
			continue
		}
		target, ok := agentcfg.Find(item.Agent)
		if !ok {
			continue
		}
		fmt.Printf("\n# %s — add to %s\n", item.AgentName, item.Path)
```

**建议改法**

```
func printExportSnippets(plan *mcp.ExportPlan) {
	for _, item := range plan.Items {
		server := item.Server()
		if item.Action != mcp.ActionCreate && item.Action != mcp.ActionUpdate {
			continue
		}
		target, ok := agentcfg.Find(item.Agent)
		if !ok {
			continue
		}
		fmt.Printf("\n# %s — add to %s\n", item.AgentName, item.Path)
		for _, warning := range item.Warnings {
			fmt.Printf("# ⚠ %s\n", warning)
		}
```

#### 3. The `errors.Is(err, os.ErrNotExist)` contract added here is only meaningful if `Delete` and `RenameKey` (and the in-memo…

- 位置：`internal/env/manager.go:120-121`
- 优先级：P3

The `errors.Is(err, os.ErrNotExist)` contract added here is only meaningful if `Delete` and `RenameKey` (and the in-memory "not found" path at manager.go:204 and manager.go:513) apply the same wrapping. Right now they still return `fmt.Errorf("variable %s not found in group %s", key, group)` without `%w`, so callers cannot rely on `errors.Is(err, os.ErrNotExist)` uniformly across the Manager API — only `Get` exposes it. Consider extending the wrapping to all "not found" sites in this file for consistency, or document the asymmetry in the package comment.

**现有代码**

```
// 保留哨兵错误（%w），调用方可用 errors.Is 判定"条目缺失"
return "", fmt.Errorf("variable %s not found in group %s: %w", key, group, err)
```

**建议改法**

```
// 保留哨兵错误（%w），调用方可用 errors.Is(err, os.ErrNotExist) 判定"条目缺失"
return "", fmt.Errorf("variable %s not found in group %s: %w", key, group, err)
```

#### 4. The `ResolveLoose` contract doc says it returns a warning instead of an error for unresolvable references, and the Plan …

- 位置：`internal/mcp/export.go:59-61`
- 优先级：P3

The `ResolveLoose` contract doc says it returns a warning instead of an error for unresolvable references, and the Plan comment hints at "reference cycles etc." for hard errors, but the actual boundary (any non-nil `err` from `ResolveLoose` becomes a hard error that poisons the target) is not spelled out. Document the contract on `ResolveLoose` explicitly — e.g., "the callback must return a non-nil error only for unresolvable *structural* conditions (cycles, malformed templates); missing env/text values must come back as `(raw, [warning], nil)`" — so future implementers don't silently turn loose warnings into hard failures.

**现有代码**

```
// ResolveLoose, when set, takes precedence: an unresolvable reference keeps
// its template literal in the written value and returns a warning instead of
// an error (profiles sync across machines before their credentials do).
```

#### 5. With the addition of KindLLMProvider and KindMCPServer, the legacy TestValidateIdentityAcceptsFiveKinds test name now un…

- 位置：`internal/syncschema/schema_test.go`
- 优先级：P3

With the addition of KindLLMProvider and KindMCPServer, the legacy TestValidateIdentityAcceptsFiveKinds test name now understates the count (it covers 5; the two new kinds are covered by TestValidateIdentityAcceptsConfigSourceKinds). Consider renaming to TestValidateIdentityAcceptsLegacyFiveKinds or merging all accepted-kind cases so the test list stays authoritative — non-blocking, current behavior is correct.

**现有代码**

```
func TestValidateIdentityAcceptsFiveKinds(t *testing.T) {
```
