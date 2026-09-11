---
repo: senv
mode: default-branch
date: 2026-09-11
change_name: senv-tui-perf
branch: tui-perf
from: origin/main
to: HEAD
mr: 
ocr_session: 421ac16e-c5d1-409c-87e7-391612995592
ocr_status: partial
---

# Code Review · senv-tui-perf

## Meta

- 仓库：`senv`
- 范围：`default-branch` `origin/main` → `HEAD`
- 分支：`tui-perf`（默认 `main`）
- OCR：files=59 comments=71 elapsed=24m15s session=`421ac16e-c5d1-409c-87e7-391612995592`
- OCR message：Review partially complete: 71 finding(s); 4 of 59 selected item(s) failed.

## 统计

P0=1 / P1=6 / P2=15 / P3=49

## Findings（完整）

### P0

#### 1. `unix.Statfs_t.Fstypename` is declared as `[16]int8` on darwin, so `stat.Fstypename[:]` has type `[]int8`. `unix.ByteSli…

- 位置：`internal/session/runtimefs_darwin.go:12`
- 优先级：P0

`unix.Statfs_t.Fstypename` is declared as `[16]int8` on darwin, so `stat.Fstypename[:]` has type `[]int8`. `unix.ByteSliceToString` takes `[]byte` (see `golang.org/x/sys/unix` v0.41.0, e.g. `unsafe.String(unsafe.SliceData(b), len(b))`), and Go does not implicitly convert between `[]int8` and `[]byte`. As written the file will not compile under `GOOS=darwin` (and `make build-darwin` will fail), which is exactly the platform this code exists to support. Convert explicitly, e.g. `unix.ByteSliceToString([]byte(stat.Fstypename[:]))`, or use `string(stat.Fstypename[:])` and let `isMemoryBackedFSType` already trim a trailing NUL. Also: the linux probe compares a kernel-defined magic number (`TMPFS_MAGIC`/`RAMFS_MAGIC`) for positive identification, while the darwin probe trusts a textual name returned by the kernel — fine per design, but be aware it relies on the kernel filling `f_fstypename` correctly. No concurrency or error-handling issues spotted.

**现有代码**

```
	if isMemoryBackedFSType(unix.ByteSliceToString(stat.Fstypename[:])) {
```

**建议改法**

```
	var name [16]byte
	copy(name[:], stat.Fstypename[:])
	if isMemoryBackedFSType(unix.ByteSliceToString(name[:])) {
```

### P1

#### 1. Triggering assembleModels on `opts.DefaultReasoning != nil` (even when the pointer is to "") silently rewrites archive m…

- 位置：`internal/llm/provider.go:348-351`
- 优先级：P1

Triggering assembleModels on `opts.DefaultReasoning != nil` (even when the pointer is to "") silently rewrites archive metadata from the current catalog and applies catalogDefaultReasoning as fallback, contradicting the field comment "空字符串表示不填充". Either skip the trigger when the trimmed pointer value is empty, or skip the catalogDefaultReasoning fallback in the fill loop when DefaultReasoning is explicitly empty. Also note this re-reads all catalog metadata onto the archive, which can be surprising for a "do nothing" edit.

**现有代码**

```
if opts.Models != nil || opts.CatalogProvider != nil || opts.ModelContexts != nil ||
		opts.ModelOutputs != nil || opts.ModelReasoning != nil ||
		opts.ModelDefaultReasoning != nil || opts.DefaultReasoning != nil ||
		opts.ModelModalities != nil {
```

#### 2. Triggering assembleModels on `opts.DefaultReasoning != nil` when the pointer is to "" still runs the full re-assembly pa…

- 位置：`internal/llm/provider.go:348-351`
- 优先级：P1

Triggering assembleModels on `opts.DefaultReasoning != nil` when the pointer is to "" still runs the full re-assembly path. Since `collectionDefault = strings.TrimSpace(opts.DefaultReasoning)` becomes "" in assembleModels, the catalog fallback at lines 627-634 (the `else if catalogDefaultReasoning[id] != ""` branch) silently fills in catalog-derived default reasoning for any model that has `ReasoningEfforts` but no archived default — directly contradicting the field comment "空字符串表示不填充" and rewriting archive metadata the user did not ask to change. The trigger should also gate on the trimmed pointer value (e.g. only enter the branch when `*opts.DefaultReasoning` is non-empty after TrimSpace), so an explicit `--default-reasoning ""` no longer has the side effect of re-baking catalog defaults into the persisted model info.

**现有代码**

```
	if opts.Models != nil || opts.CatalogProvider != nil || opts.ModelContexts != nil ||
		opts.ModelOutputs != nil || opts.ModelReasoning != nil ||
		opts.ModelDefaultReasoning != nil || opts.DefaultReasoning != nil ||
		opts.ModelModalities != nil {
```

**建议改法**

```
	opts.DefaultReasoning != nil && strings.TrimSpace(*opts.DefaultReasoning) != "") ||
		opts.ModelModalities != nil {
```

#### 3. Silent selection now prefers the disk "escape hatch" cache whenever its CreatedAt is newer than the primary (tmpfs / XDG…

- 位置：`internal/session/cache.go:535-541`
- 优先级：P1

Silent selection now prefers the disk "escape hatch" cache whenever its CreatedAt is newer than the primary (tmpfs / XDG_RUNTIME_DIR) cache. The hatch is the deliberately *less secure* store — it persists the derived session key unencrypted at ~/.cache/senv/session-<slot>.json (0600) — and is only ever populated when tmpfs is unavailable or the user opted into --insecure-cache. Previously the coexistence of two caches for one slot was an actionable hard-fail; now a transient scenario (e.g. tmpfs detection flipped, prior --insecure-cache run, manual write, or an attacker with write access to ~/.cache as the user) silently activates the disk-hatch path with no log or warning, and the boot-ID/salt check in cacheValidity still passes for a forged cache with a current BootID. Consider: (a) recording an audit/event-log entry whenever the hatch wins over a readable primary, and (b) at minimum emitting the existing InsecureCacheWarning to stderr in that branch so the user knows the key was loaded from an unencrypted file.

**现有代码**

```
switch {
		case primary != nil && hatch != nil:
			// Deterministic selection instead of "multiple caches" hard-fail
			// (ADR-0017): prefer the newer cache so an upgrade or an explicit
			// escape hatch does not strand the user. Both entries stay on disk
			// and whichever one is picked must still pass full validation.
			return selectNewerCache(slot, primary, hatch)
```

**建议改法**

```
switch {
		case primary != nil && hatch != nil:
			// Deterministic selection instead of "multiple caches" hard-fail
			// (ADR-0017): prefer the newer cache so an upgrade or an explicit
			// escape hatch does not strand the user. Both entries stay on disk
			// and whichever one is picked must still pass full validation.
			selected, err := selectNewerCache(slot, primary, hatch)
			if err != nil {
				return nil, err
			}
			if selected == hatch {
				// Surface that the key came from the unencrypted disk escape
				// hatch so it is not silently activated by a forged or stale
				// hatch file outracing the in-memory primary.
				fmt.Fprintln(os.Stderr, InsecureCacheWarning)
			}
			return selected, nil
```

#### 4. The update branch only sets `opts.Models` / `opts.CatalogProvider` / `opts.ModelContexts` when models/catalog/contexts/r…

- 位置：`internal/tui/ai_tab.go:841-860`
- 优先级：P1

The update branch only sets `opts.Models` / `opts.CatalogProvider` / `opts.ModelContexts` when models/catalog/contexts/reasoning/defaultReasoning changed, but separately sets `opts.ModelOutputs` / `opts.ModelModalities` when those changed. If only outputs or modalities change on an existing provider, `opts.Models` stays nil and `opts.CatalogProvider` stays nil — that's actually handled correctly by `EditProvider` (it falls back to `existing.Models`), so this is fine. However, when only `modalitiesChanged` is true and `ModelModalities` is empty/nil (e.g., user cleared the field), `EditProvider` skips the re-assembly entirely (provider.go:348), silently preserving old modalities. Same for `ModelOutputs`. This is the same root cause as the default-reasoning clearing bug, but here it's also structurally inconsistent: outputs/modalities have their own diff block that bypasses the bundled "trigger re-assembly" condition.

Suggestion: include `outputsChanged` and `modalitiesChanged` in the same bundled condition that sets `opts.RequireModelMetadata = true`, so that any metadata diff triggers a coherent re-assembly.

**现有代码**

```
if !equalStrings(models, existing.Models) || catalog != existing.CatalogProvider || contextsChanged ||
		reasoningChanged || defaultReasoningChanged {
		opts.Models = models
		opts.CatalogProvider = &catalog
		opts.ModelContexts = modelContexts
		opts.RequireModelMetadata = true
	}
	if outputsChanged {
		opts.ModelOutputs = modelOutputs
	}
	if reasoningChanged {
		opts.ModelReasoning = modelReasoning
	}
	if defaultReasoningChanged {
		opts.ModelDefaultReasoning = modelDefaultReasoning
		opts.DefaultReasoning = &collectionDefault
	}
	if modalitiesChanged {
		opts.ModelModalities = modelModalities
	}
```

#### 5. Clearing model_default_reasoning on an existing provider does not work: `parseDefaultReasoningField("")` returns `(nil, …

- 位置：`internal/tui/ai_tab.go:1197-1202`
- 优先级：P1

Clearing model_default_reasoning on an existing provider does not work: `parseDefaultReasoningField("")` returns `(nil, "", nil)`, so `opts.ModelDefaultReasoning` becomes nil. In `EditProvider` (provider.go:348), the condition `opts.ModelDefaultReasoning != nil` is what triggers re-assembly, so a nil value silently preserves the old default reasoning — the user's intent to clear is lost. Same pattern affects `model_outputs`, `model_contexts`, `model_reasoning`, and `model_modalities` when cleared: their parsed maps become nil and the update becomes a no-op.

Suggestion: either (a) introduce a sentinel (e.g., empty non-nil map) to mean "clear all metadata for this key", with the provider layer honoring it; or (b) detect cleared fields in `doSubmitProvider` and explicitly pass an empty non-nil map / pointer-to-empty-string so the provider layer treats it as a clear.

**现有代码**

```
// parseDefaultReasoningField 接受 per-model `m1=high`（逗号分隔）或集合级单一档位。
func parseDefaultReasoningField(raw string) (map[string]string, string, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return nil, "", nil
	}
```

**建议改法**

```
// parseDefaultReasoningField 接受 per-model `m1=high`（逗号分隔）或集合级单一档位。
func parseDefaultReasoningField(raw string) (map[string]string, string, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		// 非 nil 空 map 让 EditProvider 把现有 default reasoning 视为显式清空，
		// 而非「未提供」。
		return map[string]string{}, "", nil
	}
```

#### 6. The help text for `mcpModeChangedConfirm` promises "esc 取消" (cancel the whole unexport), but this branch silently treats…

- 位置：`internal/tui/mcp_tab.go:355-361`
- 优先级：P1

The help text for `mcpModeChangedConfirm` promises "esc 取消" (cancel the whole unexport), but this branch silently treats every non-"y" key as "skip this item" and continues iterating, finally calling `executeUnexport` at the end. Pressing esc during the per-item confirmation will skip the current item and still execute the unexport on the remaining items. Add an explicit escape that calls `cancelMode()` and returns a warning toast, matching the documented behavior.

**现有代码**

```
case mcpModeChangedConfirm:
		item := t.changedItems()[t.changedIdx]
		t.changedAllowed[item.Agent+"/"+item.Alias] = key == "y"
		t.changedIdx++
		if t.changedIdx < len(t.changedItems()) {
			return t, nil
		}
```

**建议改法**

```
case mcpModeChangedConfirm:
		if key == "esc" {
			t.cancelMode()
			return t, warnToast("已取消")
		}
		item := t.changedItems()[t.changedIdx]
		t.changedAllowed[item.Agent+"/"+item.Alias] = key == "y"
		t.changedIdx++
		if t.changedIdx < len(t.changedItems()) {
			return t, nil
		}
```

### P2

#### 1. `errors.Join(wrapper, err)` causes the joined error's `Error()` to render `wrapper.Error() + "\n" + err.Error()`. For `E…

