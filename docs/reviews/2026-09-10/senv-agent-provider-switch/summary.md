## Review 总结 · senv-agent-provider-switch

范围：`senv` / `default-branch` `origin/main` → `HEAD`
结论：P0=13 P1=9 P2=19 P3=19
完整文档：`docs/reviews/2026-09-10/senv-agent-provider-switch/review.md`

### P0

- [P0] The `--source` flag value is forwarded to `llm.Fetch` and handed straight to `client.Get(source)` with no scheme/host va… — `cmd/ai.go:95-100`
- [P0] The `--api-key` flag string is captured as a long-lived package-level variable `providerAddAPIKey` and survives until th… — `cmd/ai_provider.go:33-41`
- [P0] Command-line `--api-key` flag leaks the LLM provider credential into the process argv (visible via `/proc/<pid>/cmdline`… — `cmd/ai_provider.go:172`
- [P0] Defense-in-depth: there is no `Content-Type` check before consuming the body. Combined with the unconstrained redirect b… — `internal/llm/catalog.go:60-62`
- [P0] Force overwrite of `--api-key` silently destroys the previous credential in `llm-keys/<alias>`. `text.Manager.Set` is a … — `internal/llm/provider.go:154-159`
- [P0] `restoreFromBackup` overwrites the only good copy before restoring it. It reads `path.senv-bak` (the original content) a… — `internal/llm/switch.go:188-203`
- [P0] Line-based TOML editing is unsafe for valid TOML. `applyTOMLEdits` splits the file on `"\n"` and `upsertTopLevelLine` / … — `internal/llm/switch.go:215-234`
- [P0] Plaintext credentials persisted to user agent configs and `.senv-bak` backups with no zeroization or cleanup. Four of fi… — `internal/llm/switch.go:337-345`
- [P0] piAdapter writes two files with no coordinated rollback. The first `applyJSONMerge` succeeds and replaces `~/.pi/agent/m… — `internal/llm/switch.go:425-447`
- [P0] No synchronization around `SwitchManager.Switch`. Both `internal/tui/ai_tab.go` (via `switchCmd()` returning a `tea.Cmd`… — `internal/llm/switch.go:554-555`
- [P0] `llm_providers` is a new top-level encrypted collection, but the storage lifecycle only knows the existing collections. … — `internal/storage/llm_provider.go:9`
- [P0] Validation is only enforced on save; nothing in `LoadLLMProvider`/`LoadLLMProviderWithKey` re-runs `ValidateLLMProvider`… — `internal/storage/llm_provider.go:44-50`
- [P0] `ValidateLLMProvider` is only invoked from `SaveLLMProviderWithKey`. The corresponding `LoadLLMProviderWithKey`/`LoadLLM… — `internal/storage/types.go:126-127`

### P1

- [P1] The CLI never warns that `--base-url` is not pinned to an https-only scheme and trusts whatever the operator types. `int… — `cmd/ai_provider.go:57-61`
- [P1] `args[0]` is concatenated directly into the audit log subject (`"provider:"+args[0]`) and into the success message (`✓ 已… — `cmd/ai_provider.go:150`
- [P1] agentHomeDir silently falls back to "." when os.UserHomeDir fails (e.g., daemon/cron with no HOME and no passwd entry). … — `cmd/ai_switch.go:20-27`
- [P1] SSRF: `client.Get(source)` follows HTTP redirects with Go's default policy (up to 10 hops, any host). The only caller (`… — `internal/llm/catalog.go:55`
- [P1] SavePointers relies on `os.MkdirAll(dir, pointerDirMode)` to enforce a 0700 directory, but MkdirAll is a no-op on the mo… — `internal/llm/pointer.go:111-114`
- [P1] `parseHTTPURL` does not reject URLs that contain userinfo (`https://user:pass@host`). The userinfo is preserved in `entr… — `internal/llm/provider.go:176-182`
- [P1] `os.MkdirAll(dir, 0o700)` does not tighten an existing directory's permissions. If `~/.claude` (or any other agent confi… — `internal/llm/switch.go:146-152`
- [P1] Duplicate top-level keys after switching. `topLevelKey(e)` only looks at the first line of `e.topLevel` ("model" here), … — `internal/llm/switch.go:225-228`
- [P1] `url.Parse` accepts userinfo (`https://user:pw@host`) and is otherwise very permissive; combined with the load-time bypa… — `internal/storage/types.go:131-134`

### P2

- [P2] Audit log subject on the `add` failure path uses raw `args[0]`, while the success path uses `res.Entry.Alias`. `internal… — `cmd/ai_provider.go:68-72`
- [P2] Confirmed finding (prior pass): the inline `filepath.Join(configPath, "agent-pointers.json")` duplicates the path resolu… — `cmd/mcp.go:129`
- [P2] Fetch lacks a `context.Context` parameter. It performs a 30s network call via `client.Get(source)` (which uses `context.… — `internal/llm/catalog.go:50-55`
- [P2] LoadPointers and SavePointers perform no internal serialization, and the atomic temp+rename only guards a single write. … — `internal/llm/pointer.go:72-93`
- [P2] SavePointers renames the temp file without an intervening tmp.Sync(), so the new pointer file may not be on durable stor… — `internal/llm/pointer.go:128-139`
- [P2] Orphan credential on owned→external ref switch: when re-issuing `add <alias> --key-ref env:foo/KEY --force` (or any exte… — `internal/llm/provider.go:154-160`
- [P2] `validateCredentialInput` only validates the syntax of `--key-ref` (group/key names) via `ValidateCredentialRef`; it doe… — `internal/llm/provider.go:192-205`
- [P2] `GetProvider` detects `os.ErrNotExist` via `errors.Is` and then constructs a fresh `fmt.Errorf` that does NOT wrap the o… — `internal/llm/provider.go:269-275`
- [P2] Same `errors.Is`/replace pattern as `GetProvider`: this branch swaps the storage-level "not found" error (which wraps `o… — `internal/llm/provider.go:299-302`
- [P2] Profile is deleted BEFORE its owned credential. If `DeleteLLMProvider` succeeds but the subsequent `textManager().Delete… — `internal/llm/provider.go:306-314`
- [P2] RemoveProvider treats a missing credential as an error and returns it to the caller after the profile is already gone. `… — `internal/llm/provider.go:309-314`
- [P2] No fsync on the temp file before `os.Rename`, and the rename is not followed by a parent-dir fsync. The package comment … — `internal/llm/switch.go:169-186`
- [P2] Rollback error silently dropped on pointer-load failure. When `LoadPointers` returns a non-`ErrPointerNotFound` error (e… — `internal/llm/switch.go:599-607`
- [P2] DeleteLLMProvider routes through `deleteSSHEntry`, which calls `validateSSHIdentity(kind, name)` where `kind = "LLM prov… — `internal/storage/llm_provider.go:62-66`
- [P2] Hardcoded "SSH" prefix left in the error message. After generalizing sshKindForDir to entryKindForDir (and dropping "SSH… — `internal/storage/ssh.go:168`
- [P2] deleteSSHEntry still validates via validateSSHIdentity, which prepends "invalid SSH " to the kind. When DeleteLLMProvide… — `internal/storage/ssh.go:209-212`
- [P2] Hardcoded "SSH" prefix left in the deletion error message. With DeleteLLMProvider passing kind="LLM provider", the wrapp… — `internal/storage/ssh.go:222`
- [P2] On a failed switch the previous success notice is left in `t.notice`, so the right pane continues to render `✓ ... → ...… — `internal/tui/ai_tab.go:124-129`
- [P2] In `aiFlowConfirm`'s `y`/`enter` branch the handler resets `t.flow = aiFlowNone` *before* returning the `switchCmd()` cl… — `internal/tui/ai_tab.go:184-186`

### P3

- [P3] `cat.Counts()` before `Save` does not add real validation: `llm.Fetch` already calls `Parse`, which runs `validateProvid… — `cmd/ai.go:42-55`
- [P3] All non-`ErrCacheNotFound` errors from `llm.Load` (including permission denied, I/O errors, JSON corruption, version mis… — `cmd/ai.go:71-77`
- [P3] `Long:` help text (lines 41-44) describes the model set as "the union of models from --catalog-provider (models.dev cach… — `cmd/ai_provider.go:46-50`
- [P3] `auditOp` is invoked before `getAIProviderManager()` completes its prompt. If `getAIProviderManager()` itself fails (vau… — `cmd/ai_provider.go:52-61`
- [P3] The success message at line 74 prints `res.Entry.Alias` (trimmed) while the failure message at line 73 prints `args[0]` … — `cmd/ai_provider.go:68-81`
- [P3] CLI-level help text and human-facing messages here mix English flag descriptions with Chinese status/audit messages (`"暂… — `cmd/ai_provider.go:100`
- [P3] `aiProviderListCmd` formats output with `\t` column separators (line 106). `securefs.ValidateSegment` only rejects `\x00… — `cmd/ai_provider.go:105-106`
- [P3] Existing test `TestAIProviderFullLifecycle` (`cmd/ai_provider_test.go`) does not cover: (1) alias with leading/trailing … — `cmd/ai_provider.go:171-173`
- [P3] Passing `nil` for the provider manager here is intentional (Status() only reads pointers and config paths, no vault acce… — `cmd/ai_switch.go:75`
- [P3] The pointer file path is recomputed on every MCP request via `filepath.Join(configPath, "agent-pointers.json")` inside t… — `cmd/mcp.go:128-130`
- [P3] `llm_agent_status` only reads the local `agent-pointers.json` file (no vault access), so calling `m.pullBeforeRead()` he… — `cmd/mcp_llm.go:60-62`
- [P3] PointerFile.Set accepts (agentID, provider, model) without validating that provider and model are non-empty, and LoadPoi… — `internal/llm/pointer.go:59-69`
- [P3] LoadPointers's per-agent validation produces a duplicated "agent pointer file corrupt" prefix. SwitchedAtTime returns `f… — `internal/llm/pointer.go:87-91`
- [P3] This error message mixes Chinese (`（孤儿凭据未清理：%s）`) with English, breaking the otherwise consistent English error contract… — `internal/llm/provider.go:163`
- [P3] Same mixed-language issue: this warning string contains Chinese characters (`模型目录缓存已超过 7 天...建议执行 senv ai refresh`) inco… — `internal/llm/provider.go:247-249`
- [P3] `upsertTopLevelLine` is O(N) on the slice and builds three full intermediate slices per call (lines[:existing], lines[:i… — `internal/llm/switch.go:224-232`
- [P3] Error-wrapping inconsistency loses the primary cause. The dual-failure path uses `%v` for the primary `err` (`fmt.Errorf… — `internal/llm/switch.go:609-614`
- [P3] The diff intentionally converts the decrypt/not-found/parse error messages in loadSSHEntry (and listSSHEntries' identity… — `internal/storage/ssh.go:164`
- [P3] The half-width computation in `View()` is dead/confusing: `half := t.width / 2; if half < 20 { half = max(t.width/2, 1) … — `internal/tui/ai_tab.go:299-302`
