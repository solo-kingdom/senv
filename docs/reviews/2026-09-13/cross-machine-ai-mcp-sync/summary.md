## Review 总结 · cross-machine-ai-mcp-sync

范围：`senv` / `default-branch` `origin/main` → `HEAD`
结论：P0=0 P1=2 P2=2 P3=5
完整文档：`docs/reviews/2026-09-13/cross-machine-ai-mcp-sync/review.md`

### P1

- [P1] Warnings are aggregated once per alias (line 124-137) and then the same slice reference is assigned to every (target, al… — `internal/mcp/export.go:124-137`
- [P1] When `ResolveLoose` returns multiple unresolved references for a single field, the current `fmt.Sprintf("...: %s", warn)… — `internal/mcp/export.go:362-364`

### P2

- [P2] Line 121 wraps the raw `securefs.PathError` (with `Path: "env/<group>/<key>.enc"`), so the user-visible error becomes `v… — `internal/env/manager.go:119-122`
- [P2] executeTarget re-resolves entries (lines 295, 329) and discards the freshly-generated warnings, so the final `item.Warni… — `internal/mcp/export.go:295-301`

### P3

- [P3] The `combinedGetter` instantiation pattern is now duplicated between `cmd/text.go:351-352` and the new code in `mcpExpor… — `cmd/mcp_export.go:82-85`
- [P3] Minor UX consistency: between `printExportPlan` (which surfaces `[未解析引用]` markers) and `printExportSnippets` (the `--pri… — `cmd/mcp_export.go:259-269`
- [P3] The `errors.Is(err, os.ErrNotExist)` contract added here is only meaningful if `Delete` and `RenameKey` (and the in-memo… — `internal/env/manager.go:120-121`
- [P3] The `ResolveLoose` contract doc says it returns a warning instead of an error for unresolvable references, and the Plan … — `internal/mcp/export.go:59-61`
- [P3] With the addition of KindLLMProvider and KindMCPServer, the legacy TestValidateIdentityAcceptsFiveKinds test name now un… — `internal/syncschema/schema_test.go`