- 位置：`cmd/auth.go:58-72`
- 优先级：P2

`errors.Join(wrapper, err)` causes the joined error's `Error()` to render `wrapper.Error() + "\n" + err.Error()`. For `ErrSessionUnverifiable` chains whose inner error embeds `XDG_RUNTIME_DIR` (or `os.TempDir()`) from `internal/session/runtimefs.go:38,44`, the path is surfaced via the second half of the joined output even after `causeDetail` is sanitized. Combined with the prior confirmed finding 1, fixing only `causeDetail`'s default branch is insufficient. Replace `errors.Join(wrapper, err)` with a wrapper that exposes the original error via `Unwrap()` (so `errors.Is(returnedErr, session.ErrSessionUnverifiable)` still works), and have `authCauseError.Error()` render only the sanitized detail+action.

**现有代码**

```
// wrapAuthCause converts a session error into the shared cause vocabulary
// while preserving the original session sentinels for errors.Is assertions: the
// returned error wraps BOTH the descriptive authCauseError and the original err.
func wrapAuthCause(err error) error {
	cause, ok := session.ClassifyAuthCause(err)
	if !ok {
		return err
	}
	wrapper := &authCauseError{
		cause:  cause,
		action: causeAction(cause),
		detail: causeDetail(err),
	}
	return errors.Join(wrapper, err)
}
```

**建议改法**

```
// wrapAuthCause converts a session error into the shared cause vocabulary
// while preserving the original session sentinels for errors.Is assertions via
// Unwrap(). The wrapper renders only sanitized text so a path-bearing inner
// error (e.g. ErrSessionUnverifiable wrapping internal/session/runtimefs.go)
// never reaches CLI/MCP output.
func wrapAuthCause(err error) error {
	cause, ok := session.ClassifyAuthCause(err)
	if !ok {
		return err
	}
	return &authCauseError{
		cause:  cause,
		action: causeAction(cause),
		detail: causeDetail(err),
		inner:  err,
	}
}

func (e *authCauseError) Unwrap() error { return e.inner }
```

#### 2. The default branch returns the raw `err.Error()` for `ErrSessionUnverifiable` and `errMultipleSessionCaches`. For `ErrSe…

- 位置：`cmd/auth.go:74-87`
- 优先级：P2

The default branch returns the raw `err.Error()` for `ErrSessionUnverifiable` and `errMultipleSessionCaches`. For `ErrSessionUnverifiable`, the wrapping chain (`internal/session/manager.go:240,247,258,262` → `loadCacheForDataPath` → `runtimefs.go:37-46`) embeds the runtime filesystem path (`XDG_RUNTIME_DIR`, e.g. `/run/user/1000`) and underlying `os.Statfs` errors in the message. That contradicts the "short, secret-free human detail" contract this function advertises, and is not exercised by `TestAuthErrorNoSecretLeak` (which passes the bare `ErrSessionUnverifiable` sentinel rather than a wrapped one). Replace the default with a generic short message per cause (e.g. "environment or cache payload is unreadable" for `ErrSessionUnverifiable`; the static `errMultipleSessionCaches` text is fine) so that filesystem paths, mount details, and inner `os` error strings can never reach the user-facing wrapper.

**现有代码**

```
// causeDetail extracts a short, secret-free human detail for the cause.
func causeDetail(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, session.ErrSessionInvalidated) && errors.Is(err, session.ErrSessionVaultChanged):
		return "cached session belongs to a different vault"
	case errors.Is(err, session.ErrSessionInvalidated):
		return "system rebooted since the session was created"
	case errors.Is(err, session.ErrSessionStaleMetadata), errors.Is(err, session.ErrSessionStaleKey):
		return "metadata no longer matches the cached key"
	default:
		return msg
	}
}
```

**建议改法**

```
// causeDetail extracts a short, secret-free human detail for the cause.
func causeDetail(err error) string {
	switch {
	case errors.Is(err, session.ErrSessionInvalidated) && errors.Is(err, session.ErrSessionVaultChanged):
		return "cached session belongs to a different vault"
	case errors.Is(err, session.ErrSessionInvalidated):
		return "system rebooted since the session was created"
	case errors.Is(err, session.ErrSessionStaleMetadata), errors.Is(err, session.ErrSessionStaleKey):
		return "metadata no longer matches the cached key"
	case errors.Is(err, session.ErrSessionUnverifiable):
		return "environment or cache payload is unreadable"
	case errors.Is(err, errMultipleSessionCaches):
		return "multiple caches share an identical timestamp"
	default:
		// Last-resort fallback for causes ClassifyAuthCause does not cover;
		// never surface err.Error() directly because the underlying session
		// sentinels can wrap filesystem paths and syscall details.
		return "see cause below for the next step"
	}
}
```

#### 3. `out == nil` paired with `err != nil` (loadState/collect errors in `AutoPush` always return `nil, err`) is a real failur…

- 位置：`cmd/autosync.go:114-119`
- 优先级：P2

`out == nil` paired with `err != nil` (loadState/collect errors in `AutoPush` always return `nil, err`) is a real failure, but `End(true)` mislabels it as success in the perf log. This skews performance metrics and hides failures. The condition should distinguish "no work to do" (err==nil && out.Skip in {Clean,Locked,Ran}) from "operation failed before producing an outcome" (err!=nil && out==nil). Compare `cmd/tui.go` line 172 which uses the cleaner `err == nil && out != nil && out.Skip == provider.AutoSyncRan` pattern.

**现有代码**

```
	out, err := sp.AutoPush(ctx, autoSyncPushBudget)
	if err == nil || out == nil || out.Skip == provider.AutoSyncSkipClean || out.Skip == provider.AutoSyncSkipLocked {
		st.End(true)
		return
	}
	st.End(false)
```

**建议改法**

```
	out, err := sp.AutoPush(ctx, autoSyncPushBudget)
	if err == nil && out != nil {
		st.End(true)
		return
	}
	st.End(false)
```

#### 4. In `postRunAutoPush`, the `out == nil` branch represents a real pre-push failure (lock acquisition / loadState / collect…

- 位置：`cmd/autosync.go:115-119`
- 优先级：P2

In `postRunAutoPush`, the `out == nil` branch represents a real pre-push failure (lock acquisition / loadState / collect error in `AutoPush` always returns `nil, err`). Recording it as `End(true)` mislabels a failure as success in the perf log and is inconsistent with the autoPull branch, which correctly uses `End(false)` for `err != nil`. Either remove `out == nil` from the success-style condition or restructure so the failure path runs `End(false)`.

**现有代码**

```
if err == nil || out == nil || out.Skip == provider.AutoSyncSkipClean || out.Skip == provider.AutoSyncSkipLocked {
		st.End(true)
		return
	}
	st.End(false)
```

#### 5. `status.Detail` is interpolated verbatim into both the `senv session refresh` error and the `senv session status` Unveri…

- 位置：`cmd/session.go:226-227`
- 优先级：P2

`status.Detail` is interpolated verbatim into both the `senv session refresh` error and the `senv session status` Unverifiable panel. `DescribeCache` populates it from raw errors returned by `loadCacheForDataPath`, `cacheValidity`, and key-decode (e.g. `cannot inspect "/run/user/1000": statfs: ...`, `failed to unmarshal cache: ...`). This can leak filesystem paths and internal runtime context to whatever surface invokes the command (CI logs, screen recordings, terminal scrollback). Either drop the trailing `status.Detail` from the error message (the next-action line is already sufficient) or constrain the panel to a short, pre-known string and log the full error only via the audit logger.

**现有代码**

```
return fmt.Errorf("cannot refresh: session is unverifiable (%s); cache retained; next: %s. %s",
				sessionReasonText(status.Reason), statusNextAction(status), status.Detail)
```

**建议改法**

```
return fmt.Errorf("cannot refresh: session is unverifiable (%s); cache retained; next: %s",
				sessionReasonText(status.Reason), statusNextAction(status))
```

#### 6. The new "Session cap" line is computed with `session.DefaultMaxLifetime` directly, but the actual renewal ceiling is `Ma…

- 位置：`cmd/session.go:280-284`
- 优先级：P2

The new "Session cap" line is computed with `session.DefaultMaxLifetime` directly, but the actual renewal ceiling is `Manager.maxLifetime()` (`internal/session/manager.go:45-59`), which honours `settings.Session.MaxLifetime`. When a user configures a different `max_lifetime`, the displayed cap will not match what `RenewSession` enforces, so the "no surprise" promise of this output is broken. Surface the same value the session manager would use (e.g. add a small public helper on `*Manager` that returns the resolved ceiling, or compute it inline here from the same settings).

**现有代码**

```
if cache.TimeoutSeconds > 0 {
					// The absolute cap is why a continuously-used session still
					// expires: surface it up front so the prompt is never a
					// surprise (ADR-0017).
					cap := cache.CreatedAt.Add(session.DefaultMaxLifetime)
```

**建议改法**

```
if cache.TimeoutSeconds > 0 {
					// The absolute cap is why a continuously-used session still
					// expires: surface it up front so the prompt is never a
					// surprise (ADR-0017).
					ceiling := sessionManager.MaxLifetime()
					cap := cache.CreatedAt.Add(ceiling)
```

#### 7. In `StateExpired`, the status panel still says "Cache: will be cleared on next use" with a hard-coded "Next: senv sessio…

- 位置：`cmd/session.go:295-301`
- 优先级：P2

In `StateExpired`, the status panel still says "Cache: will be cleared on next use" with a hard-coded "Next: senv session start", while the sibling `StateInvalidated` case was migrated to "Cache: retained" + `statusNextAction(status)`. But `DescribeCache()` already returns `Retained: true` for `StateExpired` (`internal/session/manager.go:372`), so this branch both contradicts the package contract and skips the new helper. Update to `Cache: retained` / `fmt.Printf("Next: %s\n", statusNextAction(status))` for parity with `StateInvalidated`.

**现有代码**

```
case session.StateExpired:
			fmt.Println("Session: Expired")
			printSessionIdentity(cache)
			fmt.Println("Reason: internal timeout elapsed")
			fmt.Println("Cache: will be cleared on next use")
			fmt.Println("Next: senv session start")
			return nil
```

**建议改法**

```
case session.StateExpired:
			fmt.Println("Session: Expired")
			printSessionIdentity(cache)
			fmt.Printf("Reason: %s\n", sessionReasonText(status.Reason))
			fmt.Println("Cache: retained")
			fmt.Printf("Next: %s\n", statusNextAction(status))
			return nil
```

#### 8. `status.Detail` is printed verbatim in the Unverifiable branch of `senv session status`. As noted on the refresh side, `…

- 位置：`cmd/session.go:310-316`
- 优先级：P2

`status.Detail` is printed verbatim in the Unverifiable branch of `senv session status`. As noted on the refresh side, `Detail` can carry filesystem paths and internal runtime errors (`loadCacheForDataPath` -> `loadCacheAt` -> `readLocation` wraps include the inspected root path, JSON unmarshal errors, etc.). Replace with a stable, low-disclosure description (e.g. "run `senv session status --debug` for details") or omit the line entirely; log the raw error via the audit logger.

**现有代码**

```
fmt.Println("Session: Unverifiable")
			printSessionIdentity(cache)
			fmt.Printf("Reason: %s\n", sessionReasonText(status.Reason))
			if status.Detail != "" {
				fmt.Printf("Detail: %s\n", status.Detail)
			}
			fmt.Println("Cache: retained (not deleted)")
```

**建议改法**

```
fmt.Println("Session: Unverifiable")
			printSessionIdentity(cache)
			fmt.Printf("Reason: %s\n", sessionReasonText(status.Reason))
			fmt.Println("Cache: retained (not deleted)")
```

#### 9. Trim inconsistency in default-level membership check: the emptiness guard uses `strings.TrimSpace(level.Effort) == ""`, …

- 位置：`internal/llm/codexcatalog.go:183-194`
- 优先级：P2

