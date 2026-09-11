## Review 总结 · senv-tui-perf

范围：`senv` / `default-branch` `origin/main` → `HEAD`
结论：P0=1 P1=6 P2=15 P3=49
完整文档：`docs/reviews/2026-09-11/senv-tui-perf/review.md`

### P0

- [P0] `unix.Statfs_t.Fstypename` is declared as `[16]int8` on darwin, so `stat.Fstypename[:]` has type `[]int8`. `unix.ByteSli… — `internal/session/runtimefs_darwin.go:12`

### P1

- [P1] Triggering assembleModels on `opts.DefaultReasoning != nil` (even when the pointer is to "") silently rewrites archive m… — `internal/llm/provider.go:348-351`
- [P1] Triggering assembleModels on `opts.DefaultReasoning != nil` when the pointer is to "" still runs the full re-assembly pa… — `internal/llm/provider.go:348-351`
- [P1] Silent selection now prefers the disk "escape hatch" cache whenever its CreatedAt is newer than the primary (tmpfs / XDG… — `internal/session/cache.go:535-541`
- [P1] The update branch only sets `opts.Models` / `opts.CatalogProvider` / `opts.ModelContexts` when models/catalog/contexts/r… — `internal/tui/ai_tab.go:841-860`
- [P1] Clearing model_default_reasoning on an existing provider does not work: `parseDefaultReasoningField("")` returns `(nil, … — `internal/tui/ai_tab.go:1197-1202`
- [P1] The help text for `mcpModeChangedConfirm` promises "esc 取消" (cancel the whole unexport), but this branch silently treats… — `internal/tui/mcp_tab.go:355-361`

### P2

- [P2] `errors.Join(wrapper, err)` causes the joined error's `Error()` to render `wrapper.Error() + "\n" + err.Error()`. For `E… — `cmd/auth.go:58-72`
- [P2] The default branch returns the raw `err.Error()` for `ErrSessionUnverifiable` and `errMultipleSessionCaches`. For `ErrSe… — `cmd/auth.go:74-87`
- [P2] `out == nil` paired with `err != nil` (loadState/collect errors in `AutoPush` always return `nil, err`) is a real failur… — `cmd/autosync.go:114-119`
- [P2] In `postRunAutoPush`, the `out == nil` branch represents a real pre-push failure (lock acquisition / loadState / collect… — `cmd/autosync.go:115-119`
- [P2] `status.Detail` is interpolated verbatim into both the `senv session refresh` error and the `senv session status` Unveri… — `cmd/session.go:226-227`
- [P2] The new "Session cap" line is computed with `session.DefaultMaxLifetime` directly, but the actual renewal ceiling is `Ma… — `cmd/session.go:280-284`
- [P2] In `StateExpired`, the status panel still says "Cache: will be cleared on next use" with a hard-coded "Next: senv sessio… — `cmd/session.go:295-301`
- [P2] `status.Detail` is printed verbatim in the Unverifiable branch of `senv session status`. As noted on the refresh side, `… — `cmd/session.go:310-316`
- [P2] Trim inconsistency in default-level membership check: the emptiness guard uses `strings.TrimSpace(level.Effort) == ""`, … — `internal/llm/codexcatalog.go:183-194`
- [P2] Race window: `collectMu` is released between reading `prevSnap/prevIdent` and writing the new `collectSnap/collectIdent`… — `internal/provider/server_state.go:194-207`
- [P2] withVaultRead acquires an exclusive flock via WithVaultMutation (mutation.go), not a shared read lock. The legacy-group … — `internal/storage/manager.go:478-495`
- [P2] The cache correctness depends on callers holding the vault mutation lock, but `loadRekeyManifestCached` never checks `m.… — `internal/storage/rekey_manifest.go:300-303`
- [P2] `syncStatus()` runs `exporter.Plan(...)` on every up/down keypress via `updateKey`. `Plan` re-reads the vault ledger and… — `internal/tui/mcp_tab.go:294-307`
- [P2] `executeUnexport` does not mirror the `NeedsWrite` guard that `executeExport` and `cmd/mcp_export.go` apply: when the pl… — `internal/tui/mcp_tab.go:774-792`
- [P2] The `env 需要 KEY=VALUE，收到 %q` error embeds the raw env line via `%q`. When this error reaches `mcpFormReopenMsg` and is r… — `internal/tui/mcp_tab.go:1033`

### P3

- [P3] The same `ClassifyAuthCause` switch checking `AuthCauseMultipleCache` / `AuthCauseUnreadable` is duplicated in the non-i… — `cmd/auth.go:239-267`
- [P3] The `ClassifyAuthCause` switch listing `AuthCauseMultipleCache` / `AuthCauseUnreadable` (the "do not prompt, surface cau… — `cmd/auth.go:247-267`
- [P3] Trailing ". %s" produces awkward punctuation when status.Detail is empty (ReasonUnknownType is the case that reaches thi… — `cmd/session.go:226-227`
- [P3] Local variable `cap` shadows the Go built-in function `cap`. The scope is small enough that no real capacity call is hid… — `cmd/session.go:284`
- [P3] The `session` package's `ClassifyAuthCause` (`internal/session/types.go:125-146`) is documented as the single vocabulary… — `cmd/session.go:414-416`
- [P3] `statusNextAction`'s `StateUnverifiable` branch defaults to the destructive `senv session clear --all` for any reason ot… — `cmd/session.go:425-429`
- [P3] `managersSt.End(false)` is only called on the `loadTUIManagers` error path, but the perf log block also has `sourcesSt.E… — `cmd/tui.go:37-44`
- [P3] `loadTUIManagers` returns raw errors from `getManagers`, `getSSHManager`, `getMCPManager`, and `agentHomeDir` without `%… — `cmd/tui.go:57-73`
- [P3] `mcpHome` is reused as `llmHome` a few lines below (`llmHome = mcpHome`). The variable name suggests it's MCP-specific, … — `cmd/tui.go:70-81`
- [P3] Mixed Chinese/English comments on production Go code (`// List lists all environment variables ...（组名是既有审计也使用的非敏感标识）`, `… — `internal/env/manager.go:214-215`
- [P3] `List` calls `listEntries` only for the all-groups path; the `group != ""` branch still wraps errors with `fmt.Errorf("f… — `internal/env/manager.go:240-248`
- [P3] `Snapshot` performs two independent storage reads (`loadEnvVault` then `LoadSettings`) with no shared lock. A concurrent… — `internal/env/manager.go:264-274`
- [P3] `Snapshot` builds `vars := make(map[string]map[string]string, len(all))` and copies every group's full `Variables` map b… — `internal/env/manager.go:282-287`
- [P3] `listGroupsInfo` (the new `ListGroups` body) calls `Snapshot()` solely to obtain `[]GroupInfo`, but `Snapshot` also full… — `internal/env/manager.go:478-481`
- [P3] Unnecessary defensive copy: `codexInputModalities` allocates a new slice for `mods` even though the result is only used … — `internal/llm/codexcatalog.go:147-152`
- [P3] End has no idempotency guard: t.start is never reset and the threshold/state checks happen every call, so invoking End (… — `internal/perflog/perflog.go:53-64`
- [P3] MkdirAll only applies 0o700 when it actually creates the directory; if `~/.log/senv` already exists with looser permissi… — `internal/perflog/perflog.go:100-103`
- [P3] File descriptor leak: the *os.File opened in initFromEnv() is passed to slog.NewJSONHandler but never closed. The JSON h… — `internal/perflog/perflog.go:104-109`
- [P3] Unbounded log growth: perf.log is opened with O_APPEND|O_CREATE and never rotated, truncated, or size-capped. Because th… — `internal/perflog/perflog.go:104-105`
- [P3] Threshold overflow can silently disable filtering: parseThreshold builds the duration via `time.Duration(ms) * time.Mill… — `internal/perflog/perflog.go:112-121`
- [P3] `collectEntries` (lines 211–214) is a thin wrapper around `collectCached` that drops the `reads` count. It has zero call… — `internal/provider/server_state.go:211-214`
- [P3] `resetCollectCache` is the only knob exposed to invalidate the new in-memory snapshot, but the only callers are in `inte… — `internal/provider/server_state.go:498-508`
- [P3] The `slot` parameter on selectNewerCache is never referenced in the function body; it was carried over from the call-sit… — `internal/session/cache.go:564-568`
- [P3] Test gap for confirmed finding #2: there is no test that asserts the disk-hatch warning prints at most once across a `se… — `internal/session/store.go`
- [P3] The fallback path silently overwrites the original `ErrNoSecureSessionStore` cause with the disk-store result. When the … — `internal/session/store.go:76-80`
- [P3] `saveCache` is invoked not only by `StartSession` / `RenewSession` but also on every sliding-window renewal in `Manager.… — `internal/session/store.go:76-80`
- [P3] Test gap: the new `TestSaveCacheDarwinFallsBackToDisk` covers the happy fallback path but does not exercise the case whe… — `internal/session/store.go:91-93`
- [P3] For consistency with the analogous `ListHosts` wrapper in host.go (and `env.List`), consider adding the result count as … — `internal/ssh/manager.go:156-161`
- [P3] ValidateName was previously the first-line guard in LoadEnvGroupWithKey and is now inside loadEnvGroupWithRoot, after wi… — `internal/storage/manager.go:345-355`
- [P3] loadEnvVaultWithKey is all-or-nothing: any single-group failure (corrupt meta, partially-migrated directory, permission … — `internal/storage/manager.go:477-495`
- [P3] The inline seenDir closure duplicates the same root.ReadDir(EnvDirName, name) that loadEnvGroupWithRoot performs immedia… — `internal/storage/manager.go:478-495`
- [P3] The `valid` field on `manifestCacheEntry` is dead weight: the only insertion site (line 332) sets `valid: true`, there i… — `internal/storage/rekey_manifest.go:287-293`
- [P3] `manifestCache` is a package-level `map[string]manifestCacheEntry` with no size cap, no eviction, and no invalidation on… — `internal/storage/rekey_manifest.go:295-298`
- [P3] The cached `*rekeyManifest` pointer is shared across all callers that hit the same cache key, and `rekeyManifest.Entries… — `internal/storage/rekey_manifest.go:330-339`
- [P3] `Reload()` now drops the `t.loaded = false` reset, but the field is still consulted in two places: `Init()` returns nil … — `internal/tui/ai_tab.go:156-161`
- [P3] `out.Warnings[0]` only surfaces the first warning. The provider layer can return multiple (e.g., stale catalog warning +… — `internal/tui/ai_tab.go:302-304`
- [P3] `providerErrorField` matches `"output"` before `"model"`, so an error message like "model output limit must be positive"… — `internal/tui/ai_tab.go:929-938`
- [P3] The English doc comment above Reload() still claims "drops cached data and reloads", but the implementation now retains … — `internal/tui/config_tab.go:167-172`
- [P3] The new `envSnap` field on `tuiGetter` is optional, but the two callers in the package diverge: `deref.go:57` populates … — `internal/tui/deref.go:17-21`
- [P3] The `snapErr` from `envSnapshot` is silently discarded, causing graceful degradation to the per-key `envMgr.Get` path. W… — `internal/tui/deref.go:53-56`
- [P3] After `mcpFormReopenMsg` is handled, the form's `index` stays at whatever field the user last touched, but the error is … — `internal/tui/mcp_tab.go:233-243`
- [P3] `updateMode` for `mcpModeDelete` and `mcpModePlan` cancels the dialog on any key other than the listed ones (`y`/`enter`… — `internal/tui/mcp_tab.go:347-354`
- [P3] `t.changedItems()` is invoked twice on the same keypress in this branch (lines 356 and 359). Each call re-filters `unexp… — `internal/tui/mcp_tab.go:355-361`
- [P3] `updateMode` `mcpModePlan` `default` branch unconditionally cancels and toasts `已取消` for any non-`F`/`y`/`enter` keypres… — `internal/tui/mcp_tab.go:368-383`
- [P3] `mcpExportAuditTarget` and `mcpUnexportAuditTarget` both extract the alias from `plan.Items[0].Alias`, but `ExecuteUnexp… — `internal/tui/mcp_tab.go:830-844`
- [P3] The new MCP loop variable `s` shadows the method's receiver `s *searchTab`. The rest of `gather()` uses distinct inner n… — `internal/tui/search.go:163-173`
- [P3] Section comment is in Chinese while every other comment in this file is English — translate for consistency. — `internal/tui/search.go:163`
- [P3] Section comment is in Chinese while every other comment in this file is English — translate for consistency with the sur… — `internal/tui/search.go:163`
- [P3] The loop variable `s` shadows the outer receiver `s *searchTab`. Other iterations in this function use distinct names (`… — `internal/tui/search.go:166-171`