Trim inconsistency in default-level membership check: the emptiness guard uses `strings.TrimSpace(level.Effort) == ""`, but the membership comparison `level.Effort == entry.DefaultReasoningLevel` is untrimmed on both sides. If the source metadata (`LoadModelMetadata` in modelmeta.go) carries surrounding whitespace (e.g. `" high"`) for both `ReasoningEfforts` and `DefaultReasoning`, the catalog stores those raw strings, the validator treats them as non-empty, and the equality check matches — so an invalid level name like `" high"` slips through senv's validator and is written into the codex catalog, where codex will reject it downstream. Conversely, if one side happens to be trimmed and the other is not (e.g. default from `--model-default-reasoning` which is `strings.TrimSpace`'d in provider.go but reasoning efforts came from a raw upstream cache with whitespace), the comparison silently fails even though semantically they refer to the same level. The builder should store canonical (trimmed) forms, or the validator should compare trimmed values.

```suggestion_code
        defaultLevel := strings.TrimSpace(entry.DefaultReasoningLevel)
        defaultOK := false
        for _, level := range entry.SupportedReasoningLevels {
            effort := strings.TrimSpace(level.Effort)
            if effort == "" {
                return fmt.Errorf("%s (%s): supported_reasoning_levels has an empty effort", where, entry.Slug)
            }
            level.Effort = effort
            if effort == defaultLevel {
                defaultOK = true
            }
        }
        if !defaultOK {
            return fmt.Errorf("%s (%s): default_reasoning_level %q is not in supported_reasoning_levels", where, entry.Slug, entry.DefaultReasoningLevel)
        }
```

**现有代码**

```
defaultOK := false
		for _, level := range entry.SupportedReasoningLevels {
			if strings.TrimSpace(level.Effort) == "" {
				return fmt.Errorf("%s (%s): supported_reasoning_levels has an empty effort", where, entry.Slug)
			}
			if level.Effort == entry.DefaultReasoningLevel {
				defaultOK = true
			}
		}
		if !defaultOK {
			return fmt.Errorf("%s (%s): default_reasoning_level %q is not in supported_reasoning_levels", where, entry.Slug, entry.DefaultReasoningLevel)
		}
```

**建议改法**

```
defaultOK := false
		for _, level := range entry.SupportedReasoningLevels {
			effort := strings.TrimSpace(level.Effort)
			if effort == "" {
				return fmt.Errorf("%s (%s): supported_reasoning_levels has an empty effort", where, entry.Slug)
			}
			level.Effort = effort
			if effort == entry.DefaultReasoningLevel {
				defaultOK = true
			}
		}
		if !defaultOK {
			return fmt.Errorf("%s (%s): default_reasoning_level %q is not in supported_reasoning_levels", where, entry.Slug, entry.DefaultReasoningLevel)
		}
```

#### 10. Race window: `collectMu` is released between reading `prevSnap/prevIdent` and writing the new `collectSnap/collectIdent`…

- 位置：`internal/provider/server_state.go:194-207`
- 优先级：P2

Race window: `collectMu` is released between reading `prevSnap/prevIdent` and writing the new `collectSnap/collectIdent`, and `collectEntriesDiff` performs file I/O during that unlocked window. Two concurrent `collect()` calls (e.g. TUI `tuiSyncSource.Status` → `LocalSyncSnapshot` racing with `pull`/`push`/`AutoPush`) can both grab the same `prevSnap`, run their scans independently, and then race on the final write — the call that locks *last* wins, but it may have observed the *older* filesystem state. When that happens the cache is pinned to a stale `(size, sec, nsec)` and stale ciphertext until the next collect cycle re-detects the change. The most likely user-visible symptom is `collectDirty` reporting "no change" right after a local edit because `current[id].Ciphertext` is the pre-edit blob. Fix options: serialize the whole critical section under `collectMu` (simpler, slow), or assign a monotonically increasing generation per call and only commit if `c.generation` is still ours (avoids clobbering a fresher observation).

**现有代码**

```
func (c *localCache) collectCached() (map[string]Entry, int, error) {
	c.collectMu.Lock()
	prevSnap := c.collectSnap
	prevIdent := c.collectIdent
	c.collectMu.Unlock()

	entries, ident, reads, err := c.collectEntriesDiff(prevSnap, prevIdent)
	if err != nil {
		return nil, reads, err
	}
	c.collectMu.Lock()
	c.collectSnap = entries
	c.collectIdent = ident
	c.collectMu.Unlock()
```

#### 11. withVaultRead acquires an exclusive flock via WithVaultMutation (mutation.go), not a shared read lock. The legacy-group …

- 位置：`internal/storage/manager.go:478-495`
- 优先级：P2

withVaultRead acquires an exclusive flock via WithVaultMutation (mutation.go), not a shared read lock. The legacy-group branch calls MigrateEnvGroupIfNeeded → SaveEnvGroupMetaWithKey + SaveEnvVarWithKey for every legacy file encountered, each opening its own data root. For a vault with N legacy groups and M variables per group, this performs up to N×(M+1) writes under the exclusive flock — blocking all concurrent writers (CLI save, autosync, session, etc.) for the entire bulk-load duration. Consider keeping migration lazy on the per-group LoadEnvGroupWithKey path and only enumerating directories in the bulk loader, or invoking MigrateAllEnvGroups during an explicit pre-commit step.

**现有代码**

```
	for _, name := range groups {
		if seenDir := func() bool {
			_, err := root.ReadDir(EnvDirName, name)
			return err == nil
		}(); seenDir {
			eg, err := m.loadEnvGroupNewFormatFromRoot(root, name, cryptoKey)
			if err != nil {
				return nil, err
			}
			out[name] = eg
			continue
		}
		eg, err := m.loadEnvGroupWithRoot(root, name, cryptoKey)
		if err != nil {
			return nil, err
		}
		out[name] = eg
	}
```

#### 12. The cache correctness depends on callers holding the vault mutation lock, but `loadRekeyManifestCached` never checks `m.…

- 位置：`internal/storage/rekey_manifest.go:300-303`
- 优先级：P2

The cache correctness depends on callers holding the vault mutation lock, but `loadRekeyManifestCached` never checks `m.mutationLocked` and never acquires the lock itself. The comment justifies the design by stating "任意读写都先过 flock", yet the only enforced check is the implicit call graph (this function is only reachable from `recoverRekeyLocked`, which is only called from `WithVaultMutation` at mutation.go:39 and the rekey writer's defer at rekey.go:171). Compare `LoadMetadata` at manager.go:242 and `SaveMetadata` at manager.go:268, which both explicitly gate on `m.mutationLocked`. A future lightweight read path that skips `WithVaultMutation` (the whole point of the tui-perf-load work is to avoid per-read flock) will read stale or mid-mutation manifest data with no diagnostic. Either guard with `if !m.mutationLocked { return m.loadRekeyManifest() }` to enforce the invariant, or accept the coupling as a load-bearing constraint and document it on the function (e.g., "// MUST be called only from recoverRekeyLocked under WithVaultMutation").

**现有代码**

```
func (m *Manager) loadRekeyManifestCached() (*rekeyManifest, error) {
	cacheKey := m.configPath + "\x00" + m.dataPath
	metaStat, metaErr := os.Lstat(filepath.Join(m.configPath, MetadataFile))
	_, manifestErr := os.Lstat(filepath.Join(m.configPath, rekeyManifestFile))
```

**建议改法**

```
func (m *Manager) loadRekeyManifestCached() (*rekeyManifest, error) {
	// loadRekeyManifestCached relies on the caller holding the vault mutation lock:
	// the cache fingerprint (metadata stat + manifest existence) is only safe to
	// compare against on-disk state when no concurrent rekey is mutating it.
	// recoverRekeyLocked satisfies this via WithVaultMutation; reject any other
	// caller so a future lock-free read path can't silently return stale data.
	if !m.mutationLocked {
		return m.loadRekeyManifest()
	}
	cacheKey := m.configPath + "\x00" + m.dataPath
	metaStat, metaErr := os.Lstat(filepath.Join(m.configPath, MetadataFile))
	_, manifestErr := os.Lstat(filepath.Join(m.configPath, rekeyManifestFile))
```

#### 13. `syncStatus()` runs `exporter.Plan(...)` on every up/down keypress via `updateKey`. `Plan` re-reads the vault ledger and…

- 位置：`internal/tui/mcp_tab.go:294-307`
- 优先级：P2

`syncStatus()` runs `exporter.Plan(...)` on every up/down keypress via `updateKey`. `Plan` re-reads the vault ledger and walks every agent; on a tab that should be light browsing, this turns arrow-key navigation into repeated disk + JSON I/O. Debounce syncStatus (e.g. only recompute when selection actually changes or after a short idle window) or skip the Plan call entirely and render a coarse "已导出/未导出" only, deferring detailed state to a manual refresh.

**现有代码**

```
case "up", "k":
		if t.focusLeft && t.serverIndex > 0 {
			t.serverIndex--
			t.syncStatus()
		} else if !t.focusLeft && t.agentIndex > 0 {
			t.agentIndex--
		}
	case "down", "j":
		if t.focusLeft && t.serverIndex < len(t.servers)-1 {
			t.serverIndex++
			t.syncStatus()
		} else if !t.focusLeft && t.agentIndex < len(t.agents)-1 {
			t.agentIndex++
		}
```

**建议改法**

```
case "up", "k":
		if t.focusLeft && t.serverIndex > 0 {
			t.serverIndex--
			t.scheduleStatusRefresh()
		} else if !t.focusLeft && t.agentIndex > 0 {
			t.agentIndex--
		}
	case "down", "j":
		if t.focusLeft && t.serverIndex < len(t.servers)-1 {
			t.serverIndex++
			t.scheduleStatusRefresh()
		} else if !t.focusLeft && t.agentIndex < len(t.agents)-1 {
			t.agentIndex++
		}
```

#### 14. `executeUnexport` does not mirror the `NeedsWrite` guard that `executeExport` and `cmd/mcp_export.go` apply: when the pl…

- 位置：`internal/tui/mcp_tab.go:774-792`
- 优先级：P2

`executeUnexport` does not mirror the `NeedsWrite` guard that `executeExport` and `cmd/mcp_export.go` apply: when the plan contains only `UnexportAbsent` items (alias was never exported, or already removed) the goroutine runs `ExecuteUnexport`, which returns a zero-failure report; the toast shows `已撤回`, and the audit log records `ok=true` for `unexport 0 项`. The user is told an unexport succeeded while nothing happened. Gate the execution on `plan.NeedsWrite()` (and surface `无需写入` like the export branch) before recording audit + emitting the success toast.

**现有代码**

```
func (t *mcpTab) executeUnexport(plan *mcp.UnexportPlan, force bool, alias string, allowed map[string]bool) tea.Cmd {
	if plan == nil {
		return warnToast("没有可执行的计划")
	}
	if allowed == nil {
		allowed = map[string]bool{}
	}
	mgrs := t.mgr
	return func() tea.Msg {
		exporter, err := t.exporter(force)
		if err != nil {
			recordAudit(mgrs, session.AuditOpMCPExport, "mcp:"+alias, false, "unexport 失败")
			return errMsg{err: err}
		}
		report, err := exporter.ExecuteUnexport(plan, func(item mcp.UnexportItem) bool {
			return allowed[item.Agent+"/"+item.Alias]
		})
		ok := err == nil && report.Failures == 0
		recordAudit(mgrs, session.AuditOpMCPExport, mcpUnexportAuditTarget(plan, alias), ok, fmt.Sprintf("unexport %d 项", len(report.Items)))
```

#### 15. The `env 需要 KEY=VALUE，收到 %q` error embeds the raw env line via `%q`. When this error reaches `mcpFormReopenMsg` and is r…

- 位置：`internal/tui/mcp_tab.go:1033`
- 优先级：P2

The `env 需要 KEY=VALUE，收到 %q` error embeds the raw env line via `%q`. When this error reaches `mcpFormReopenMsg` and is rendered into `form.errs`, the user's plaintext env value (which can be a secret) is echoed back to the screen. Include only the key name in the error, not the value, so a mis-typed secret isn't displayed.

**现有代码**

```
return nil, fmt.Errorf("env 需要 KEY=VALUE，收到 %q", line)
```

**建议改法**

```
return nil, fmt.Errorf("env 需要 KEY=VALUE，得到 %s", key)
```

### P3

#### 1. The same `ClassifyAuthCause` switch checking `AuthCauseMultipleCache` / `AuthCauseUnreadable` is duplicated in the non-i…

- 位置：`cmd/auth.go:239-267`
- 优先级：P3

The same `ClassifyAuthCause` switch checking `AuthCauseMultipleCache` / `AuthCauseUnreadable` is duplicated in the non-interactive branch (block A) and the interactive branch (block B). Any future cause added to "do not prompt" must be added to both switches or the interactive path will silently prompt for a password that cannot resolve the cause. Extract a small helper (e.g. `causeBlocksPrompt(cause session.AuthRootCause) bool`) and call it once from each branch, or fold the two blocks into a single early-return after the cause classification.

**现有代码**

```
	// 3. Non-interactive / captured-stdout: refuse to prompt.
	if !stdinIsTerminal() || (activeAuthOpts.requireStdoutTTY && !stdoutIsTerminal()) {
		// A platform-store failure is not the same as "there is no session".
		// Linux tmpfs unavailability must remain actionable instead of being
		// rewritten to the generic hint.
		if errors.Is(err, session.ErrNoSecureSessionStore) {
			return nil, err
		}
		// Root causes that a password could not fix anyway (multiple caches,
		// unreadable environment) stay actionable and are not collapsed into
		// "run session start", which would loop the user back here.
		if cause, ok := session.ClassifyAuthCause(err); ok {
			switch cause {
			case session.AuthCauseMultipleCache, session.AuthCauseUnreadable:
				return nil, wrapAuthCause(err)
			}
		}
		return nil, ErrNeedSession
	}

	// 4. Some root causes cannot be resolved by re-entering the password, so
	// report them instead of prompting and looping. Expired/restarted/metadata
	// replaced do fall through to the prompt, which is the intended recovery.
	if cause, ok := session.ClassifyAuthCause(err); ok {
		switch cause {
		case session.AuthCauseMultipleCache, session.AuthCauseUnreadable:
			return nil, wrapAuthCause(err)
		}
	}
```

**建议改法**

```
	// 3. Root causes that a password cannot fix anyway (multiple caches,
	// unreadable environment) must skip the prompt in both branches;
	// otherwise the user loops back here with the same "no session" error.
	causeBlocksPrompt := false
	if cause, ok := session.ClassifyAuthCause(err); ok {
		switch cause {
		case session.AuthCauseMultipleCache, session.AuthCauseUnreadable:
			causeBlocksPrompt = true
		}
	}

	// 4. Non-interactive / captured-stdout: refuse to prompt.
	if !stdinIsTerminal() || (activeAuthOpts.requireStdoutTTY && !stdoutIsTerminal()) {
		// A platform-store failure is not the same as "there is no session".
		// Linux tmpfs unavailability must remain actionable instead of being
		// rewritten to the generic hint.
		if errors.Is(err, session.ErrNoSecureSessionStore) {
			return nil, err
		}
		if causeBlocksPrompt {
			return nil, wrapAuthCause(err)
		}
		return nil, ErrNeedSession
	}

	// 5. Interactive path: skip the prompt for the same unactionable causes.
	if causeBlocksPrompt {
		return nil, wrapAuthCause(err)
	}
```

#### 2. The `ClassifyAuthCause` switch listing `AuthCauseMultipleCache` / `AuthCauseUnreadable` (the "do not prompt, surface cau…

- 位置：`cmd/auth.go:247-267`
- 优先级：P3

The `ClassifyAuthCause` switch listing `AuthCauseMultipleCache` / `AuthCauseUnreadable` (the "do not prompt, surface cause" set) is duplicated verbatim in the non-interactive branch (above) and the interactive branch. Any future cause added to the "do not prompt" set must be added to both switches or the interactive path will silently prompt and loop. Extract a helper such as `shouldNotPrompt(cause) bool` (or have `ClassifyAuthCause` itself expose the no-prompt classification) and reuse it in both places.

**现有代码**

```
// Root causes that a password could not fix anyway (multiple caches,
	// unreadable environment) stay actionable and are not collapsed into
	// "run session start", which would loop the user back here.
	if cause, ok := session.ClassifyAuthCause(err); ok {
		switch cause {
		case session.AuthCauseMultipleCache, session.AuthCauseUnreadable:
			return nil, wrapAuthCause(err)
		}
	}
	return nil, ErrNeedSession
}

// 4. Some root causes cannot be resolved by re-entering the password, so
// report them instead of prompting and looping. Expired/restarted/metadata
// replaced do fall through to the prompt, which is the intended recovery.
if cause, ok := session.ClassifyAuthCause(err); ok {
	switch cause {
	case session.AuthCauseMultipleCache, session.AuthCauseUnreadable:
		return nil, wrapAuthCause(err)
	}
}
```

**建议改法**

```
// shouldNotPrompt reports whether the given cause must NOT be remedied by a
// password prompt: either a password cannot fix it, or doing so would loop the
// user back to the same error.
func shouldNotPrompt(cause session.AuthRootCause) bool {
	switch cause {
	case session.AuthCauseMultipleCache, session.AuthCauseUnreadable:
		return true
	default:
		return false
	}
}
```

#### 3. Trailing ". %s" produces awkward punctuation when status.Detail is empty (ReasonUnknownType is the case that reaches thi…

- 位置：`cmd/session.go:226-227`
- 优先级：P3

Trailing ". %s" produces awkward punctuation when status.Detail is empty (ReasonUnknownType is the case that reaches this branch with empty Detail): the error reads `... next: senv session clear --all. ` with a dangling period and space. Either append Detail conditionally (e.g. `if status.Detail != "" { ... }`) or drop the trailing separator. Same shape appears for the StateUnverifiable refresh error - keep them aligned.

**现有代码**

```
return fmt.Errorf("cannot refresh: session is unverifiable (%s); cache retained; next: %s. %s",
			sessionReasonText(status.Reason), statusNextAction(status), status.Detail)
```

#### 4. Local variable `cap` shadows the Go built-in function `cap`. The scope is small enough that no real capacity call is hid…

- 位置：`cmd/session.go:284`
- 优先级：P3

Local variable `cap` shadows the Go built-in function `cap`. The scope is small enough that no real capacity call is hidden today, but the same name is then used as the receiver in subsequent `fmt.Printf` calls. Rename to `capAt` or `capTime` to keep the built-in available and silence linters (golangci-lint `predeclared`).

**现有代码**

```
cap := cache.CreatedAt.Add(session.DefaultMaxLifetime)
```

#### 5. The `session` package's `ClassifyAuthCause` (`internal/session/types.go:125-146`) is documented as the single vocabulary…

- 位置：`cmd/session.go:414-416`
- 优先级：P3

The `session` package's `ClassifyAuthCause` (`internal/session/types.go:125-146`) is documented as the single vocabulary shared between command errors and `senv session status`. The new `statusNextAction` helper is now the second source of truth for "what to tell the user next", and it lives in `cmd/session.go` (the very boundary the ADR was meant to keep unified). Consider moving `statusNextAction` into the `session` package next to `ClassifyAuthCause`, or at least add a comment that points readers there, so future state/reason additions only need to be wired in one place.

**现有代码**

```
// statusNextAction renders exactly one deterministic next step for a
// non-reusable session state, so `senv session status` and command errors agree.
func statusNextAction(status session.CacheStatus) string {
```

#### 6. `statusNextAction`'s `StateUnverifiable` branch defaults to the destructive `senv session clear --all` for any reason ot…

- 位置：`cmd/session.go:425-429`
- 优先级：P3

`statusNextAction`'s `StateUnverifiable` branch defaults to the destructive `senv session clear --all` for any reason other than `ReasonUnreadable`. Today only `ReasonUnknownType` reaches that branch (see `cacheValidity` in `internal/session/manager.go:308-330`), but a future reason added to that switch would silently start advising users to wipe every vault's session. Branch on the specific reasons (`ReasonUnknownType` is the only current one) and add an explicit "unknown reason" fallback that points at `senv session status`/docs rather than `--clear --all`, so the destructive suggestion is only ever returned for reasons that truly need it.

**现有代码**

```
case session.StateUnverifiable:
		if status.Reason == session.ReasonUnreadable {
			return "resolve the environment issue above, then retry (`senv session clear --all` discards the cache)"
		}
		return "senv session clear --all"
```

#### 7. `managersSt.End(false)` is only called on the `loadTUIManagers` error path, but the perf log block also has `sourcesSt.E…

- 位置：`cmd/tui.go:37-44`
- 优先级：P3

`managersSt.End(false)` is only called on the `loadTUIManagers` error path, but the perf log block also has `sourcesSt.End(true)` hardcoded for a phase that itself performs no failing operation. This is fine today but consider: if the perflog contract later wants to distinguish "managers loaded" from "all deps loaded", these two phase boundaries don't tell you which subsystem inside `loadTUIManagers` (SSH vs MCP vs HOME vs LLM) caused the slowdown. For diagnosis of the very problem this commit is meant to address (slow TUI load), the inner granularity matters more than the outer two. Optional.

**现有代码**

```
	managersSt := perflog.Start("tui.managers")
	mgrs, auditMgr, err := loadTUIManagers(refreshRequested(cmd))
	if err != nil {
		managersSt.End(false)
		return err
	}
	managersSt.End(true)
	defer auditMgr.Close()
```

**建议改法**

```
	// 外层 perf 标记覆盖整段 manager 装配，便于定位 TUI 启动慢的根因。
	managersSt := perflog.Start("tui.managers")
	mgrs, auditMgr, err := loadTUIManagers(refreshRequested(cmd))
	if err != nil {
		managersSt.End(false)
		return err
	}
	managersSt.End(true)
	defer auditMgr.Close()
```

#### 8. `loadTUIManagers` returns raw errors from `getManagers`, `getSSHManager`, `getMCPManager`, and `agentHomeDir` without `%…

- 位置：`cmd/tui.go:57-73`
- 优先级：P3

`loadTUIManagers` returns raw errors from `getManagers`, `getSSHManager`, `getMCPManager`, and `agentHomeDir` without `%w` wrapping. `resolveAuth` already wraps via `wrapAuthCause` so the auth path gives a friendly message, but failures from `getManagers` (env/text/config), SSH, or HOME resolution are returned verbatim. Adding one-line context (e.g. `fmt.Errorf("init ssh manager: %w", err)`) makes triage faster when multiple subsystems can fail identically. Not blocking — the underlying errors are descriptive enough on their own.

**现有代码**

```
func loadTUIManagers(refresh bool) (tui.Managers, *session.Manager, error) {
	envMgr, textMgr, configMgr, err := getManagers()
	if err != nil {
		return tui.Managers{}, nil, err
	}
	sshMgr, err := getSSHManager()
	if err != nil {
		return tui.Managers{}, nil, err
	}
	mcpMgr, err := getMCPManager()
	if err != nil {
		return tui.Managers{}, nil, err
	}
	mcpHome, err := agentHomeDir()
	if err != nil {
		return tui.Managers{}, nil, err
	}
```

**建议改法**

```
func loadTUIManagers(refresh bool) (tui.Managers, *session.Manager, error) {
	envMgr, textMgr, configMgr, err := getManagers()
	if err != nil {
		return tui.Managers{}, nil, fmt.Errorf("load env/text/config managers: %w", err)
	}
	sshMgr, err := getSSHManager()
	if err != nil {
		return tui.Managers{}, nil, fmt.Errorf("load ssh manager: %w", err)
	}
	mcpMgr, err := getMCPManager()
	if err != nil {
		return tui.Managers{}, nil, fmt.Errorf("load mcp manager: %w", err)
	}
	mcpHome, err := agentHomeDir()
	if err != nil {
		return tui.Managers{}, nil, fmt.Errorf("resolve agent home: %w", err)
	}
```

#### 9. `mcpHome` is reused as `llmHome` a few lines below (`llmHome = mcpHome`). The variable name suggests it's MCP-specific, …

- 位置：`cmd/tui.go:70-81`
- 优先级：P3

`mcpHome` is reused as `llmHome` a few lines below (`llmHome = mcpHome`). The variable name suggests it's MCP-specific, but it's actually the shared user HOME consumed by both LLM and MCP exporters. Consider renaming the local to `home` (matching what `agentHomeDir()` actually returns) or splitting into two locals with an explanatory comment to avoid future readers thinking MCP has its own home directory independent of HOME.

**现有代码**

```
	mcpHome, err := agentHomeDir()
	if err != nil {
		return tui.Managers{}, nil, err
	}
	// LLM 管理器在 vault 可用时注入；不可用时 AI Tab 不注册，其余不受影响。
	llmMgr, llmErr := getAIProviderManager()
	var llmPointer string
	var llmHome string
	if llmErr == nil {
		llmPointer = filepath.Join(getConfigPath(), "agent-pointers.json")
		llmHome = mcpHome
	}
```

**建议改法**

```
	home, err := agentHomeDir()
	if err != nil {
		return tui.Managers{}, nil, err
	}
	// LLM 管理器在 vault 可用时注入；不可用时 AI Tab 不注册，其余不受影响。
	// LLM 与 MCP 共享同一 HOME：MCP 写到 ~/.claude/、LLM switch 也写到 HOME 下的 agent 配置。
	llmMgr, llmErr := getAIProviderManager()
	var llmPointer string
	var llmHome string
	if llmErr == nil {
		llmPointer = filepath.Join(getConfigPath(), "agent-pointers.json")
		llmHome = home
	}
```

#### 10. Mixed Chinese/English comments on production Go code (`// List lists all environment variables ...（组名是既有审计也使用的非敏感标识）`, `…

- 位置：`internal/env/manager.go:214-215`
- 优先级：P3

Mixed Chinese/English comments on production Go code (`// List lists all environment variables ...（组名是既有审计也使用的非敏感标识）`, `// loadEnvVault 一次锁内批量加载...`, `// Snapshot 一次批量加载...`, `// ListGroups 列出全部分组...`). The rest of this file uses English-only doc comments (e.g. `// Export exports environment variables from active groups`). Mixed-language comments complicate grep-based audits and translation tooling, and break `gofmt -d` consistency expectations. Recommendation: keep doc comments in English; relegate Chinese to the linked ADR if needed.

**现有代码**

```
// List lists all environment variables in a group (or all groups if group is
// empty)，附耗时日志（组名是既有审计也使用的非敏感标识）。
```

**建议改法**

```
// List lists all environment variables in a group (or all groups if group is empty), with timing logged via perflog.
```

#### 11. `List` calls `listEntries` only for the all-groups path; the `group != ""` branch still wraps errors with `fmt.Errorf("f…

- 位置：`internal/env/manager.go:240-248`
- 优先级：P3

`List` calls `listEntries` only for the all-groups path; the `group != ""` branch still wraps errors with `fmt.Errorf("failed to load group %s: %w", ...)`. The all-groups branch now returns the unwrapped error from `loadEnvVault()` (`"list env groups: %w"` or `"invalid historical env group %q: %w"` from storage). The two adjacent wrapped variants in `List`/`ListGroups` previously provided a consistent envelope; callers that surfaced these messages (e.g. CLI/TUI error renderers) will now see different text depending on which path produced the error. Consider wrapping once at this seam for parity with the 4 sibling sites (AddGroup/ActivateGroup/DeactivateGroup/DeleteGroup lines 365/397/545/625) that still use `fmt.Errorf("failed to list groups: %w", err)`.

**现有代码**

```
} else {
		all, err := m.loadEnvVault()
		if err != nil {
			return nil, err
		}
		for g, eg := range all {
			result[g] = eg.Variables
		}
	}
```

**建议改法**

```
} else {
		all, err := m.loadEnvVault()
		if err != nil {
			return nil, fmt.Errorf("failed to list groups: %w", err)
		}
		for g, eg := range all {
			result[g] = eg.Variables
		}
	}
```

#### 12. `Snapshot` performs two independent storage reads (`loadEnvVault` then `LoadSettings`) with no shared lock. A concurrent…

- 位置：`internal/env/manager.go:264-274`
- 优先级：P3

`Snapshot` performs two independent storage reads (`loadEnvVault` then `LoadSettings`) with no shared lock. A concurrent `ActivateGroup`/`SetDefaultGroup` between the two reads can produce `vars` and `[]GroupInfo` that disagree on which groups are `IsActive`/`IsDefault` for the same frame. The previous `ListGroups` had the same TOCTOU, but `Snapshot` is now the single TUI data source for env/search/deref/AI cred-ref collection, so any inconsistency is more visible. This is a low-severity design note: document the snapshot semantics as "best-effort point-in-time, not atomic" or expose a single storage-side snapshot API.

**现有代码**

```
st := perflog.Start("env.snapshot")
	all, err := m.loadEnvVault()
	if err != nil {
		st.End(false)
		return nil, nil, err
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		st.End(false)
		return nil, nil, err
	}
```

**建议改法**

```
// Note: vars and settings are read under two separate locks. A concurrent
	// ActivateGroup/SetDefaultGroup can produce vars/gis with transient
	// disagreement on IsActive/IsDefault. This is acceptable for display-only
	// consumers (TUI) but is not an atomic point-in-time snapshot.
	st := perflog.Start("env.snapshot")
	all, err := m.loadEnvVault()
	if err != nil {
		st.End(false)
		return nil, nil, err
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		st.End(false)
		return nil, nil, err
	}
```

#### 13. `Snapshot` builds `vars := make(map[string]map[string]string, len(all))` and copies every group's full `Variables` map b…

- 位置：`internal/env/manager.go:282-287`
- 优先级：P3

`Snapshot` builds `vars := make(map[string]map[string]string, len(all))` and copies every group's full `Variables` map by reference, only to discard `vars` entirely in the `ListGroups` -> `listGroupsInfo` path. The `vars` allocation is only useful for the TUI snapshot consumer (`internal/tui/snapshot.go`), which already returns both views in one call. If `Snapshot` is only consumed for `[]GroupInfo` in `ListGroups`, the variable map is pure waste. This is a structural duplicate of the `listGroupsInfo` inefficiency noted above; consolidating `ListGroups` to call `loadEnvVault()` directly (without the `vars` map) removes the wasted allocation and the indirect round-trip through `Snapshot`.

**现有代码**

```
vars := make(map[string]map[string]string, len(all))
	gis := make([]GroupInfo, 0, len(all))
	for _, name := range names {
		eg := all[name]
		vars[name] = eg.Variables
		isActive := name == settings.DefaultGroup
```

**建议改法**

```
gis := make([]GroupInfo, 0, len(all))
	for _, name := range names {
		eg := all[name]
		isActive := name == settings.DefaultGroup
```

#### 14. `listGroupsInfo` (the new `ListGroups` body) calls `Snapshot()` solely to obtain `[]GroupInfo`, but `Snapshot` also full…

- 位置：`internal/env/manager.go:478-481`
- 优先级：P3

`listGroupsInfo` (the new `ListGroups` body) calls `Snapshot()` solely to obtain `[]GroupInfo`, but `Snapshot` also fully decrypts and materializes a `vars` map for every group just to compute `VarCount = len(eg.Variables)`. For vaults with large variables maps, this regresses `ListGroups` callers — `internal/tui/search.go:113` and `internal/tui/ai_tab.go:205` are existing list-only call sites. Two cheaper paths exist: (a) read group metadata files only (count comes from a per-group index without decrypting variable values), or (b) have `ListGroups` build `gis` directly from the `LoadEnvVaultWithKey` result without constructing the `vars` map. The current code does the maximum work for callers that want the minimum.

**现有代码**

```
func (m *Manager) listGroupsInfo() ([]GroupInfo, error) {
	_, gis, err := m.Snapshot()
	return gis, err
}
```

**建议改法**

```
func (m *Manager) listGroupsInfo() ([]GroupInfo, error) {
	all, err := m.loadEnvVault()
	if err != nil {
		return nil, err
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	gis := make([]GroupInfo, 0, len(all))
	for _, name := range names {
		eg := all[name]
		isActive := name == settings.DefaultGroup
		if !isActive {
			for _, g := range settings.ActiveGroups {
				if g == name {
					isActive = true
					break
				}
			}
		}
		gis = append(gis, GroupInfo{
			Name:      name,
			IsActive:  isActive,
			VarCount:  len(eg.Variables),
			IsDefault: name == settings.DefaultGroup,
		})
	}
	return gis, nil
}
```

#### 15. Unnecessary defensive copy: `codexInputModalities` allocates a new slice for `mods` even though the result is only used …

- 位置：`internal/llm/codexcatalog.go:147-152`
- 优先级：P3

Unnecessary defensive copy: `codexInputModalities` allocates a new slice for `mods` even though the result is only used to populate `entry.InputModalities`, which is then immediately serialized to JSON inside `buildCodexCatalog`. The `meta.InputModalities` source is also already canonicalized upstream by `normalizeInputModalities` (lowercased, trimmed, deduped) in provider.go. Returning the input slice directly is safe and matches the JSON-output lifetime.

```suggestion_code
func codexInputModalities(mods []string) []string {
    if len(mods) == 0 {
        return []string{"text"}
    }
    return mods
}
```

**现有代码**

```
func codexInputModalities(mods []string) []string {
	if len(mods) == 0 {
		return []string{"text"}
	}
	return append([]string(nil), mods...)
}
```

**建议改法**

```
func codexInputModalities(mods []string) []string {
	if len(mods) == 0 {
		return []string{"text"}
	}
	return mods
}
```

#### 16. End has no idempotency guard: t.start is never reset and the threshold/state checks happen every call, so invoking End (…

- 位置：`internal/perflog/perflog.go:53-64`
- 优先级：P3

End has no idempotency guard: t.start is never reset and the threshold/state checks happen every call, so invoking End (or EndErr) twice on the same Timer produces two JSON lines describing the same stage, with the second one's `duration_ms` continuing to grow after the first one. The package doc says "方法可安全用于 defer", which implies single-call use, but nothing enforces that — a `defer st.End(...)` plus an early `st.End(...)` in an error branch, or a misuse, will silently duplicate the entry. Consider marking the Timer as ended (e.g. via `sync.Once` or a `done` flag set under a mutex) so subsequent End/EndErr calls become no-ops.

**现有代码**

```
func (t *Timer) End(ok bool) {
	ensureInit()
	if logger == nil {
		return
	}
	d := time.Since(t.start)
	if d < threshold {
		return
	}
	logger.Info(t.stage,
		append([]any{"duration_ms", d.Milliseconds(), "ok", ok}, t.attrs...)...)
}
```

**建议改法**

```
type Timer struct {
	done      sync.Once
	stage     string
	start     time.Time
	attrs     []any
}

func (t *Timer) End(ok bool) {
	t.done.Do(func() {
		ensureInit()
		if logger == nil {
			return
		}
		d := time.Since(t.start)
		if d < threshold {
			return
		}
		logger.Info(t.stage,
			append([]any{"duration_ms", d.Milliseconds(), "ok", ok}, t.attrs...)...)
	})
}
```

#### 17. MkdirAll only applies 0o700 when it actually creates the directory; if `~/.log/senv` already exists with looser permissi…

- 位置：`internal/perflog/perflog.go:100-103`
- 优先级：P3

MkdirAll only applies 0o700 when it actually creates the directory; if `~/.log/senv` already exists with looser permissions (e.g. from an earlier version of the tool or a manual `mkdir`), the permission is never tightened. The contained perf.log is still opened with 0o600 so file content stays protected, but the directory listing (and the mere existence of perf.log) would still be visible to other local users on a shared host. Consider an explicit `os.Chmod(dir, 0o700)` after MkdirAll succeeds, or at minimum a stat+chmod path, so existing dirs are normalized to the intended mode.

**现有代码**

```
dir := filepath.Join(home, ".log", "senv")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
```

**建议改法**

```
dir := filepath.Join(home, ".log", "senv")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	// MkdirAll leaves existing directories' mode untouched; tighten if needed.
	if info, err := os.Stat(dir); err == nil {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			_ = os.Chmod(dir, 0o700)
		}
	}
```

#### 18. File descriptor leak: the *os.File opened in initFromEnv() is passed to slog.NewJSONHandler but never closed. The JSON h…

- 位置：`internal/perflog/perflog.go:104-109`
- 优先级：P3

File descriptor leak: the *os.File opened in initFromEnv() is passed to slog.NewJSONHandler but never closed. The JSON handler does not take ownership of the file, and the package never calls f.Close() (verified by code search). For a long-running TUI session this leaks one FD for the entire process lifetime, and because the same FD is reused on every init, future runs cannot truncate/rotate the file at startup. Consider either (a) wrapping the handler in a closer that holds *os.File and exposes a Close(), or (b) keeping the *os.File as a package-level field and closing it on a process-exit hook / re-init.

**现有代码**

```
+	f, err := os.OpenFile(filepath.Join(dir, "perf.log"),
+		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
+	if err != nil {
+		return
+	}
+	logger = slog.New(slog.NewJSONHandler(f, nil))
```

#### 19. Unbounded log growth: perf.log is opened with O_APPEND|O_CREATE and never rotated, truncated, or size-capped. Because th…

- 位置：`internal/perflog/perflog.go:104-105`
- 优先级：P3

Unbounded log growth: perf.log is opened with O_APPEND|O_CREATE and never rotated, truncated, or size-capped. Because the FD is also never closed (see related finding), a long-running TUI can silently consume user disk space. Even for an internal best-effort logger, consider a startup truncation strategy (e.g. truncate to 0 if size > N MB) or a rotation hook so a single misconfigured threshold cannot fill the disk.

**现有代码**

```
+	f, err := os.OpenFile(filepath.Join(dir, "perf.log"),
+		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
```

#### 20. Threshold overflow can silently disable filtering: parseThreshold builds the duration via `time.Duration(ms) * time.Mill…

- 位置：`internal/perflog/perflog.go:112-121`
- 优先级：P3

Threshold overflow can silently disable filtering: parseThreshold builds the duration via `time.Duration(ms) * time.Millisecond` and only validates `err == nil` and `ms <= 0`. On a 64-bit `int`, a sufficiently large SENV_PERF_THRESHOLD (e.g. near math.MaxInt64) overflows the multiplication into a tiny or negative duration, causing every End() call to exceed `d < threshold` and triggering a write-amplification storm against perf.log. Add an explicit upper bound (e.g. `ms > 60_000` → fall back to default) and/or use `time.Millisecond * time.Duration(ms)` with an overflow check.

**现有代码**

```
func parseThreshold(v string) time.Duration {
	if v == "" {
		return DefaultThreshold
	}
	ms, err := strconv.Atoi(v)
	if err != nil || ms <= 0 {
		return DefaultThreshold
	}
	return time.Duration(ms) * time.Millisecond
}
```

#### 21. `collectEntries` (lines 211–214) is a thin wrapper around `collectCached` that drops the `reads` count. It has zero call…

- 位置：`internal/provider/server_state.go:211-214`
- 优先级：P3

`collectEntries` (lines 211–214) is a thin wrapper around `collectCached` that drops the `reads` count. It has zero call sites in production or tests (verified via search across the whole repo). Either delete it or wire it up — leaving an unreferenced helper makes future readers wonder which entry point they should use.

**现有代码**

```
func (c *localCache) collectEntries() (map[string]Entry, error) {
	entries, _, err := c.collectCached()
	return entries, err
}
```

#### 22. `resetCollectCache` is the only knob exposed to invalidate the new in-memory snapshot, but the only callers are in `inte…

- 位置：`internal/provider/server_state.go:498-508`
- 优先级：P3

`resetCollectCache` is the only knob exposed to invalidate the new in-memory snapshot, but the only callers are in `internal/provider/collect_incremental_test.go` (lines 102 and 139). None of the production write paths (`mutate`, `apply`, `applyRemoteOpts`, `saveStateOpts`, `writeMetadata`) call it.

The mtime+size check inside `collectEntriesDiff` will self-correct when files are written via `AtomicWrite` (which creates a new inode and updates mtime), so the common sync path is fine. But the cache becomes a latent correctness hazard if any of these happen:

- A future code path writes to `dataPath`/`configPath` directly (e.g., a new test helper or a recovery routine) without going through `mutate`/`AtomicWrite`.
- The filesystem has 1-second mtime resolution and `AtomicWrite` is called twice within the same second with the same size (rare but possible on ext4 with `strictatime` mount options or NFS).
- A second `senv` process touches the same vault directory (multi-CLI scenario the docstring at `server.go:54-66` already contemplates for `clearLocalSession`).

Recommendation: invalidate the snapshot from the single bottleneck that covers every write — call `c.resetCollectCache()` immediately before `return nil` at the end of `mutate` (line 507). The next `collect()` will then do a full re-stat pass instead of trusting the stale `collectSnap`/`collectIdent`. The performance cost is bounded (the diff path is still the hot path when nothing changed) and the safety gain is concrete.

**现有代码**

```
func (c *localCache) mutate(fn func(*cacheTransaction) error) error {
	tx := newCacheTransaction(c)
	defer tx.close()
	if err := fn(tx); err != nil {
		if rollbackErr := tx.rollback(); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("cache rollback failure: %w", rollbackErr))
		}
		return err
	}
	return nil
}
```

#### 23. The `slot` parameter on selectNewerCache is never referenced in the function body; it was carried over from the call-sit…

- 位置：`internal/session/cache.go:564-568`
- 优先级：P3

The `slot` parameter on selectNewerCache is never referenced in the function body; it was carried over from the call-site shape but is dead. Drop it to keep the helper's signature honest about what it actually needs.

**现有代码**

```
// selectNewerCache resolves two readable caches for one slot. The newer
// created_at wins; an exact tie is ambiguous and reported as an actionable
// error. Neither cache is deleted here: the ignored one may be the only
// recovery key for another vault slot.
func selectNewerCache(slot string, primary, hatch *SessionCache) (*SessionCache, error) {
```

**建议改法**

```
// selectNewerCache resolves two readable caches for one slot. The newer
// created_at wins; an exact tie is ambiguous and reported as an actionable
// error. Neither cache is deleted here: the ignored one may be the only
// recovery key for another vault slot.
func selectNewerCache(primary, hatch *SessionCache) (*SessionCache, error) {
```

#### 24. Test gap for confirmed finding #2: there is no test that asserts the disk-hatch warning prints at most once across a `se…

- 位置：`internal/session/store.go`
- 优先级：P3

Test gap for confirmed finding #2: there is no test that asserts the disk-hatch warning prints at most once across a `session start` followed by multiple sliding-window renewals (`Manager.renewOnUse` -> `saveCache`). On stock Darwin without a verified tmpfs, `saveCache` is invoked on every business command after the first, so the current test only confirms the warning fires for a single `saveCache` call and does not cover the renewal storm that the docs (SECURITY.md "磁盘逃生舱") imply should print exactly once per process or only on explicit `session start`. Adding a regression test that calls `saveCache` N times in the same process under the Darwin-no-tmpfs probe and asserts the warning appears once (or N times, matching the intended contract) would lock the chosen behavior in.

**现有代码**

```
func TestSaveCacheDarwinFallsBackToDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
```

#### 25. The fallback path silently overwrites the original `ErrNoSecureSessionStore` cause with the disk-store result. When the …

- 位置：`internal/session/store.go:76-80`
- 优先级：P3

The fallback path silently overwrites the original `ErrNoSecureSessionStore` cause with the disk-store result. When the primary store fails for a documented reason (no tmpfs/ramfs on Darwin) AND the disk hatch write also fails (permission, disk full, etc.), the caller only sees the disk error and cannot tell that the secure store was the underlying trigger. Wrap the fallback write so both errors survive — e.g. `err = errors.Join(err, (diskCacheStore{}).Save(slot, cache))` or `err = fmt.Errorf("%w; disk hatch: %w", err, hatchErr)` — and keep `errors.Is(err, ErrNoSecureSessionStore)` working for callers that branch on the sentinel.

**现有代码**

```
		err = activeSessionStoreFor(slot).Save(slot, cache)
		if shouldFallbackToDiskHatch(err) {
			fmt.Fprintln(os.Stderr, InsecureCacheWarning)
			err = (diskCacheStore{}).Save(slot, cache)
		}
```

**建议改法**

```
		err = activeSessionStoreFor(slot).Save(slot, cache)
		if shouldFallbackToDiskHatch(err) {
			fmt.Fprintln(os.Stderr, InsecureCacheWarning)
			hatchErr := (diskCacheStore{}).Save(slot, cache)
			if hatchErr != nil {
				err = fmt.Errorf("secure store unavailable and disk hatch failed: %w; %w", err, hatchErr)
			} else {
				err = nil
			}
		}
```

#### 26. `saveCache` is invoked not only by `StartSession` / `RenewSession` but also on every sliding-window renewal in `Manager.…

- 位置：`internal/session/store.go:76-80`
- 优先级：P3

`saveCache` is invoked not only by `StartSession` / `RenewSession` but also on every sliding-window renewal in `Manager.renewOnUse` (which itself is triggered by `GetCachedKey` for each business command). On stock Darwin with no proven tmpfs, that means every successful command after the first prints `InsecureCacheWarning` again. The `legacyCacheNoticeMessage` path already solved this with a per-boot marker file (`notifyLegacyCacheOnce` in `cache.go`); consider mirroring that pattern here so the disk-hatch warning is shown at most once per boot instead of once per save.

**现有代码**

```
		err = activeSessionStoreFor(slot).Save(slot, cache)
		if shouldFallbackToDiskHatch(err) {
			fmt.Fprintln(os.Stderr, InsecureCacheWarning)
			err = (diskCacheStore{}).Save(slot, cache)
		}
```

#### 27. Test gap: the new `TestSaveCacheDarwinFallsBackToDisk` covers the happy fallback path but does not exercise the case whe…

- 位置：`internal/session/store.go:91-93`
- 优先级：P3

Test gap: the new `TestSaveCacheDarwinFallsBackToDisk` covers the happy fallback path but does not exercise the case where `diskCacheStore{}.Save` also fails (e.g. `XDG_CACHE_HOME` pointed at an unwritable directory). That is exactly the path where confirmed finding #1 surfaces — the original `ErrNoSecureSessionStore` cause gets dropped. Add a regression test that injects a writable `XDG_RUNTIME_DIR` overridden to return `unknown`, forces `sessionHostOS = "darwin"`, and points `XDG_CACHE_HOME` at a non-writable path, then asserts the returned error still chains `errors.Is(err, ErrNoSecureSessionStore)` (so callers like `cmd/auth.go` keep the actionable hint instead of a misleading permission/disk-full message).

**现有代码**

```
func shouldFallbackToDiskHatch(err error) bool {
	return err != nil && errors.Is(err, ErrNoSecureSessionStore) && sessionHostOS == "darwin"
}
```

#### 28. For consistency with the analogous `ListHosts` wrapper in host.go (and `env.List`), consider adding the result count as …

- 位置：`internal/ssh/manager.go:156-161`
- 优先级：P3

For consistency with the analogous `ListHosts` wrapper in host.go (and `env.List`), consider adding the result count as a perflog attribute when the call succeeds (e.g. `if err == nil { st.With("keypairs", len(res)) }`). The count is non-sensitive and matches the telemetry shape used by sibling `List*` wrappers, making `ssh.list-keypairs` rows directly comparable to `ssh.list-hosts` when diagnosing TUI load latency.

**现有代码**

```
func (m *Manager) ListKeyPairs() ([]KeyPairSummary, error) {
	st := perflog.Start("ssh.list-keypairs")
	res, err := m.listKeyPairsSummaries()
	st.EndErr(err)
	return res, err
}
```

**建议改法**

```
func (m *Manager) ListKeyPairs() ([]KeyPairSummary, error) {
	st := perflog.Start("ssh.list-keypairs")
	res, err := m.listKeyPairsSummaries()
	if err == nil {
		st.With("keypairs", len(res))
	}
	st.EndErr(err)
	return res, err
}
```

#### 29. ValidateName was previously the first-line guard in LoadEnvGroupWithKey and is now inside loadEnvGroupWithRoot, after wi…

- 位置：`internal/storage/manager.go:345-355`
- 优先级：P3

ValidateName was previously the first-line guard in LoadEnvGroupWithKey and is now inside loadEnvGroupWithRoot, after withVaultRead acquires the exclusive flock and runs recoverRekeyLocked and after openDataRoot opens the data root. An invalid input now pays a flock acquisition, manifest recovery, and a root open/close before failing. Move ValidateName back to the public entry point so it short-circuits before any I/O — the password-derived LoadEnvGroup wrapper already does this and the *WithKey variant should match.

**现有代码**

```
func (m *Manager) LoadEnvGroupWithKey(group string, key []byte) (*EnvGroup, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*EnvGroup, error) { return locked.LoadEnvGroupWithKey(group, key) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return m.loadEnvGroupWithRoot(root, group, key)
}
```

#### 30. loadEnvVaultWithKey is all-or-nothing: any single-group failure (corrupt meta, partially-migrated directory, permission …

- 位置：`internal/storage/manager.go:477-495`
- 优先级：P3

loadEnvVaultWithKey is all-or-nothing: any single-group failure (corrupt meta, partially-migrated directory, permission error) returns nil and discards successfully-loaded groups already in `out`. The TUI Snapshot caller (internal/env/manager.go Snapshot) renders an empty env tab on a single-group corruption. Consider partial-result-with-error semantics (return out + error indicating which group failed) or at minimum include the failing group name in the wrapped error so the TUI can degrade gracefully.

**现有代码**

```
	out := make(map[string]*EnvGroup, len(groups))
	for _, name := range groups {
		if seenDir := func() bool {
			_, err := root.ReadDir(EnvDirName, name)
			return err == nil
		}(); seenDir {
			eg, err := m.loadEnvGroupNewFormatFromRoot(root, name, cryptoKey)
			if err != nil {
				return nil, err
			}
			out[name] = eg
			continue
		}
		eg, err := m.loadEnvGroupWithRoot(root, name, cryptoKey)
		if err != nil {
			return nil, err
		}
		out[name] = eg
	}
```

#### 31. The inline seenDir closure duplicates the same root.ReadDir(EnvDirName, name) that loadEnvGroupWithRoot performs immedia…

- 位置：`internal/storage/manager.go:478-495`
- 优先级：P3

The inline seenDir closure duplicates the same root.ReadDir(EnvDirName, name) that loadEnvGroupWithRoot performs immediately afterwards on line 363, so legacy groups incur two directory stats. The first enumeration loop already knows which names are directories vs legacy — record the distinction in a small struct (or two maps) at enumeration time, then call loadEnvGroupWithRoot unconditionally. This also removes the closure entirely.

**现有代码**

```
	for _, name := range groups {
		if seenDir := func() bool {
			_, err := root.ReadDir(EnvDirName, name)
			return err == nil
		}(); seenDir {
			eg, err := m.loadEnvGroupNewFormatFromRoot(root, name, cryptoKey)
			if err != nil {
				return nil, err
			}
			out[name] = eg
			continue
		}
		eg, err := m.loadEnvGroupWithRoot(root, name, cryptoKey)
		if err != nil {
			return nil, err
		}
		out[name] = eg
	}
```

#### 32. The `valid` field on `manifestCacheEntry` is dead weight: the only insertion site (line 332) sets `valid: true`, there i…

- 位置：`internal/storage/rekey_manifest.go:287-293`
- 优先级：P3

The `valid` field on `manifestCacheEntry` is dead weight: the only insertion site (line 332) sets `valid: true`, there is no path that inserts `valid: false`, and no code transitions an existing entry to invalid. The guard `ok && cached.valid` at line 319 reduces to `ok`. Either drop the field (and its initializer) to make the cache invariant ("if present, the entry is authoritative") explicit, or wire it up to a real invalidation path — e.g., set `manifestCache[cacheKey] = manifestCacheEntry{}` (valid=false) at the start of `saveRekeyManifest` so a writer proactively invalidates the cache without waiting for the next stat to diverge.

**现有代码**

```
type manifestCacheEntry struct {
	valid       bool
	metaSize    int64
	metaModNano int64
	hasManifest bool
	manifest    *rekeyManifest
}
```

**建议改法**

```
type manifestCacheEntry struct {
	metaSize    int64
	metaModNano int64
	hasManifest bool
	manifest    *rekeyManifest
}
```

#### 33. `manifestCache` is a package-level `map[string]manifestCacheEntry` with no size cap, no eviction, and no invalidation on…

- 位置：`internal/storage/rekey_manifest.go:295-298`
- 优先级：P3

`manifestCache` is a package-level `map[string]manifestCacheEntry` with no size cap, no eviction, and no invalidation on path deletion. In production this is harmless because each process creates exactly one `Manager` per vault (all `cmd/*.go` production call sites instantiate a single `storage.NewManager(configPath, dataPath)`), but test suites construct many distinct vaults in the same process (`internal/storage/rekey_concurrent_test.go`, the `*rekey_concurrent_test.go` files across `internal/{config,env,provider,text}`, and the `cmd/*_test.go` files). A long-running `go test ./...` retains every `*rekeyManifest` ever loaded and prevents GC of their `Entries` backing arrays. A simple bounded LRU (size ~16) or an explicit `PurgeManifestCache()` hook called from test teardown would close this without affecting the production hot path.

**现有代码**

```
var (
	manifestCacheMu sync.Mutex
	manifestCache   = map[string]manifestCacheEntry{}
)
```

**建议改法**

```
const manifestCacheMaxEntries = 16

var (
	manifestCacheMu sync.Mutex
	manifestCache   = map[string]manifestCacheEntry{}
)

func PurgeManifestCache() {
	manifestCacheMu.Lock()
	manifestCache = map[string]manifestCacheEntry{}
	manifestCacheMu.Unlock()
}
```

#### 34. The cached `*rekeyManifest` pointer is shared across all callers that hit the same cache key, and `rekeyManifest.Entries…

- 位置：`internal/storage/rekey_manifest.go:330-339`
- 优先级：P3

The cached `*rekeyManifest` pointer is shared across all callers that hit the same cache key, and `rekeyManifest.Entries` is a `[]rekeyManifestEntry` whose backing array is shared through the slice header. No defensive copy is made on the fill path. Current downstream consumers (`recoverEntries` at rekey.go:521, `cleanupRekeyLocked` at rekey.go:561, and the hash dispatch at rekey.go:498–508) only read the manifest, so the shared pointer is safe today — but there is no method receiver, struct comment, or test pinning the read-only contract. A future change that mutates an entry field (e.g., marking it recovered, sorting in place, appending on partial failure) will silently corrupt every concurrent reader's view of the same cache entry. Shallow-clone the manifest and its `Entries` slice header at cache-store time so callers that `append` get their own backing array while existing readers stay unaffected.

**现有代码**

```
manifestCacheMu.Lock()
	manifestCache[cacheKey] = manifestCacheEntry{
		valid:       true,
		metaSize:    metaSize,
		metaModNano: metaModNano,
		hasManifest: hasManifest,
		manifest:    manifest,
	}
	manifestCacheMu.Unlock()
	return manifest, nil
```

**建议改法**

```
cloned := *manifest
	cloned.Entries = append([]rekeyManifestEntry(nil), manifest.Entries...)
	manifestCacheMu.Lock()
	manifestCache[cacheKey] = manifestCacheEntry{
		valid:       true,
		metaSize:    metaSize,
		metaModNano: metaModNano,
		hasManifest: hasManifest,
		manifest:    &cloned,
	}
	manifestCacheMu.Unlock()
	return manifest, nil
```

#### 35. `Reload()` now drops the `t.loaded = false` reset, but the field is still consulted in two places: `Init()` returns nil …

- 位置：`internal/tui/ai_tab.go:156-161`
- 优先级：P3

`Reload()` now drops the `t.loaded = false` reset, but the field is still consulted in two places: `Init()` returns nil early if `t.loaded`, and the `r` keybinding sets it to false before calling `load()`. With the new stale-while-revalidate behavior, `Reload()` is now identical to simply calling `load()` — the stale-while-revalidate invariant is enforced by `load()` itself not touching `t.loaded` (it only gets set in the `aiLoadedMsg` handler). The function name still implies cache invalidation, which no longer happens. Consider renaming to `Refresh()` or removing the now-redundant helper, and clarify in the comment that the top-level callsite (after sync) intentionally does not invalidate the cache.

**现有代码**

```
// Reload drops cached data and reloads; the top level calls it after a
// background sync applies remote changes.
func (t *aiTab) Reload() tea.Cmd {
	// stale-while-revalidate：后台重载期间旧数据保持可见，完成后静默替换。
	return t.load()
}
```

#### 36. `out.Warnings[0]` only surfaces the first warning. The provider layer can return multiple (e.g., stale catalog warning +…

- 位置：`internal/tui/ai_tab.go:302-304`
- 优先级：P3

`out.Warnings[0]` only surfaces the first warning. The provider layer can return multiple (e.g., stale catalog warning + base URL normalization warning + something else). Users editing a provider with stale metadata will see only one of several warnings, losing visibility into secondary issues. The toast on submit (`truncateWidth(res.Warnings[0], 40)`) has the same limitation. Consider joining all warnings with a separator and applying the width budget to the joined string.

**现有代码**

```
if len(out.Warnings) > 0 {
		notice += "；" + out.Warnings[0]
	}
```

#### 37. `providerErrorField` matches `"output"` before `"model"`, so an error message like "model output limit must be positive"…

- 位置：`internal/tui/ai_tab.go:929-938`
- 优先级：P3

`providerErrorField` matches `"output"` before `"model"`, so an error message like "model output limit must be positive" lands in the `model_outputs` field — correct. But the order of cases can cause subtle misrouting for future error wording. For example, if `assembleModels` ever emits an error containing both the substring "default reasoning" and "modalities", only the first match wins. Currently no error message spans both, but the new fields' error messages from `assembleModels` (e.g., "--model-output model %q is not in the final model set") do not contain "output" as a free substring — they contain "--model-output". This will route correctly to `model_outputs`. However, the substring matching is brittle: future additions of error wording could accidentally route to the wrong field.

Suggestion: when wiring new error messages in `assembleModels`, prefer field-specific error prefixes (e.g., "--model-output", "--model-modalities") so `providerErrorField` can disambiguate unambiguously.

**现有代码**

```
case strings.Contains(msg, "output"):
		return "model_outputs"
	case strings.Contains(msg, "default reasoning"), strings.Contains(msg, "默认推理"):
		return "model_default_reasoning"
	case strings.Contains(msg, "modalit"):
		return "model_modalities"
	case strings.Contains(msg, "reasoning"):
		return "model_reasoning"
	case strings.Contains(msg, "model"):
		return "models"
```

#### 38. The English doc comment above Reload() still claims "drops cached data and reloads", but the implementation now retains …

- 位置：`internal/tui/config_tab.go:167-172`
- 优先级：P3

The English doc comment above Reload() still claims "drops cached data and reloads", but the implementation now retains cached data (stale-while-revalidate) and only swaps it when the new configLoadedMsg lands. The new Chinese inline comment describes the actual behavior, but the English doc string is now stale and will mislead future maintainers. Consider updating the English comment to match — e.g. "Reload refreshes data in the background while keeping the current view visible; the top level calls it after a background sync applies remote changes."

**现有代码**

```
// Reload drops cached data and reloads; the top level calls it after a
// background sync applies remote changes.
func (t *configTab) Reload() tea.Cmd {
	// stale-while-revalidate：后台重载期间旧数据保持可见，完成后静默替换。
	return t.load()
}
```

**建议改法**

```
// Reload refreshes data in the background while keeping the current view
// visible (stale-while-revalidate); the top level calls it after a
// background sync applies remote changes. The new data replaces the
// cached copy silently once configLoadedMsg lands.
func (t *configTab) Reload() tea.Cmd {
	return t.load()
}
```

#### 39. The new `envSnap` field on `tuiGetter` is optional, but the two callers in the package diverge: `deref.go:57` populates …

- 位置：`internal/tui/deref.go:17-21`
- 优先级：P3

The new `envSnap` field on `tuiGetter` is optional, but the two callers in the package diverge: `deref.go:57` populates it (and benefits from the snapshot), while `mcp_tab.go:211` leaves it nil and stays on the per-key path. The asymmetry is intentional (the MCP exporter is one-shot, so the per-key path is fine; `resolveValues` is called on every filter/tab change), but a short comment on the field — or a constructor like `newTUIGetter(mgr, useSnap bool)` — would prevent a future maintainer from "fixing" `mcp_tab.go` to also pass a snapshot, and from accidentally passing a stale `envSnap` from a previous invalidation window. As-is the contract (caller is responsible for snapshot freshness) lives only in this file.

**现有代码**

```
type tuiGetter struct {
	envMgr  *env.Manager
	textMgr *text.Manager
	envSnap map[string]map[string]string
}
```

#### 40. The `snapErr` from `envSnapshot` is silently discarded, causing graceful degradation to the per-key `envMgr.Get` path. W…

- 位置：`internal/tui/deref.go:53-56`
- 优先级：P3

The `snapErr` from `envSnapshot` is silently discarded, causing graceful degradation to the per-key `envMgr.Get` path. While the fallback is correct, the failure is invisible: a transient snapshot error (e.g., vault lock contention, storage I/O hiccup) silently turns the perf-optimized `resolveValues` back into a slow N-key reader with no log, metric, or UI hint. The codebase already uses `perflog` for similar instrumentation (see `env_tab.go:135` and `ai_tab.go`), so wrapping this path with `perflog.Start("tui.resolve-values")` (and `End(false)` on the snap error) would make the silent fallback diagnosable. As-is, an operator chasing a "TUI felt slow" report has no signal that the snapshot path was bypassed.

**现有代码**

```
	vars, _, snapErr := envSnapshot(mgr)
	if snapErr != nil {
		vars = nil
	}
```

#### 41. After `mcpFormReopenMsg` is handled, the form's `index` stays at whatever field the user last touched, but the error is …

- 位置：`internal/tui/mcp_tab.go:233-243`
- 优先级：P3

After `mcpFormReopenMsg` is handled, the form's `index` stays at whatever field the user last touched, but the error is attached to a different field. The user sees the validation message next to (for example) the `alias` row while the cursor still points at `command`, forcing manual navigation to read the error. Move the cursor to the field named by `msg.field` (set `t.form.index = t.form.fieldIndex(msg.field)` after attaching the error) so the offending input is already focused.

**现有代码**

```
	case mcpFormReopenMsg:
		msg.form.SetSize(t.width, t.height)
		for key, value := range msg.values {
			msg.form.SetValue(key, value)
		}
		if index := msg.form.fieldIndex(msg.field); index >= 0 {
			msg.form.errs[index] = msg.err.Error()
		}
		t.form = msg.form
		t.formSubmit = msg.submit
		return t, nil
```

#### 42. `updateMode` for `mcpModeDelete` and `mcpModePlan` cancels the dialog on any key other than the listed ones (`y`/`enter`…

- 位置：`internal/tui/mcp_tab.go:347-354`
- 优先级：P3

`updateMode` for `mcpModeDelete` and `mcpModePlan` cancels the dialog on any key other than the listed ones (`y`/`enter`/`F`), even though `Help()` only advertises `esc/n 取消`. Pressing an arrow key while reviewing a plan, or pressing `tab`/`space`/etc. during the delete prompt, silently discards the staged action and shows a misleading 「已取消」 toast. Other tabs (`ai_tab.go` `updateMode`, `ssh_tab.go` `renderModal`) only act on the documented confirm/cancel keys and ignore everything else; mirror that pattern so a stray keypress can't wipe out a half-typed plan or delete confirmation.

**现有代码**

```
		case mcpModeDelete:
			if key == "y" || key == "enter" {
				alias := t.pendingAlias
				t.cancelMode()
				return t, t.doDelete(alias)
			}
			t.cancelMode()
			return t, warnToast("已取消")
```

#### 43. `t.changedItems()` is invoked twice on the same keypress in this branch (lines 356 and 359). Each call re-filters `unexp…

- 位置：`internal/tui/mcp_tab.go:355-361`
- 优先级：P3

`t.changedItems()` is invoked twice on the same keypress in this branch (lines 356 and 359). Each call re-filters `unexportPlan.Items`, which is wasteful and means the two results are not guaranteed to be the same slice if the plan is ever mutated between calls. Cache the slice once at the top of the branch (also fixes the same duplication in the `mcpModePlan` arm at line 373).

**现有代码**

```
case mcpModeChangedConfirm:
		item := t.changedItems()[t.changedIdx]
		t.changedAllowed[item.Agent+"/"+item.Alias] = key == "y"
		t.changedIdx++
		if t.changedIdx < len(t.changedItems()) {
			return t, nil
		}
```

**建议改法**

```
case mcpModeChangedConfirm:
		items := t.changedItems()
		if t.changedIdx >= len(items) {
			return t, nil
		}
		item := items[t.changedIdx]
		t.changedAllowed[item.Agent+"/"+item.Alias] = key == "y"
		t.changedIdx++
		if t.changedIdx < len(items) {
			return t, nil
		}
```

#### 44. `updateMode` `mcpModePlan` `default` branch unconditionally cancels and toasts `已取消` for any non-`F`/`y`/`enter` keypres…

- 位置：`internal/tui/mcp_tab.go:368-383`
- 优先级：P3

`updateMode` `mcpModePlan` `default` branch unconditionally cancels and toasts `已取消` for any non-`F`/`y`/`enter` keypress (arrow keys, space, tab, …). That contradicts the help text `enter/y 确认 · F 覆盖漂移 · esc/n 取消` and is inconsistent with how `ai_tab.updateMode` / `ssh_tab` confirmation modes behave. Restrict cancel to `esc`/`n` and let other keys be no-ops.

**现有代码**

```
	case mcpModePlan:
		switch key {
		case "F":
			return t.replanForce()
		case "y", "enter":
			if t.planKind == "unexport" && len(t.changedItems()) > 0 {
				t.changedAllowed = map[string]bool{}
				t.changedIdx = 0
				t.mode = mcpModeChangedConfirm
				return t, nil
			}
			return t.confirmPlan()
		default:
			t.cancelMode()
			return t, warnToast("已取消")
		}
```

#### 45. `mcpExportAuditTarget` and `mcpUnexportAuditTarget` both extract the alias from `plan.Items[0].Alias`, but `ExecuteUnexp…

- 位置：`internal/tui/mcp_tab.go:830-844`
- 优先级：P3

`mcpExportAuditTarget` and `mcpUnexportAuditTarget` both extract the alias from `plan.Items[0].Alias`, but `ExecuteUnexport` re-uses those `UnexportItem` records verbatim in its `report.Items`. When `UnexportRemove` succeeds and the alias came from the ledger filter (i.e. it was previously exported), `plan.Items[0].Alias` is normally populated. However, if the unexport plan was constructed from a ledger whose fingerprint no longer matches, the alias could be empty (or stale) — `mcpUnexportAuditTarget` defends with a fallback to `alias` (the pendingAlias), but `mcpExportAuditTarget` has no such guard and would emit `"mcp: agents:..."`. For symmetry and to keep audit subjects well-formed, apply the same `plan.Items[0].Alias → alias → "mcp:"` fallback to `mcpExportAuditTarget`.

**现有代码**

```
func mcpExportAuditTarget(plan *mcp.ExportPlan) string {
	if plan == nil || len(plan.Items) == 0 {
		return "mcp:"
	}
	agents := make([]string, 0, len(plan.Items))
	seen := map[string]bool{}
	for _, item := range plan.Items {
		if seen[item.Agent] {
			continue
		}
		seen[item.Agent] = true
		agents = append(agents, item.Agent)
	}
	return "mcp:" + plan.Items[0].Alias + " agents:" + strings.Join(agents, ",")
}
```

#### 46. The new MCP loop variable `s` shadows the method's receiver `s *searchTab`. The rest of `gather()` uses distinct inner n…

- 位置：`internal/tui/search.go:163-173`
- 优先级：P3

The new MCP loop variable `s` shadows the method's receiver `s *searchTab`. The rest of `gather()` uses distinct inner names (`g`, `ti`, `c`, `h`, `p`) — rename to `srv` (matching `t.currentServer()` in mcp_tab.go) to keep the convention and avoid shadowing.

**现有代码**

```
		// MCP: 档案 alias 与 command 是标识；List 只带 env 键名，值不进库存。
		if mgr.MCP != nil {
			if servers, err := mgr.MCP.List(); err == nil {
				for _, s := range servers {
					all = append(all, searchResult{
						resultType: typeMCP, key: s.Alias, extra: s.Command,
						preview: s.Command,
					})
				}
			}
		}
```

**建议改法**

```
		// MCP: profile alias and command are identifiers; List only carries
		// env key names, values never enter the search inventory.
		if mgr.MCP != nil {
			if servers, err := mgr.MCP.List(); err == nil {
				for _, srv := range servers {
					all = append(all, searchResult{
						resultType: typeMCP, key: srv.Alias, extra: srv.Command,
						preview: srv.Command,
					})
				}
			}
		}
```

#### 47. Section comment is in Chinese while every other comment in this file is English — translate for consistency.

- 位置：`internal/tui/search.go:163`
- 优先级：P3

Section comment is in Chinese while every other comment in this file is English — translate for consistency.

**现有代码**

```
		// MCP: 档案 alias 与 command 是标识；List 只带 env 键名，值不进库存。
```

#### 48. Section comment is in Chinese while every other comment in this file is English — translate for consistency with the sur…

- 位置：`internal/tui/search.go:163`
- 优先级：P3

Section comment is in Chinese while every other comment in this file is English — translate for consistency with the surrounding block comments (e.g. the `SSH`, `AI`, and `Text` blocks above this one).

**现有代码**

```
// MCP: 档案 alias 与 command 是标识；List 只带 env 键名，值不进库存。
```

**建议改法**

```
// MCP: alias and command are identifiers; List returns env key names only,
		// never values.
```

#### 49. The loop variable `s` shadows the outer receiver `s *searchTab`. Other iterations in this function use distinct names (`…

- 位置：`internal/tui/search.go:166-171`
- 优先级：P3

The loop variable `s` shadows the outer receiver `s *searchTab`. Other iterations in this function use distinct names (`g`, `ti`, `c`, `h`, `p`). Rename to `srv` (matching `t.currentServer()` / `srv` in `mcp_tab.go`) to keep the convention and avoid the shadow.

**现有代码**

```
for _, s := range servers {
					all = append(all, searchResult{
						resultType: typeMCP, key: s.Alias, extra: s.Command,
						preview: s.Command,
					})
				}
```

**建议改法**

```
for _, srv := range servers {
					all = append(all, searchResult{
						resultType: typeMCP, key: srv.Alias, extra: srv.Command,
						preview: srv.Command,
					})
				}
```

## Warnings

- {'file': 'internal/tui/config_tab.go', 'message': 'round 2: LLM completion error: POST "https://api.minimaxi.com/v1/chat/completions": 429 Too Many Requests {"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)","http_code":"429"}', 'type': 'review_round_failed'}
- {'file': 'internal/tui/ai_tab.go', 'message': 'round 2: LLM completion error: POST "https://api.minimaxi.com/v1/chat/completions": 429 Too Many Requests {"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)","http_code":"429"}', 'type': 'review_round_failed'}
- {'file': 'internal/storage/rekey_manifest.go', 'message': 'round 2: LLM completion error: POST "https://api.minimaxi.com/v1/chat/completions": 429 Too Many Requests {"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)","http_code":"429"}', 'type': 'review_round_failed'}
- {'file': 'internal/storage/manager.go', 'message': 'round 2: LLM completion error: POST "https://api.minimaxi.com/v1/chat/completions": 429 Too Many Requests {"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)","http_code":"429"}', 'type': 'review_round_failed'}
- {'file': 'internal/tui/deref.go', 'message': 'round 2: LLM completion error: POST "https://api.minimaxi.com/v1/chat/completions": 429 Too Many Requests {"type":"rate_limit_error","message":"已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)","http_code":"429"}', 'type': 'review_round_failed'}
