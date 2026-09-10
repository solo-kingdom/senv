---
repo: senv
mode: default-branch
date: 2026-09-10
change_name: senv-agent-provider-switch
branch: agent-provider-switch
from: origin/main
to: HEAD
mr: 
ocr_session: 1eb92ea1-9ae4-4b19-a85d-e70a060fa4d1
ocr_status: complete
---

# Code Review · senv-agent-provider-switch

## Meta

- 仓库：`senv`
- 范围：`default-branch` `origin/main` → `HEAD`
- 分支：`agent-provider-switch`（默认 `main`）
- OCR：files=22 comments=60 elapsed=18m10s session=`1eb92ea1-9ae4-4b19-a85d-e70a060fa4d1`
- OCR message：Review complete: 60 finding(s) across 22 selected item(s).

## 统计

P0=13 / P1=9 / P2=19 / P3=19

## Findings（完整）

### P0

#### 1. The `--source` flag value is forwarded to `llm.Fetch` and handed straight to `client.Get(source)` with no scheme/host va…

- 位置：`cmd/ai.go:95-100`
- 优先级：P0

The `--source` flag value is forwarded to `llm.Fetch` and handed straight to `client.Get(source)` with no scheme/host validation. While the flag is user-typed on a CLI, the change set also wires catalog access into MCP and TUI paths; any future caller that derives `--source` from configuration or env would have no guardrail against `file://`, `http://` to internal hosts, or unintended redirects (Go's default `http.Client` follows redirects).

→ Suggestion: at minimum document that `--source` must be an HTTPS URL pointing to a public models.dev mirror, or validate scheme (`https` only) and reject known internal ranges before calling `Fetch`.

**现有代码**

```
func init() {
	rootCmd.AddCommand(aiCmd)
	aiRefreshCmd.Flags().StringVar(&aiRefreshSource, "source", "",
		"catalog source URL (default "+llm.DefaultCatalogURL+")")
	aiCmd.AddCommand(aiRefreshCmd)
	aiCmd.AddCommand(aiCatalogCmd)
```

#### 2. The `--api-key` flag string is captured as a long-lived package-level variable `providerAddAPIKey` and survives until th…

- 位置：`cmd/ai_provider.go:33-41`
- 优先级：P0

The `--api-key` flag string is captured as a long-lived package-level variable `providerAddAPIKey` and survives until the next `senv` invocation overwrites the flag binding. Cobra does not zero it after use. Anyone able to read the process memory (core dump, `/proc/<pid>/mem` with sufficient privileges, debugger) after the command returns can recover the raw key. Combine this with the argv exposure (see companion comment) and the key has at least three independent plaintext footprints. Prefer reading into a local, `defer`ing an explicit zeroize (`for i := range buf { buf[i] = 0 }`) on a `[]byte`, and avoid keeping the secret in a process-global flag binding longer than necessary.

**现有代码**

```
var (
	providerAddBaseURL string
	providerAddAPIKey  string
	providerAddKeyRef  string
	providerAddCatalog string
	providerAddModels  []string
	providerAddDefault string
	providerAddForce   bool
)
```

**建议改法**

```
var (
	providerAddBaseURL string
	// providerAddAPIKey 故意不放在这里；改用本地变量 + defer zeroize，
	// 避免凭据在包级变量中残留到下次命令调用。
	providerAddKeyRef  string
	providerAddCatalog string
	providerAddModels  []string
	providerAddDefault string
	providerAddForce   bool
)
```

#### 3. Command-line `--api-key` flag leaks the LLM provider credential into the process argv (visible via `/proc/<pid>/cmdline`…

- 位置：`cmd/ai_provider.go:172`
- 优先级：P0

Command-line `--api-key` flag leaks the LLM provider credential into the process argv (visible via `/proc/<pid>/cmdline`, `ps`, `top`), shell history (`.bash_history`/`.zsh_history`), system audit logs, and any crash dumps. This contradicts the file's own stated goal of credential storage in the vault — even if at-rest storage is encrypted, the input path here is plaintext across the whole process boundary. The same file already routes through `resolveAuth(..., authPrompt)` for vault unlock, proving a secure interactive prompt pathway exists in this package; the new command should read the LLM API key via TTY prompt (`authPrompt`-style helper), stdin, or an env/file indirection — never a positional flag value.

**现有代码**

```
+	aiProviderAddCmd.Flags().StringVar(&providerAddAPIKey, "api-key", "", "API key (stored encrypted in vault)")
```

**建议改法**

```
		// 示例：从 stdin/TTY 读取后填入 opts.APIKey，避免出现在 argv 上
		apiKey:          readAPIKeySecurely(),
```

#### 4. Defense-in-depth: there is no `Content-Type` check before consuming the body. Combined with the unconstrained redirect b…

- 位置：`internal/llm/catalog.go:60-62`
- 优先级：P0

Defense-in-depth: there is no `Content-Type` check before consuming the body. Combined with the unconstrained redirect behaviour above, a redirect target can serve `text/html` (or any non-JSON) and have it written into the response slice before `Parse` rejects it on JSON shape. The `validateProviders` check is a safety net but it still runs after a full read of attacker-controlled bytes; verify `resp.Header.Get("Content-Type")` starts with `application/json` (or similar) before reading, and require HTTPS scheme on `source` up-front.

**现有代码**

```
if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch catalog: unexpected status %s", resp.Status)
	}
```

**建议改法**

```
if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch catalog: unexpected status %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return nil, fmt.Errorf("fetch catalog: unexpected content-type %q", ct)
	}
```

#### 5. Force overwrite of `--api-key` silently destroys the previous credential in `llm-keys/<alias>`. `text.Manager.Set` is a …

- 位置：`internal/llm/provider.go:154-159`
- 优先级：P0

Force overwrite of `--api-key` silently destroys the previous credential in `llm-keys/<alias>`. `text.Manager.Set` is a blind overwrite with no backup or warning, so users updating only the model list or base URL can lose API key access with no way to recover. Consider either (a) refusing the operation when `opts.APIKey != ""` and an existing credential is present without an explicit `--rotate-credential` flag, or (b) preserving the existing credential and only updating profile metadata unless the caller opts into replacement.

Reproduction: `AddProvider(Alias:"p", APIKey:"old")` then `AddProvider(Alias:"p", APIKey:"new", Force:true)` — `tm.Get("llm-keys","p")` now returns `"new"` and `"old"` is gone forever.

**现有代码**

```
		// 先写凭据后写档案；档案失败时回删新建凭据（vault mutation 是锁非事务）。
		if opts.APIKey != "" {
			if err := locked.textManager().Set(LLMKeysGroup, alias, opts.APIKey); err != nil {
				return fmt.Errorf("store credential: %w", err)
			}
		}
```

**建议改法**

```
		// 先写凭据后写档案；档案失败时回删新建凭据（vault mutation 是锁非事务）。
		if opts.APIKey != "" {
			if _, existed := locked.textManager().Get(LLMKeysGroup, alias); existed == nil && opts.Force {
				// rotating an existing credential -- consider requiring an explicit --rotate flag
			}
			if err := locked.textManager().Set(LLMKeysGroup, alias, opts.APIKey); err != nil {
				return fmt.Errorf("store credential: %w", err)
			}
		}
```

#### 6. `restoreFromBackup` overwrites the only good copy before restoring it. It reads `path.senv-bak` (the original content) a…

- 位置：`internal/llm/switch.go:188-203`
- 优先级：P0

`restoreFromBackup` overwrites the only good copy before restoring it. It reads `path.senv-bak` (the original content) and then calls `atomicWriteWithBackup(path, original)`, whose first step is to read the current (modified) `path` and write it into `path.senv-bak`. If the subsequent rename fails (ENOSPC, EIO, permission), the original content has just been clobbered in the backup and there is no good copy left. The restore should write `original` to a fresh temp file in `dir` and `os.Rename` it to `path` directly, never touching the `.senv-bak` slot — only remove the backup once the rename has succeeded.

**现有代码**

```
// restoreFromBackup 用 <path>.senv-bak 恢复原配置；备份不存在时删除 path。
// 恢复错误原样返回，由调用方并入最终错误。
func restoreFromBackup(path string) error {
	backup := path + ".senv-bak"
	data, err := os.ReadFile(backup)
	if err != nil {
		if os.IsNotExist(err) {
			return os.Remove(path)
		}
		return err
	}
	if err := atomicWriteWithBackup(path, data); err != nil {
		return err
	}
	return os.Remove(backup)
}
```

#### 7. Line-based TOML editing is unsafe for valid TOML. `applyTOMLEdits` splits the file on `"\n"` and `upsertTopLevelLine` / …

- 位置：`internal/llm/switch.go:215-234`
- 优先级：P0

Line-based TOML editing is unsafe for valid TOML. `applyTOMLEdits` splits the file on `"\n"` and `upsertTopLevelLine` / `upsertTOMLBlock` only look at trimmed line prefixes. This ignores multi-line basic strings (`"""..."""`), multi-line literal strings (`'''...'''`), multi-line arrays, inline tables, and arrays of tables (`[[...]]`); the header detector explicitly skips `[[` but the body of an array-of-tables block is still treated as in-block-or-not via `inBlock`, so adding a sibling `[[...]]` for an unrelated agent section will be silently dropped when replacing a header block. If the user's existing config has a multi-line string or array, an insert at the computed `insertAt` can land inside the literal and the next time the agent parses the file it will refuse to start. Either use a real TOML library (e.g. `github.com/pelletier/go-toml/v2`) or restrict the primitive to keys whose value is provably a single-line scalar; for arrays/strings/tables, append to the file end and let the user re-order.

**现有代码**

```
// applyTOMLEdits 逐条应用编辑并原子写回。topLevel 行替换同名顶层赋值；
// 不存在时插入到首个表头之前（TOML 顶层键不允许出现在表头之后）。
func applyTOMLEdits(path string, edits []tomlEdit) error {
	src, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.Split(string(src), "\n")

	for _, e := range edits {
		key := topLevelKey(e)
		if key != "" {
			lines = upsertTopLevelLine(lines, key, e.topLevel...)
		}
		if e.header != "" {
			lines = upsertTOMLBlock(lines, e.header, e.block)
		}
	}
	return atomicWriteWithBackup(path, []byte(strings.Join(lines, "\n")))
}
```

#### 8. Plaintext credentials persisted to user agent configs and `.senv-bak` backups with no zeroization or cleanup. Four of fi…

- 位置：`internal/llm/switch.go:337-345`
- 优先级：P0

Plaintext credentials persisted to user agent configs and `.senv-bak` backups with no zeroization or cleanup. Four of five supported agents (Claude Code, Kimi, Pi, OpenCode) write the decrypted API key into `ANTHROPIC_AUTH_TOKEN` / `api_key` / `apiKey` fields. Combined with `.senv-bak` files that are created next to every touched config and never removed (only `os.WriteFile(path+".senv-bak", existing, 0o600)` at line 154, no matching cleanup), the prior credentialed config sits on disk in addition to the new one. `req.Credential` carries the raw plaintext string through the entire call chain, and `os.MkdirAll(dir, 0o700)` does not tighten an already-existing directory, so the "0600 收敛" claim in the docstring above is misleading — the parent directory permissions are inherited as-is and the `.senv-bak` retains the prior plaintext key. The docstring should at minimum warn the user; ideally the package offers an env-var mode for the other adapters too, and removes the `.senv-bak` after a successful switch (or moves it out of the agent's config dir).

**现有代码**

```
Apply: func(req SwitchRequest) error {
			return applyJSONMerge(req.ConfigPath, func(root map[string]any) error {
				root["model"] = req.Model
				env := ensureSubMap(root, "env")
				env["ANTHROPIC_BASE_URL"] = req.BaseURL
				env["ANTHROPIC_AUTH_TOKEN"] = req.Credential
				return nil
			})
		},
```

#### 9. piAdapter writes two files with no coordinated rollback. The first `applyJSONMerge` succeeds and replaces `~/.pi/agent/m…

- 位置：`internal/llm/switch.go:425-447`
- 优先级：P0

piAdapter writes two files with no coordinated rollback. The first `applyJSONMerge` succeeds and replaces `~/.pi/agent/models.json`; if the second `applyJSONMerge` (for `settings.json`) then fails, this function returns the error and `Switch()` propagates it without calling `restoreFromBackup` (lines 595-597 of `Switch` only restore on pointer-file errors, not on `adapter.Apply` errors). Result: models.json contains the new provider while settings.json still names the previous `defaultProvider`, and pi will fail to start because the selected provider isn't in `models.json` (or will silently fall back). The package comment promises "失败 → 回滚配置", so the missing restore on `adapter.Apply` failure is a contract violation specifically for pi. Either wrap the two writes in a small helper that registers both files for rollback on the first error, or have `Switch()` track all touched files and restore them whenever `adapter.Apply` returns an error.

**现有代码**

```
Apply: func(req SwitchRequest) error {
			id := senvProviderID(req.ProviderAlias)
			if err := applyJSONMerge(req.ConfigPath, func(root map[string]any) error {
				providers := ensureSubMap(root, "providers")
				models := make([]map[string]any, 0, 1)
				models = append(models, map[string]any{"id": req.Model, "name": req.Model})
				providers[id] = map[string]any{
					"baseUrl": req.BaseURL,
					"api":     "openai-completions",
					"apiKey":  req.Credential,
					"models":  models,
				}
				return nil
			}); err != nil {
				return err
			}
			settings := filepath.Join(filepath.Dir(req.ConfigPath), "settings.json")
			return applyJSONMerge(settings, func(root map[string]any) error {
				root["defaultProvider"] = id
				root["defaultModel"] = req.Model
				return nil
			})
		},
```

#### 10. No synchronization around `SwitchManager.Switch`. Both `internal/tui/ai_tab.go` (via `switchCmd()` returning a `tea.Cmd`…

- 位置：`internal/llm/switch.go:554-555`
- 优先级：P0

No synchronization around `SwitchManager.Switch`. Both `internal/tui/ai_tab.go` (via `switchCmd()` returning a `tea.Cmd` that calls `sm.Switch`) and `cmd/mcp_llm.go` (added in this change) can drive `Switch` concurrently with each other or with a CLI invocation. There is no `sync.Mutex` anywhere in `internal/llm/`. Interleaving `adapter.Apply` (which writes `<path>.senv-bak`) with another goroutine's `restoreFromBackup` (which reads and then overwrites `<path>.senv-bak` before the rename) can destroy the only good copy and leave both agents pointing at half-written configs. Either add a process-wide mutex inside `SwitchManager` (keyed on absolute config path so different agents don't block each other) or document explicitly that concurrent switching for the same agent is unsupported and have `Switch` detect/reject it.

**现有代码**

```
// Switch 执行完整切换：校验 → 解密凭据 → 适配器写回 → 指针更新（失败回滚）。
func (sm *SwitchManager) Switch(agentID, providerAlias, model string) (*SwitchOutput, error) {
```

#### 11. `llm_providers` is a new top-level encrypted collection, but the storage lifecycle only knows the existing collections. …

- 位置：`internal/storage/llm_provider.go:9`
- 优先级：P0

`llm_providers` is a new top-level encrypted collection, but the storage lifecycle only knows the existing collections. `rekeyPreflight` has no `llm_providers` case, so any provider causes password changes/rekey to fail with an unknown-identity error; `HasOrphanedData` also ignores this directory, so initializing a vault with only provider files can mint a new key and permanently orphan the existing ciphertext. `CheckConsistency` likewise omits provider files, allowing a provider-only key/desynchronization failure to be reported as healthy. Register this collection alongside the other encrypted collections in orphan detection, consistency probing, and rekey classification, and cover the password-change path with a test.

**现有代码**

```
const LLMProviderDirName = "llm_providers"
```

**建议改法**

```
const LLMProviderDirName = "llm_providers" // register with all encrypted collection lifecycle scans
```

#### 12. Validation is only enforced on save; nothing in `LoadLLMProvider`/`LoadLLMProviderWithKey` re-runs `ValidateLLMProvider`…

- 位置：`internal/storage/llm_provider.go:44-50`
- 优先级：P0

Validation is only enforced on save; nothing in `LoadLLMProvider`/`LoadLLMProviderWithKey` re-runs `ValidateLLMProvider`. A tampered or partial ciphertext that decrypts to JSON with `BaseURL` outside http(s, or with an empty `Models` slice, is happily handed to `ProviderManager.GetProvider`/`ListProviders`, then consumed at `internal/llm/switch.go:590` (BaseURL written into agent config) and through MCP/TUI views. Call `ValidateLLMProvider` (or equivalent) inside `LoadLLMProviderWithKey` after `loadSSHEntry` so the constraints cannot be bypassed by anything other than a brand-new save.

**现有代码**

```
func (m *Manager) LoadLLMProviderWithKey(alias string, cryptoKey []byte) (*LLMProviderEntry, error) {
	var entry LLMProviderEntry
	if err := m.loadSSHEntry(LLMProviderDirName, alias, &entry, cryptoKey); err != nil {
		return nil, err
	}
	return &entry, nil
}
```

**建议改法**

```
func (m *Manager) LoadLLMProviderWithKey(alias string, cryptoKey []byte) (*LLMProviderEntry, error) {
	var entry LLMProviderEntry
	if err := m.loadSSHEntry(LLMProviderDirName, alias, &entry, cryptoKey); err != nil {
		return nil, err
	}
	if err := entry.ValidateLLMProvider(); err != nil {
		return nil, fmt.Errorf("provider %q failed validation: %w", alias, err)
	}
	return &entry, nil
}
```

#### 13. `ValidateLLMProvider` is only invoked from `SaveLLMProviderWithKey`. The corresponding `LoadLLMProviderWithKey`/`LoadLLM…

- 位置：`internal/storage/types.go:126-127`
- 优先级：P0

`ValidateLLMProvider` is only invoked from `SaveLLMProviderWithKey`. The corresponding `LoadLLMProviderWithKey`/`LoadLLMProvider` in `internal/storage/llm_provider.go` returns the deserialized entry without re-running this check, and downstream consumers (`ProviderManager.GetProvider`/`ListProviders`, `internal/llm/switch.go:590`, the MCP and TUI views in `cmd/mcp_llm.go` and `internal/tui/ai_tab.go`) use `BaseURL`/`CredentialRef`/`Models` directly. Anything that can write JSON into the encrypted entry (tampered ciphertext, partial migration, future buggy code path) therefore bypasses the http(s) scheme allowlist and the `DefaultModel ⊆ Models` invariant. Either add a load-side call to `ValidateLLMProvider` from `LoadLLMProviderWithKey`, or document in this comment that the function is advisory only and not a trust boundary.

**现有代码**

```
// ValidateLLMProvider checks cross-field invariants of a provider entry.
func (e *LLMProviderEntry) ValidateLLMProvider() error {
```

**建议改法**

```
// ValidateLLMProvider checks cross-field invariants of a provider entry.
// It must be invoked on both save AND load (see internal/storage/llm_provider.go)
// because the persistence layer is the trust boundary for these constraints.
func (e *LLMProviderEntry) ValidateLLMProvider() error {
```

### P1

#### 1. The CLI never warns that `--base-url` is not pinned to an https-only scheme and trusts whatever the operator types. `int…

- 位置：`cmd/ai_provider.go:57-61`
- 优先级：P1

The CLI never warns that `--base-url` is not pinned to an https-only scheme and trusts whatever the operator types. `internal/llm/provider.go` `parseHTTPURL` accepts both `http://` and `https://`, so a user can `senv ai provider add foo --base-url http://attacker.example.com --key-ref ...` and the resulting provider will happily ship LLM traffic (including prompts and, for non-Codex agents, the API key as `Authorization: Bearer ...`) in cleartext to an attacker-controlled host. Either (a) reject non-https at the CLI layer with a clear error, or (b) require an explicit `--insecure-http` opt-in flag. At minimum, surface a stderr warning when the scheme is `http:`.

**现有代码**

```
		res, err := mgr.AddProvider(llm.AddProviderOptions{
			Alias:           args[0],
			BaseURL:         providerAddBaseURL,
			APIKey:          providerAddAPIKey,
			KeyRef:          providerAddKeyRef,
```

**建议改法**

```
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(providerAddBaseURL)), "http://") {
			fmt.Fprintln(cmd.ErrOrStderr(), "⚠ base URL 使用明文 http，凭据将以明文发送；建议改用 https")
		}
		res, err := mgr.AddProvider(llm.AddProviderOptions{
```

#### 2. `args[0]` is concatenated directly into the audit log subject (`"provider:"+args[0]`) and into the success message (`✓ 已…

- 位置：`cmd/ai_provider.go:150`
- 优先级：P1

`args[0]` is concatenated directly into the audit log subject (`"provider:"+args[0]`) and into the success message (`✓ 已删除 LLM Provider %s`). Because `storage.ValidateName` permits a wide character set and the audit pipeline forwards `target` verbatim into the audit log, an alias containing tab/newline/CR/control characters could break log parsing in `cmd/audit.go` consumers (and any downstream aggregator that splits on whitespace or assumes single-line records). Either reject aliases containing control characters up front (`strings.ContainsAny(alias, "\t\r\n")`) or scrub before logging. The same hazard exists in `cmd/ssh.go` and is worth fixing centrally rather than only here.

**现有代码**

```
		auditOp(session.AuditOpLLMProvider, "provider:"+args[0], true, "remove")
```

**建议改法**

```
		auditAlias := sanitizeAuditSubject("provider:" + args[0])
		auditOp(session.AuditOpLLMProvider, auditAlias, true, "remove")
```

#### 3. agentHomeDir silently falls back to "." when os.UserHomeDir fails (e.g., daemon/cron with no HOME and no passwd entry). …

- 位置：`cmd/ai_switch.go:20-27`
- 优先级：P1

agentHomeDir silently falls back to "." when os.UserHomeDir fails (e.g., daemon/cron with no HOME and no passwd entry). That "." is then joined into agent config paths like filepath.Join(".", ".claude", "settings.json") = ".claude/settings.json" — a relative path. The Switch command will then write the provider config to the current working directory instead of erroring, leaving the user with an unexpected file under cwd and a pointer record pointing at nothing. The Status command will similarly read relative paths. cmd/mcp_install.go already shows the correct pattern: return an error so the caller can surface it. agentHomeDir is called from three places (ai switch/status, mcp llmHome, tui LLMHome), and the resulting relative paths propagate uniformly — fix here to fail fast.

**现有代码**

```
// agentHomeDir 返回 agent 配置写入的 home 目录（测试可经 HOME 覆盖）。
func agentHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
```

**建议改法**

```
// agentHomeDir 返回 agent 配置写入的 home 目录（测试可经 HOME 覆盖）。
// 无法确定 home 时返回错误：filepath.Join(".", ".claude", ...) 会生成相对路径，
// 把 agent 配置写到 cwd 是无意义的副作用，必须让调用方显式失败。
func agentHomeDir() (string, error) {
	return os.UserHomeDir()
}
```

#### 4. SSRF: `client.Get(source)` follows HTTP redirects with Go's default policy (up to 10 hops, any host). The only caller (`…

- 位置：`internal/llm/catalog.go:55`
- 优先级：P1

SSRF: `client.Get(source)` follows HTTP redirects with Go's default policy (up to 10 hops, any host). The only caller (`cmd/ai.go` line 42) passes the `--source` flag value straight through, so a user-supplied URL can 302-redirect to internal endpoints (e.g. `http://169.254.169.254/...`, `http://localhost:...`) and have the body persisted as the catalog cache or used for timing / internal-CSRF probing. Configure a custom `*http.Client` with `CheckRedirect` that pins the host to the original `source` (or refuses redirects entirely) before exposing this to user input.

**现有代码**

```
resp, err := client.Get(source)
```

**建议改法**

```
// 仅在 source 与原始 URL 同主机时跟随跳转，否则直接拒绝。
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		if req.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}
	resp, err := client.Get(source)
```

#### 5. SavePointers relies on `os.MkdirAll(dir, pointerDirMode)` to enforce a 0700 directory, but MkdirAll is a no-op on the mo…

- 位置：`internal/llm/pointer.go:111-114`
- 优先级：P1

SavePointers relies on `os.MkdirAll(dir, pointerDirMode)` to enforce a 0700 directory, but MkdirAll is a no-op on the mode of an existing directory. The pointer path lives at `~/.config/senv/agent-pointers.json` (cmd/ai_switch.go:17), and `~/.config/senv` is also created by other call sites in this codebase with looser modes (e.g., `cmd/mcp_install.go:240` uses `os.MkdirAll(dir, 0o755)`, several tests use `0o755`). If the config dir was created earlier with mode 0755, the pointer file lands in a world-readable/traversable directory, even though the file itself is 0600. This contradicts the package's stated "收紧权限" intent and is inconsistent with the `WriteSensitiveFile` helper added in the 2026-09-02 security-hardening change, which explicitly stats-then-chmods to tighten a loose directory. Either route the pointer write through `storage.WriteSensitiveFile` (stat → loosen-or-tighten as needed → MkdirAll → WriteFile 0600) or replicate the stat-then-chmod sequence here before MkdirAll so the 0700 intent is enforced on every save, not just on first creation.

**现有代码**

```
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, pointerDirMode); err != nil {
		return fmt.Errorf("create pointer dir: %w", err)
	}
```

**建议改法**

```
	dir := filepath.Dir(path)
	if info, statErr := os.Stat(dir); statErr == nil {
		if !info.IsDir() {
			return fmt.Errorf("pointer dir %s is not a directory", dir)
		}
		if info.Mode().Perm() != pointerDirMode {
			if err := os.Chmod(dir, pointerDirMode); err != nil {
				return fmt.Errorf("tighten pointer dir perms: %w", err)
			}
		}
	} else if os.IsNotExist(statErr) {
		if err := os.MkdirAll(dir, pointerDirMode); err != nil {
			return fmt.Errorf("create pointer dir: %w", err)
		}
	} else {
		return fmt.Errorf("stat pointer dir: %w", statErr)
	}
```

#### 6. `parseHTTPURL` does not reject URLs that contain userinfo (`https://user:pass@host`). The userinfo is preserved in `entr…

- 位置：`internal/llm/provider.go:176-182`
- 优先级：P1

`parseHTTPURL` does not reject URLs that contain userinfo (`https://user:pass@host`). The userinfo is preserved in `entry.BaseURL` and then echoed verbatim by `cmd/ai_provider.go` (list and show), `cmd/mcp_llm.go` (MCP JSON), and `internal/tui/ai_tab.go` (TUI detail panel). If a user follows a typical "URL with embedded credentials" pattern from a provider's docs, the credential will leak to stdout, log files, and MCP responses. Reject `u.User != nil` (and ideally normalize the URL via `u.Redacted()` before storing so the stored form also drops any userinfo).

**现有代码**

```
func parseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("not an http(s) URL")
	}
	return u, nil
}
```

**建议改法**

```
func parseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("not an http(s) URL")
	}
	if u.User != nil {
		return nil, fmt.Errorf("base URL must not contain userinfo")
	}
	return u, nil
}
```

#### 7. `os.MkdirAll(dir, 0o700)` does not tighten an existing directory's permissions. If `~/.claude` (or any other agent confi…

- 位置：`internal/llm/switch.go:146-152`
- 优先级：P1

`os.MkdirAll(dir, 0o700)` does not tighten an existing directory's permissions. If `~/.claude` (or any other agent config dir) already exists with `0755` from a prior install, the new `0600` settings.json inside is still discoverable by other local users, and `~/.senv-bak` containing the previous plaintext credentialed config is reachable the same way. The `applyJSONMerge` docstring claims "文件权限收敛 0600" but only the file is tightened — the directory inherits. Either explicitly `os.Chmod(dir, 0o700)` when the directory pre-exists (taking care not to widen permissions on a dir the user intentionally set loosely), or document that the caller must pre-create the directory with the desired mode.

**现有代码**

```
// atomicWriteWithBackup 将原文件复制为 <path>.senv-bak，再以 temp+rename
// 原子替换 path。原文件不存在时跳过备份。
func atomicWriteWithBackup(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
```

#### 8. Duplicate top-level keys after switching. `topLevelKey(e)` only looks at the first line of `e.topLevel` ("model" here), …

- 位置：`internal/llm/switch.go:225-228`
- 优先级：P1

Duplicate top-level keys after switching. `topLevelKey(e)` only looks at the first line of `e.topLevel` ("model" here), but `upsertTopLevelLine(lines, "model", rendered...)` is then called with **both** rendered lines. When the existing config already has `model = "..."` followed by `model_provider = "..."`, the function replaces the matched `model` line with both new lines and keeps the old `model_provider` line, leaving the file with two `model_provider` entries. The same happens on first switch if the user previously edited the file by hand. Note: `TestUpsertTOMLTopLevelAndBlock` invokes `upsertTopLevelLine` once per key (the intended usage), so this regression isn't covered. Fix by either calling `upsertTopLevelLine` per key inside `applyTOMLEdits`, or by iterating `e.topLevel` and keying on each line individually.

**现有代码**

```
		key := topLevelKey(e)
		if key != "" {
			lines = upsertTopLevelLine(lines, key, e.topLevel...)
		}
```

**建议改法**

```
		if key := topLevelKey(e); key != "" {
			lines = upsertTopLevelLine(lines, key, e.topLevel[0])
		}
		for _, extra := range e.topLevel[1:] {
			if k, _, ok := strings.Cut(extra, "="); ok {
				lines = upsertTopLevelLine(lines, strings.TrimSpace(k), extra)
			}
		}
```

#### 9. `url.Parse` accepts userinfo (`https://user:pw@host`) and is otherwise very permissive; combined with the load-time bypa…

- 位置：`internal/storage/types.go:131-134`
- 优先级：P1

`url.Parse` accepts userinfo (`https://user:pw@host`) and is otherwise very permissive; combined with the load-time bypass noted above, a maliciously crafted entry can carry embedded credentials in `BaseURL`. `url.Parse("https://")` returns nil error with empty Host, which the current check catches, but `https://evil.example/` with embedded userinfo passes. Use `u, err := url.ParseRequestURI(e.BaseURL)` plus a `u.Hostname()`/`u.Port()` check, or strip userinfo with `u.User = nil`, before accepting. Apply the same change to the parallel check in `internal/llm/provider.go:parseHTTPURL` so the on-disk and on-write rules agree.

**现有代码**

```
u, err := url.Parse(e.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("provider %q has invalid base URL %q", e.Alias, e.BaseURL)
	}
```

**建议改法**

```
u, err := url.ParseRequestURI(e.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return fmt.Errorf("provider %q has invalid base URL %q", e.Alias, e.BaseURL)
	}
```

### P2

#### 1. Audit log subject on the `add` failure path uses raw `args[0]`, while the success path uses `res.Entry.Alias`. `internal…

- 位置：`cmd/ai_provider.go:68-72`
- 优先级：P2

Audit log subject on the `add` failure path uses raw `args[0]`, while the success path uses `res.Entry.Alias`. `internal/llm/provider.go` normalizes via `strings.TrimSpace(opts.Alias)` before storage and validation, so for any input with leading/trailing whitespace the two log lines for the same logical operation will not correlate (`provider:foo` vs `provider: foo`). This also defeats audit correlation if the alias is ever lowercased or otherwise canonicalized downstream. Use a single normalized form (resolve through `mgr` / a helper, or recompute `strings.TrimSpace(args[0])` on the failure path). Peer command `cmd/ssh.go` consistently uses `args[0]` for both branches; pick one convention for the new command family.

**现有代码**

```
		if err != nil {
			auditOp(session.AuditOpLLMProvider, "provider:"+args[0], false, "add 失败")
			return err
		}
		auditOp(session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "add")
```

**建议改法**

```
		alias := strings.TrimSpace(args[0])
		if err != nil {
			auditOp(session.AuditOpLLMProvider, "provider:"+alias, false, "add 失败")
			return err
		}
		auditOp(session.AuditOpLLMProvider, "provider:"+alias, true, "add")
```

#### 2. Confirmed finding (prior pass): the inline `filepath.Join(configPath, "agent-pointers.json")` duplicates the path resolu…

- 位置：`cmd/mcp.go:129`
- 优先级：P2

Confirmed finding (prior pass): the inline `filepath.Join(configPath, "agent-pointers.json")` duplicates the path resolution in `cmd/ai_switch.go:16` (`agentPointerPath()`) and the same inline expression in `cmd/tui.go:43`. Route both call sites through the existing `agentPointerPath()` helper so the pointer filename and parent directory stay in lockstep with the AI switch/status CLI surface and the spec at `openspec/specs/llm-provider-switch/spec.md` (single canonical location: `~/.config/senv/agent-pointers.json`).

**现有代码**

```
llmPointer: filepath.Join(configPath, "agent-pointers.json"),
```

#### 3. Fetch lacks a `context.Context` parameter. It performs a 30s network call via `client.Get(source)` (which uses `context.…

- 位置：`internal/llm/catalog.go:50-55`
- 优先级：P2

Fetch lacks a `context.Context` parameter. It performs a 30s network call via `client.Get(source)` (which uses `context.Background()` internally), so callers cannot cancel an in-flight request, apply a per-call deadline different from the hardcoded 30s, or propagate tracing. For a CLI invoked from TUI/MCP, a hung fetch can block the caller for the full client timeout with no way to interrupt. The standard Go pattern is `Fetch(ctx context.Context, source string, client *http.Client)` with `http.NewRequestWithContext` + `client.Do`.

**现有代码**

```
// Fetch 从 source 拉取目录并解析校验；client 为 nil 时使用默认 client。
func Fetch(source string, client *http.Client) (*Catalog, error) {
	if client == nil {
		client = defaultCatalogClient
	}
	resp, err := client.Get(source)
```

**建议改法**

```
// Fetch 从 source 拉取目录并解析校验；client 为 nil 时使用默认 client。
func Fetch(ctx context.Context, source string, client *http.Client) (*Catalog, error) {
	if client == nil {
		client = defaultCatalogClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}
	resp, err := client.Do(req)
```

#### 4. LoadPointers and SavePointers perform no internal serialization, and the atomic temp+rename only guards a single write. …

- 位置：`internal/llm/pointer.go:72-93`
- 优先级：P2

LoadPointers and SavePointers perform no internal serialization, and the atomic temp+rename only guards a single write. Per the package doc this file is the single source of truth for status/switch, and the change group ships both an interactive TUI (`internal/tui/ai_tab.go` calls `sm.Switch` → SavePointers, and `sm.Status` → LoadPointers) and a CLI (`cmd/ai_switch.go`'s `ai switch` / `ai status`). A user who keeps the TUI open in one terminal and runs `senv ai switch` in another will have two independent OS processes loading, mutating, and saving the same `~/.config/senv/agent-pointers.json`. The last writer wins and earlier updates silently drop — the pointer will diverge from the agent configs that were successfully written, and `senv ai status` will report a wrong (provider, model) until the next switch. No `flock`/`syscall.Flock`/`FileLock` exists in `internal/llm`, `internal/tui`, or `cmd`. Guard the load-mutate-save window with a process-level `flock(LOCK_EX)` on the pointer file (or a sibling `.lock` file), or document the single-writer invariant in the package comment and refuse to write when an existing lock is held. As written, this race is reachable from shipped entry points and contradicts the "pointer is the only source of truth" invariant.

**现有代码**

```
func LoadPointers(path string) (*PointerFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPointerNotFound
		}
		return nil, fmt.Errorf("read agent pointer file: %w", err)
	}
	var pf PointerFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPointerCorrupt, err)
	}
	if pf.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrPointerCorrupt, pf.Version)
	}
	for id, p := range pf.Agents {
		if _, err := p.SwitchedAtTime(); err != nil {
			return nil, fmt.Errorf("%w: agent %q: %v", ErrPointerCorrupt, id, errors.Unwrap(err))
		}
	}
	return &pf, nil
}
```

**建议改法**

```
// LoadPointers takes a process-level flock(LOCK_SH) on path before reading so
// concurrent writers (e.g. TUI + CLI) cannot interleave a save with this read.
func LoadPointers(path string) (*PointerFile, error) {
	lockPath := path + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, pointerFileMode)
	if err != nil {
		return nil, fmt.Errorf("open pointer lock: %w", err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_SH); err != nil {
		return nil, fmt.Errorf("acquire pointer shared lock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	// ...existing body...
}
```

#### 5. SavePointers renames the temp file without an intervening tmp.Sync(), so the new pointer file may not be on durable stor…

- 位置：`internal/llm/pointer.go:128-139`
- 优先级：P2

SavePointers renames the temp file without an intervening tmp.Sync(), so the new pointer file may not be on durable storage when rename returns. close(2) only releases the file descriptor; the data may still sit in the OS page cache. A crash between os.Rename and the kernel's writeback can leave a zero-byte or partially-written agent-pointers.json even though the rename itself was atomic. Since the package doc declares this file the unique source of truth (`唯一事实源`) for agent status and multiple writers (cmd/ai_switch.go, internal/tui/ai_tab.go) call SavePointers on user-driven switch operations, the lost write silently regresses the pointer to whatever the previous successful save was — or to ErrPointerNotFound. Add tmp.Sync() (with its own error path that removes the temp file) between the existing Chmod and Close, or between Close and Rename.

**现有代码**

```
if err := tmp.Chmod(pointerFileMode); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace agent pointer file: %w", err)
	}
```

**建议改法**

```
if err := tmp.Chmod(pointerFileMode); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace agent pointer file: %w", err)
	}
```

#### 6. Orphan credential on owned→external ref switch: when re-issuing `add <alias> --key-ref env:foo/KEY --force` (or any exte…

- 位置：`internal/llm/provider.go:154-160`
- 优先级：P2

Orphan credential on owned→external ref switch: when re-issuing `add <alias> --key-ref env:foo/KEY --force` (or any external ref) on an existing profile whose current `CredentialRef` is `text:llm-keys/<alias>`, the previous text entry is left behind as an orphan. `opts.APIKey == ""` skips the Set path, so there is no new credential to roll back, and the success path never deletes the prior owned entry. Result: an encrypted key persists in `llm-keys/<alias>` that nothing references, `senv text list llm-keys` keeps showing it, and there is no CLI path to discover or clean it up (the reverse direction is fine: external→owned only writes a new owned entry and leaves the external ref alone, which is by design). Fix: in the mutate closure, if `existing != nil && existing.CredentialRef == OwnedCredentialRef(alias) && entry.CredentialRef != OwnedCredentialRef(alias)`, also delete the previous text entry (idempotent — use storage.DeleteTextFile or ignore "not found" from text.Manager.Delete).

**现有代码**

```
// 先写凭据后写档案；档案失败时回删新建凭据（vault mutation 是锁非事务）。
		if opts.APIKey != "" {
			if err := locked.textManager().Set(LLMKeysGroup, alias, opts.APIKey); err != nil {
				return fmt.Errorf("store credential: %w", err)
			}
		}
		if err := locked.save(alias, entry); err != nil {
```

**建议改法**

```
// 凭据切换语义：owned→owned 用 Set 覆盖；owned→external 删旧；
		// external→owned/无凭据变化 不动外部凭据（不属于本管理器）。
		switch {
		case opts.APIKey != "":
			if err := locked.textManager().Set(LLMKeysGroup, alias, opts.APIKey); err != nil {
				return fmt.Errorf("store credential: %w", err)
			}
		case existing != nil && existing.CredentialRef == OwnedCredentialRef(alias):
			if err := locked.textManager().Delete(LLMKeysGroup, alias); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete previous credential: %w", err)
			}
		}
		if err := locked.save(alias, entry); err != nil {
```

#### 7. `validateCredentialInput` only validates the syntax of `--key-ref` (group/key names) via `ValidateCredentialRef`; it doe…

- 位置：`internal/llm/provider.go:192-205`
- 优先级：P2

`validateCredentialInput` only validates the syntax of `--key-ref` (group/key names) via `ValidateCredentialRef`; it does NOT verify that the referenced credential actually exists in the vault. A user can save a profile whose `CredentialRef` points at a non-existent `env:openai/KEY` or `text:secrets/OPENAI`, and the failure only surfaces when `SwitchManager.Switch` calls `resolveCredential` (in `internal/llm/switch.go`) at runtime — far from the save command, with a misleading "decrypt credential" error message. Consider a best-effort existence check at save time (returning a non-fatal warning, mirroring the stale-catalog warning pattern), or at minimum surface the missing-credential error with a clearer message ("credential env:openai/KEY not found; create it with `senv text set` first").

**现有代码**

```
func validateCredentialInput(opts AddProviderOptions) error {
	hasKey := strings.TrimSpace(opts.APIKey) != ""
	hasRef := strings.TrimSpace(opts.KeyRef) != ""
	switch {
	case hasKey && hasRef:
		return fmt.Errorf("--api-key and --key-ref are mutually exclusive")
	case hasKey:
		return nil
	case hasRef:
		return ValidateCredentialRef(opts.KeyRef)
	default:
		return fmt.Errorf("either --api-key or --key-ref is required")
	}
}
```

**建议改法**

```
func validateCredentialInput(opts AddProviderOptions) error {
	hasKey := strings.TrimSpace(opts.APIKey) != ""
	hasRef := strings.TrimSpace(opts.KeyRef) != ""
	switch {
	case hasKey && hasRef:
		return fmt.Errorf("--api-key and --key-ref are mutually exclusive")
	case hasKey:
		return nil
	case hasRef:
		return ValidateCredentialRef(opts.KeyRef)
	default:
		return fmt.Errorf("either --api-key or --key-ref is required")
	}
}

// credentialRefExists returns nil when a `env:` / `text:` ref can be resolved
// in the current vault, or a descriptive error otherwise. Used to warn at save
// time so users do not discover the problem only at `senv ai switch`.
```

#### 8. `GetProvider` detects `os.ErrNotExist` via `errors.Is` and then constructs a fresh `fmt.Errorf` that does NOT wrap the o…

- 位置：`internal/llm/provider.go:269-275`
- 优先级：P2

`GetProvider` detects `os.ErrNotExist` via `errors.Is` and then constructs a fresh `fmt.Errorf` that does NOT wrap the original error. The underlying `loadSSHEntry` chain (`"%s %q not found: %w"`) does preserve `os.ErrNotExist`, so this replacement breaks `errors.Is(err, os.ErrNotExist)` for any future caller that wants to distinguish "missing" from "corrupt/denied". Use `%w` to wrap rather than replace.

**现有代码**

```
func (m *ProviderManager) GetProvider(alias string) (*storage.LLMProviderEntry, error) {
	entry, err := m.load(alias)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("provider %q not found", alias)
	}
	return entry, err
}
```

**建议改法**

```
func (m *ProviderManager) GetProvider(alias string) (*storage.LLMProviderEntry, error) {
	entry, err := m.load(alias)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("provider %q not found: %w", alias, err)
	}
	return entry, err
}
```

#### 9. Same `errors.Is`/replace pattern as `GetProvider`: this branch swaps the storage-level "not found" error (which wraps `o…

- 位置：`internal/llm/provider.go:299-302`
- 优先级：P2

Same `errors.Is`/replace pattern as `GetProvider`: this branch swaps the storage-level "not found" error (which wraps `os.ErrNotExist`) for a fresh string error, breaking sentinel detection. Use `%w` so `errors.Is(err, os.ErrNotExist)` still matches.

**现有代码**

```
		entry, loadErr := locked.load(alias)
		if errors.Is(loadErr, os.ErrNotExist) {
			return fmt.Errorf("provider %q not found", alias)
		}
```

**建议改法**

```
		entry, loadErr := locked.load(alias)
		if errors.Is(loadErr, os.ErrNotExist) {
			return fmt.Errorf("provider %q not found: %w", alias, loadErr)
		}
```

#### 10. Profile is deleted BEFORE its owned credential. If `DeleteLLMProvider` succeeds but the subsequent `textManager().Delete…

- 位置：`internal/llm/provider.go:306-314`
- 优先级：P2

Profile is deleted BEFORE its owned credential. If `DeleteLLMProvider` succeeds but the subsequent `textManager().Delete(LLMKeysGroup, alias)` fails, the credential becomes an orphan: there is no profile pointing at it, no API to list owned credentials, and no recovery command. Either reorder (delete credential first, profile second — same partial-failure shape but the orphan is a profile, which `ListProviders` can surface) or add a sweep helper that lists `llm-keys/*` entries whose owning profile is absent and report them so the user can clean up.

**现有代码**

```
		if err := locked.storage.DeleteLLMProvider(alias); err != nil {
			return err
		}
		if entry.CredentialRef == OwnedCredentialRef(alias) {
			if err := locked.textManager().Delete(LLMKeysGroup, alias); err != nil {
				return fmt.Errorf("delete credential: %w", err)
			}
			credRemoved = true
		}
```

**建议改法**

```
		// Delete credential first so a partial failure leaves a dangling profile
		// (visible via ListProviders) rather than an orphan credential that no
		// listing path can discover.
		owned := entry.CredentialRef == OwnedCredentialRef(alias)
		if owned {
			if err := locked.textManager().Delete(LLMKeysGroup, alias); err != nil {
				return fmt.Errorf("delete credential: %w", err)
			}
			credRemoved = true
		}
		if err := locked.storage.DeleteLLMProvider(alias); err != nil {
			return err
		}
```

#### 11. RemoveProvider treats a missing credential as an error and returns it to the caller after the profile is already gone. `…

- 位置：`internal/llm/provider.go:309-314`
- 优先级：P2

RemoveProvider treats a missing credential as an error and returns it to the caller after the profile is already gone. `text.Manager.Delete` calls `loadTextFile` first and returns the wrapped `os.ErrNotExist` when the text entry has already been removed (e.g. via `senv text delete`, or via the orphan-on-update path above). At that point `DeleteLLMProvider` has already succeeded, so the user sees "delete credential: ... not found" and an `auditOp(..., false, "remove 失败")` audit entry even though the profile is gone. The retry then fails with "provider not found", leaving the operator confused about whether the operation succeeded. Fix: ignore `errors.Is(err, os.ErrNotExist)` for the cleanup delete (or use the already-idempotent `storage.DeleteTextFile` directly), so a missing owned credential is treated as success.

**现有代码**

```
if entry.CredentialRef == OwnedCredentialRef(alias) {
			if err := locked.textManager().Delete(LLMKeysGroup, alias); err != nil {
				return fmt.Errorf("delete credential: %w", err)
			}
			credRemoved = true
		}
```

**建议改法**

```
if entry.CredentialRef == OwnedCredentialRef(alias) {
			if err := locked.textManager().Delete(LLMKeysGroup, alias); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete credential: %w", err)
			}
			credRemoved = true
		}
```

#### 12. No fsync on the temp file before `os.Rename`, and the rename is not followed by a parent-dir fsync. The package comment …

- 位置：`internal/llm/switch.go:169-186`
- 优先级：P2

No fsync on the temp file before `os.Rename`, and the rename is not followed by a parent-dir fsync. The package comment claims "temp+rename 原子替换", but `os.Rename` is only atomic on the directory entry; without `tmp.Sync()` (or `f.Sync()`) before close, a power loss between the rename and the page cache flush can leave an empty / zero-length / partially-written file at `~/.codex/config.toml` etc. Combined with the `restoreFromBackup` chain below, the only good copy can be lost. Add `tmp.Sync()` before `tmp.Close()`, and ideally `dirF, _ := os.Open(dir); dirF.Sync(); dirF.Close()` after the rename to durably commit the directory entry. (This is not switch.go-specific — no other writer in the repo fsyncs either — but the docstring claims atomicity so this is the file to fix first.)

**现有代码**

```
if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
```

#### 13. Rollback error silently dropped on pointer-load failure. When `LoadPointers` returns a non-`ErrPointerNotFound` error (e…

- 位置：`internal/llm/switch.go:599-607`
- 优先级：P2

Rollback error silently dropped on pointer-load failure. When `LoadPointers` returns a non-`ErrPointerNotFound` error (e.g. permission denied, partial write, malformed JSON that happens to unmarshal), `_ = restoreFromBackup(configPath)` swallows the restore error. The user only sees "load agent pointers: <err>" while the agent config may still hold the just-written plaintext API key and the real rollback failure (e.g. backup file also gone) is hidden. This is asymmetric with the `SavePointers` branch below, which threads `rbErr` into the returned error.

**现有代码**

```
	pf, err := LoadPointers(sm.pointerPath)
	if err != nil {
		if errors.Is(err, ErrPointerNotFound) {
			pf = &PointerFile{}
		} else {
			_ = restoreFromBackup(configPath)
			return nil, fmt.Errorf("load agent pointers: %w", err)
		}
	}
```

**建议改法**

```
	pf, err := LoadPointers(sm.pointerPath)
	if err != nil {
		if errors.Is(err, ErrPointerNotFound) {
			pf = &PointerFile{}
		} else if rbErr := restoreFromBackup(configPath); rbErr != nil {
			return nil, fmt.Errorf("load agent pointers: %w; restore config also failed: %v", err, rbErr)
		} else {
			return nil, fmt.Errorf("load agent pointers: %w (config restored)", err)
		}
	}
```

#### 14. DeleteLLMProvider routes through `deleteSSHEntry`, which calls `validateSSHIdentity(kind, name)` where `kind = "LLM prov…

- 位置：`internal/storage/llm_provider.go:62-66`
- 优先级：P2

DeleteLLMProvider routes through `deleteSSHEntry`, which calls `validateSSHIdentity(kind, name)` where `kind = "LLM provider"`. That helper prefixes the error as `"invalid SSH "+kind`, so a bad alias surfaces to the CLI as e.g. `invalid SSH LLM provider "../bad": ...` — both the "SSH" label and the user-facing message are wrong for an LLM profile. The same helper also wraps delete failures with `fmt.Errorf("failed to delete SSH %s %q", kind, ...)`. Either thread `validateLLMProviderIdentity`/`entryKindForDir(LLMProviderDirName)` through `deleteSSHEntry`, or duplicate a tiny `deleteLLMEntry` helper that uses the correct kind — `loadSSHEntry` already gets this right via `entryKindForDir`.

**现有代码**

```
// DeleteLLMProvider removes one provider profile. A missing alias is not an
// error, matching the existing idempotent entry deletion behavior.
func (m *Manager) DeleteLLMProvider(alias string) error {
	return m.deleteSSHEntry(LLMProviderDirName, alias, "LLM provider")
}
```

**建议改法**

```
// DeleteLLMProvider removes one provider profile. A missing alias is not an
// error, matching the existing idempotent entry deletion behavior.
func (m *Manager) DeleteLLMProvider(alias string) error {
	return m.deleteLLMEntry(LLMProviderDirName, alias, "LLM provider")
}

func (m *Manager) deleteLLMEntry(dir, name, kind string) error {
	if err := validateEntryIdentity(kind, name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.deleteLLMEntry(dir, name, kind) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := removeManagedFile(root, dir, name+ConfigFileSuffix); err != nil {
		return fmt.Errorf("failed to delete %s %q: %w", kind, name, err)
	}
	return nil
}
```

#### 15. Hardcoded "SSH" prefix left in the error message. After generalizing sshKindForDir to entryKindForDir (and dropping "SSH…

- 位置：`internal/storage/ssh.go:168`
- 优先级：P2

Hardcoded "SSH" prefix left in the error message. After generalizing sshKindForDir to entryKindForDir (and dropping "SSH" from sibling messages), this encrypt-failure message still reads "failed to encrypt SSH %s entry" — for an LLM provider save it becomes "failed to encrypt SSH llm_providers entry", which is wrong. Drop the "SSH" literal so it matches the sibling "failed to serialize %s entry" message on line 164.

**现有代码**

```
		return fmt.Errorf("failed to encrypt SSH %s entry: %w", dir, err)
```

**建议改法**

```
		return fmt.Errorf("failed to encrypt %s entry: %w", dir, err)
```

#### 16. deleteSSHEntry still validates via validateSSHIdentity, which prepends "invalid SSH " to the kind. When DeleteLLMProvide…

- 位置：`internal/storage/ssh.go:209-212`
- 优先级：P2

deleteSSHEntry still validates via validateSSHIdentity, which prepends "invalid SSH " to the kind. When DeleteLLMProvider calls this with kind="LLM provider", an invalid alias surfaces as "invalid SSH LLM provider %q", which is misleading. Use validateEntryIdentity (the kind-neutral helper introduced in this diff) so SSH and non-SSH callers share the same shape and the message no longer hardcodes "SSH".

**现有代码**

```
func (m *Manager) deleteSSHEntry(dir, name, kind string) error {
	if err := validateSSHIdentity(kind, name); err != nil {
		return err
	}
```

**建议改法**

```
func (m *Manager) deleteSSHEntry(dir, name, kind string) error {
	if err := validateEntryIdentity(kind, name); err != nil {
		return err
	}
```

#### 17. Hardcoded "SSH" prefix left in the deletion error message. With DeleteLLMProvider passing kind="LLM provider", the wrapp…

- 位置：`internal/storage/ssh.go:222`
- 优先级：P2

Hardcoded "SSH" prefix left in the deletion error message. With DeleteLLMProvider passing kind="LLM provider", the wrapped error becomes "failed to delete SSH LLM provider %q", which contradicts the design doc's stated goal of removing the SSH prefix from shared helper messages. Drop the "SSH" literal to match the load entry error wording.

**现有代码**

```
		return fmt.Errorf("failed to delete SSH %s %q: %w", kind, name, err)
```

**建议改法**

```
		return fmt.Errorf("failed to delete %s %q: %w", kind, name, err)
```

#### 18. On a failed switch the previous success notice is left in `t.notice`, so the right pane continues to render `✓ ... → ...…

- 位置：`internal/tui/ai_tab.go:124-129`
- 优先级：P2

On a failed switch the previous success notice is left in `t.notice`, so the right pane continues to render `✓ ... → ...` while the bottom bar shows the failure error — misleading on retry-after-failure. Clear `t.notice` (or set it to a failure message) before returning the `errMsg` command so the UI does not contradict itself.

**现有代码**

```
case aiSwitchResultMsg:
		if msg.err != nil {
			// 失败走顶层错误横幅；指针与配置由 SwitchManager 保证不变。
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
```

**建议改法**

```
case aiSwitchResultMsg:
		if msg.err != nil {
			// 失败走顶层错误横幅；指针与配置由 SwitchManager 保证不变。
			// 清空 notice，避免上一条成功提示与本次失败同时显示造成误导。
			t.notice = ""
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
```

#### 19. In `aiFlowConfirm`'s `y`/`enter` branch the handler resets `t.flow = aiFlowNone` *before* returning the `switchCmd()` cl…

- 位置：`internal/tui/ai_tab.go:184-186`
- 优先级：P2

In `aiFlowConfirm`'s `y`/`enter` branch the handler resets `t.flow = aiFlowNone` *before* returning the `switchCmd()` closure. Because the actual `Switch()` runs inside the bubbletea command, `InputMode()` returns false for the entire async window, so global shortcuts (digits/tab/`q`/etc.) are re-enabled. The user can press `s` and start a second switch flow, or press `r` to reload `t.providers`, while the first `Switch` is still writing to the agent config and pointer file. The two flows then race in the `aiSwitchResultMsg` / `aiLoadedMsg` handlers and overwrite each other's `t.notice` and `t.rows`. Keep `t.flow != aiFlowNone` (or introduce an `aiFlowPending`) until the `aiSwitchResultMsg` for this switch is processed, so `InputMode()` stays true while the I/O is in flight.

**现有代码**

```
	case "y", "enter":
		t.flow = aiFlowNone
		return t, t.switchCmd()
```

**建议改法**

```
	case "y", "enter":
		// Keep t.flow != aiFlowNone until aiSwitchResultMsg arrives so that
		// InputMode() stays true and global shortcuts (s/r/digits) cannot
		// race the in-flight Switch.
		return t, t.switchCmd()
```

### P3

#### 1. `cat.Counts()` before `Save` does not add real validation: `llm.Fetch` already calls `Parse`, which runs `validateProvid…

- 位置：`cmd/ai.go:42-55`
- 优先级：P3

`cat.Counts()` before `Save` does not add real validation: `llm.Fetch` already calls `Parse`, which runs `validateProviders` on the same payload, so by the time we reach this line the catalog is already valid and `Counts()` cannot fail in practice. The "validate-then-save" comment is misleading; the order is correct (no save on Fetch failure) but the extra `Counts()` call provides no additional guard.

→ Suggestion: drop the `Counts()` call here and just call it once in the print path, or remove the misleading comment.

**现有代码**

```
cat, err := llm.Fetch(source, nil)
		if err != nil {
			return err
		}
		providers, models, err := cat.Counts()
		if err != nil {
			return err
		}
		// 先拉取并校验、后落盘，保证失败时旧缓存原样保留。
		if err := llm.Save(catalogCachePath(), cat); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "已刷新模型目录：%d 个 provider，%d 个 model（来源 %s）\n",
			providers, models, cat.Source)
```

**建议改法**

```
cat, err := llm.Fetch(source, nil)
		if err != nil {
			return err
		}
		// 先拉取并校验、后落盘（Fetch 内部已校验），保证失败时旧缓存原样保留。
		if err := llm.Save(catalogCachePath(), cat); err != nil {
			return err
		}
		providers, models, err := cat.Counts()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "已刷新模型目录：%d 个 provider，%d 个 model（来源 %s）\n",
			providers, models, cat.Source)
```

#### 2. All non-`ErrCacheNotFound` errors from `llm.Load` (including permission denied, I/O errors, JSON corruption, version mis…

- 位置：`cmd/ai.go:71-77`
- 优先级：P3

All non-`ErrCacheNotFound` errors from `llm.Load` (including permission denied, I/O errors, JSON corruption, version mismatch) are collapsed into the same `"模型目录缓存损坏"` message. A user who hit a `permission denied` on `~/.config/senv/cache/models-dev.json` would be told the cache is corrupt and asked to rerun `senv ai refresh`, which would hit the same permission error and look like the tool is broken.

→ Suggestion: branch on `errors.Is(err, llm.ErrCacheCorrupt)` to give a distinct message, and leave the catch-all for genuine I/O errors (e.g., `"无法读取模型目录缓存（%v）；请检查文件权限或重新执行 senv ai refresh"`).

**现有代码**

```
cat, err := llm.Load(catalogCachePath())
		if errors.Is(err, llm.ErrCacheNotFound) {
			return fmt.Errorf("本地暂无模型目录缓存，请先执行 senv ai refresh")
		}
		if err != nil {
			return fmt.Errorf("模型目录缓存损坏（%v）；请重新执行 senv ai refresh", err)
		}
```

**建议改法**

```
cat, err := llm.Load(catalogCachePath())
		if errors.Is(err, llm.ErrCacheNotFound) {
			return fmt.Errorf("本地暂无模型目录缓存，请先执行 senv ai refresh")
		}
		if errors.Is(err, llm.ErrCacheCorrupt) {
			return fmt.Errorf("模型目录缓存损坏（%v）；请重新执行 senv ai refresh", err)
		}
		if err != nil {
			return fmt.Errorf("无法读取模型目录缓存（%v）；请检查文件权限或重新执行 senv ai refresh", err)
		}
```

#### 3. `Long:` help text (lines 41-44) describes the model set as "the union of models from --catalog-provider (models.dev cach…

- 位置：`cmd/ai_provider.go:46-50`
- 优先级：P3

`Long:` help text (lines 41-44) describes the model set as "the union of models from --catalog-provider (models.dev cache) and custom --model values" but does not mention: (1) `--api-key` and `--key-ref` are mutually exclusive and one is required; (2) the model set is deduplicated and sorted alphabetically (the storage shape is `[m1,m2,...]` not insertion order); (3) `--default-model`, if set, must appear in the union. These are enforced by `validateCredentialInput` / `assembleModels` / default-model check in `provider.go` but not advertised in `--help`.

**现有代码**

```
Long: `Save an LLM provider profile with base URL, credential reference and
model set. The model set is the union of models from --catalog-provider
(models.dev cache) and custom --model values. The credential is provided via
--api-key (stored into the reserved vault text group) or --key-ref (reference
an existing env/text entry).`,
```

#### 4. `auditOp` is invoked before `getAIProviderManager()` completes its prompt. If `getAIProviderManager()` itself fails (vau…

- 位置：`cmd/ai_provider.go:52-61`
- 优先级：P3

`auditOp` is invoked before `getAIProviderManager()` completes its prompt. If `getAIProviderManager()` itself fails (vault unavailable, wrong password retries exhausted, etc.), no audit record is written. This is the right call for a missing manager (nothing meaningful to log) but is worth noting for completeness: failed invocations that never reach the provider manager (e.g., user aborts password prompt with Ctrl+C / EOF) leave no audit trail. Confirm whether session-level audit requires recording these aborted attempts at the CLI layer.

**现有代码**

```
RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		res, err := mgr.AddProvider(llm.AddProviderOptions{
			Alias:           args[0],
			BaseURL:         providerAddBaseURL,
			APIKey:          providerAddAPIKey,
			KeyRef:          providerAddKeyRef,
```

#### 5. The success message at line 74 prints `res.Entry.Alias` (trimmed) while the failure message at line 73 prints `args[0]` …

- 位置：`cmd/ai_provider.go:68-81`
- 优先级：P3

The success message at line 74 prints `res.Entry.Alias` (trimmed) while the failure message at line 73 prints `args[0]` (untrimmed). This mirrors the audit-log asymmetry already flagged in confirmed finding #3 — same root cause: alias whitespace normalization happens inside `AddProvider`, not at the CLI boundary. Recommend either trimming `args[0]` at the CLI boundary (and rejecting empty after trim) or having `AddProvider` return the canonical alias on failure too.

**现有代码**

```
if err != nil {
			auditOp(session.AuditOpLLMProvider, "provider:"+args[0], false, "add 失败")
			return err
		}
		auditOp(session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "add")
		for _, w := range res.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", w)
		}
		detail := res.Entry.DefaultModel
		if detail == "" {
			detail = "无"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ 已保存 LLM Provider %s（%d 个模型，默认 %s）\n",
			res.Entry.Alias, len(res.Entry.Models), detail)
```

#### 6. CLI-level help text and human-facing messages here mix English flag descriptions with Chinese status/audit messages (`"暂…

- 位置：`cmd/ai_provider.go:100`
- 优先级：P3

CLI-level help text and human-facing messages here mix English flag descriptions with Chinese status/audit messages (`"暂无 LLM Provider..."`, `"凭据为外部引用，已保留"`, `"add 失败"`). The other audit-log sites (`cmd/env.go`, `cmd/text.go`, `cmd/ssh.go`, `cmd/sync.go`) use English detail strings uniformly; this file introduces the first Chinese audit details and the first Chinese success messages in the CLI surface. Pick one language for `auditOp(...detail...)` and stdout/stderr messages to keep log-aggregation rules locale-independent; if localization is the goal, route through the existing i18n mechanism rather than hard-coding strings.

**现有代码**

```
		fmt.Fprintln(cmd.OutOrStdout(), "暂无 LLM Provider，使用 senv ai provider add 添加")
```

**建议改法**

```
		fmt.Fprintln(cmd.OutOrStdout(), "no LLM providers configured; use 'senv ai provider add' to create one")
```

#### 7. `aiProviderListCmd` formats output with `\t` column separators (line 106). `securefs.ValidateSegment` only rejects `\x00…

- 位置：`cmd/ai_provider.go:105-106`
- 优先级：P3

`aiProviderListCmd` formats output with `\t` column separators (line 106). `securefs.ValidateSegment` only rejects `\x00:/\\` in alias names, so a tab character passes validation. An alias containing a literal tab would split across the column boundary and break `senv ai provider list` output. The natural mitigation is to refuse whitespace-containing aliases at `AddProvider` time so list/show/remove all reject them uniformly, or to render the alias with %q / escape tabs here.

**现有代码**

```
fmt.Fprintf(out, "%s\t%s\t模型数 %d\t默认 %s\t目录 %s\n",
				e.Alias, e.BaseURL, len(e.Models), orDash(e.DefaultModel), orDash(e.CatalogProvider))
```

#### 8. Existing test `TestAIProviderFullLifecycle` (`cmd/ai_provider_test.go`) does not cover: (1) alias with leading/trailing …

- 位置：`cmd/ai_provider.go:171-173`
- 优先级：P3

Existing test `TestAIProviderFullLifecycle` (`cmd/ai_provider_test.go`) does not cover: (1) alias with leading/trailing whitespace (would exercise the audit-log asymmetry — confirmed finding #3); (2) alias containing tab/newline characters (would exercise the list-output column separator issue flagged above); (3) `--base-url` with `http://` scheme (would exercise the HTTPS-pinning gap — confirmed finding #5); (4) empty `--api-key` + non-empty `--key-ref` (would exercise `validateCredentialInput` mutual-exclusion). Test gap only — recommend deterministic unit tests for each.

**现有代码**

```
aiProviderAddCmd.Flags().StringVar(&providerAddBaseURL, "base-url", "", "provider base URL (https)")
aiProviderAddCmd.Flags().StringVar(&providerAddAPIKey, "api-key", "", "API key (stored encrypted in vault)")
aiProviderAddCmd.Flags().StringVar(&providerAddKeyRef, "key-ref", "", "reference to an existing entry: env:<group>/<key> or text:<group>/<key>")
```

#### 9. Passing `nil` for the provider manager here is intentional (Status() only reads pointers and config paths, no vault acce…

- 位置：`cmd/ai_switch.go:75`
- 优先级：P3

Passing `nil` for the provider manager here is intentional (Status() only reads pointers and config paths, no vault access), but the contract is non-obvious and easy to break: any future change in SwitchManager.Status that touches providerManager will panic. Add a one-line comment here (e.g. `// nil is safe: Status() never resolves providers or credentials`) so a future reader doesn't try to "fix" it.

**现有代码**

```
sm := llm.NewSwitchManager(nil, agentPointerPath(), agentHomeDir())
```

#### 10. The pointer file path is recomputed on every MCP request via `filepath.Join(configPath, "agent-pointers.json")` inside t…

- 位置：`cmd/mcp.go:128-130`
- 优先级：P3

The pointer file path is recomputed on every MCP request via `filepath.Join(configPath, "agent-pointers.json")` inside the request-closure. This duplicates the resolution in `agentPointerPath()` (`cmd/ai_switch.go:16`) and TUI wiring (`cmd/tui.go:43`). Consider sharing a single helper to keep the canonical location (`~/.config/senv/agent-pointers.json` per spec/ADR 0003) in one place — if the path ever changes, every caller must be updated in lockstep.

**现有代码**

```
llm:        llm.NewProviderManagerWithKey(store, key),
			llmPointer: filepath.Join(configPath, "agent-pointers.json"),
			llmHome:    agentHomeDir(),
```

#### 11. `llm_agent_status` only reads the local `agent-pointers.json` file (no vault access), so calling `m.pullBeforeRead()` he…

- 位置：`cmd/mcp_llm.go:60-62`
- 优先级：P3

`llm_agent_status` only reads the local `agent-pointers.json` file (no vault access), so calling `m.pullBeforeRead()` here triggers AutoPull (potentially network I/O for the server provider) on a path that does not depend on remote vault state. For consistency with the design intent ("pointer is local state, never requires unlocking the vault"), this should drop the pre-read pull — the peer `llmProviderList` legitimately needs it because it reads encrypted profiles, but status does not.

**现有代码**

```
func (m *managers) llmAgentStatus(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	sm := llm.NewSwitchManager(m.llm, m.llmPointer, m.llmHome)
```

**建议改法**

```
func (m *managers) llmAgentStatus(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	// Status only reads the local pointer file (no vault access), so no AutoPull.
	sm := llm.NewSwitchManager(m.llm, m.llmPointer, m.llmHome)
```

#### 12. PointerFile.Set accepts (agentID, provider, model) without validating that provider and model are non-empty, and LoadPoi…

- 位置：`internal/llm/pointer.go:59-69`
- 优先级：P3

PointerFile.Set accepts (agentID, provider, model) without validating that provider and model are non-empty, and LoadPointers only validates SwitchedAt, not Provider/Model. A caller that invokes Set with an empty string (e.g., a partial switch propagating an unset field) silently persists a broken record, and a hand-edited JSON file with empty Provider/Model is accepted on load. Status code in switch.go will then report e.g. `provider_alias / gpt-4 (timestamp)` for an unset provider, and downstream catalog/provider lookups in switch.go (LookupAgent, entry lookup) will route on the empty string and silently fail or hit a wrong provider. Add an explicit non-empty check in Set (return an error from a new signature `Set(agentID, provider, model string) error`) and reject entries with empty Provider/Model during the LoadPointers validation loop, returning ErrPointerCorrupt.

**现有代码**

```
// Set 记录或覆盖 agent 指针，时间戳取当前时刻。
func (pf *PointerFile) Set(agentID, provider, model string) {
	if pf.Agents == nil {
		pf.Agents = map[string]AgentPointer{}
	}
	pf.Agents[agentID] = AgentPointer{
		Provider:   provider,
		Model:      model,
		SwitchedAt: time.Now().Format(time.RFC3339),
	}
}
```

**建议改法**

```
// Set 记录或覆盖 agent 指针，时间戳取当前时刻。
func (pf *PointerFile) Set(agentID, provider, model string) error {
	if agentID == "" {
		return errors.New("agent id is empty")
	}
	if provider == "" || model == "" {
		return fmt.Errorf("pointer for %q: provider and model must be non-empty", agentID)
	}
	if pf.Agents == nil {
		pf.Agents = map[string]AgentPointer{}
	}
	pf.Agents[agentID] = AgentPointer{
		Provider:   provider,
		Model:      model,
		SwitchedAt: time.Now().Format(time.RFC3339),
	}
	return nil
}
```

#### 13. LoadPointers's per-agent validation produces a duplicated "agent pointer file corrupt" prefix. SwitchedAtTime returns `f…

- 位置：`internal/llm/pointer.go:87-91`
- 优先级：P3

LoadPointers's per-agent validation produces a duplicated "agent pointer file corrupt" prefix. SwitchedAtTime returns `fmt.Errorf("%w: bad switched_at %q: %v", ErrPointerCorrupt, p.SwitchedAt, err)` which wraps ErrPointerCorrupt via %w and embeds the time.Parse error with %v. LoadPointers then does `fmt.Errorf("%w: agent %q: %v", ErrPointerCorrupt, id, errors.Unwrap(err))`. Because errors.Unwrap on the inner error returns ErrPointerCorrupt itself (it is the first wrapped target in the chain), the outer %w wraps the sentinel a second time and %v prints the sentinel's literal string into the message, yielding e.g. `agent pointer file corrupt: agent "id": agent pointer file corrupt: bad switched_at "x": parse error ...`. The underlying time.ParseError is also lost because SwitchedAtTime used %v instead of %w. errors.Is still matches the sentinel, but the diagnostic is misleading and the parse cause is irrecoverable. Replace with `fmt.Errorf("agent %q: %w", id, err)` (preserving the full chain from SwitchedAtTime) or restructure so the parse error is wrapped as the cause.

**现有代码**

```
	for id, p := range pf.Agents {
		if _, err := p.SwitchedAtTime(); err != nil {
			return nil, fmt.Errorf("%w: agent %q: %v", ErrPointerCorrupt, id, errors.Unwrap(err))
		}
	}
```

**建议改法**

```
	for id, p := range pf.Agents {
		if _, err := p.SwitchedAtTime(); err != nil {
			return nil, fmt.Errorf("agent %q: %w", id, err)
		}
	}
```

#### 14. This error message mixes Chinese (`（孤儿凭据未清理：%s）`) with English, breaking the otherwise consistent English error contract…

- 位置：`internal/llm/provider.go:163`
- 优先级：P3

This error message mixes Chinese (`（孤儿凭据未清理：%s）`) with English, breaking the otherwise consistent English error contract used by `AddProvider`'s other paths and making the string hard to grep for English-locale users. Use English ("orphan credential not cleaned up: %s") or move the parenthetical hint to a separate warning channel.

**现有代码**

```
				return fmt.Errorf("save provider: %w（孤儿凭据未清理：%s）", err, OwnedCredentialRef(alias))
```

**建议改法**

```
				return fmt.Errorf("save provider: %w (orphan credential not cleaned up: %s)", err, OwnedCredentialRef(alias))
```

#### 15. Same mixed-language issue: this warning string contains Chinese characters (`模型目录缓存已超过 7 天...建议执行 senv ai refresh`) inco…

- 位置：`internal/llm/provider.go:247-249`
- 优先级：P3

Same mixed-language issue: this warning string contains Chinese characters (`模型目录缓存已超过 7 天...建议执行 senv ai refresh`) inconsistent with the English warnings/errors elsewhere. Translate to English so users with `LANG=en_*` can grep and parse it.

**现有代码**

```
			warnings = append(warnings, fmt.Sprintf(
				"模型目录缓存已超过 7 天（拉取于 %s），建议执行 senv ai refresh",
				at.Local().Format("2006-01-02")))
```

**建议改法**

```
			warnings = append(warnings, fmt.Sprintf(
				"model catalog cache is older than 7 days (fetched %s); run `senv ai refresh`",
				at.Local().Format("2006-01-02")))
```

#### 16. `upsertTopLevelLine` is O(N) on the slice and builds three full intermediate slices per call (lines[:existing], lines[:i…

- 位置：`internal/llm/switch.go:224-232`
- 优先级：P3

`upsertTopLevelLine` is O(N) on the slice and builds three full intermediate slices per call (lines[:existing], lines[:insertAt], lines[insertAt:]). On every `applyTOMLEdits` invocation, we now scan the entire file up to the first header once per edit. For a kimi switch with three edits and a real-world config containing many top-level keys or comments, this is wasteful — but more importantly, `applyTOMLEdits` is called even for unchanged keys because `topLevelKey` always returns the first key in `e.topLevel`. For codex (lines 358-385) both `model` and `model_provider` end up in the same edit's `topLevel` slice, and `topLevelKey` returns only `"model"`; if `model_provider` already exists in the file, the upsert will replace the `model` line and then the next render will be the unmodified `model_provider` line sitting adjacent. This works in practice but means the function relies on the two keys always being inserted in the same edit, which is fragile if a future caller splits them. Either compute per-key replacement in a loop, or document and assert that `e.topLevel` always contains exactly one entry.

**现有代码**

```
for _, e := range edits {
		key := topLevelKey(e)
		if key != "" {
			lines = upsertTopLevelLine(lines, key, e.topLevel...)
		}
		if e.header != "" {
			lines = upsertTOMLBlock(lines, e.header, e.block)
		}
	}
```

#### 17. Error-wrapping inconsistency loses the primary cause. The dual-failure path uses `%v` for the primary `err` (`fmt.Errorf…

- 位置：`internal/llm/switch.go:609-614`
- 优先级：P3

Error-wrapping inconsistency loses the primary cause. The dual-failure path uses `%v` for the primary `err` (`fmt.Errorf("save pointers: %v; restore config also failed: %w", err, rbErr)`) so callers cannot `errors.Is(err, ErrPointerNotFound)` or any other sentinel wrapped by `SavePointers`. `Status()`'s warning similarly uses `%v` for the pointer parse error, hiding it from any caller that wants to log/inspect the sentinel. Use `%w` for both so the original chain is preserved — the format string can still show both via the `%w` + a wrapped secondary.

**现有代码**

```
if err := SavePointers(sm.pointerPath, pf); err != nil {
		if rbErr := restoreFromBackup(configPath); rbErr != nil {
			return nil, fmt.Errorf("save pointers: %v; restore config also failed: %w", err, rbErr)
		}
		return nil, fmt.Errorf("save pointers: %w (config restored)", err)
	}
```

#### 18. The diff intentionally converts the decrypt/not-found/parse error messages in loadSSHEntry (and listSSHEntries' identity…

- 位置：`internal/storage/ssh.go:164`
- 优先级：P3

The diff intentionally converts the decrypt/not-found/parse error messages in loadSSHEntry (and listSSHEntries' identity check) to use entryKindForDir(dir) so non-SSH collections render friendly kind names like "LLM provider" instead of the raw directory name. The matching serialize error on line 164 was missed: it still interpolates dir ("llm_providers"/"keypairs"/"hosts"). For an LLM provider save that fails to marshal, the user sees "failed to serialize llm_providers entry: ..." rather than the "LLM provider" wording used everywhere else in this helper. Recommend switching to entryKindForDir(dir) for consistency with the other messages touched by this same diff.

**现有代码**

```
return fmt.Errorf("failed to serialize %s entry: %w", dir, err)
```

**建议改法**

```
return fmt.Errorf("failed to serialize %s entry: %w", entryKindForDir(dir), err)
```

#### 19. The half-width computation in `View()` is dead/confusing: `half := t.width / 2; if half < 20 { half = max(t.width/2, 1) …

- 位置：`internal/tui/ai_tab.go:299-302`
- 优先级：P3

The half-width computation in `View()` is dead/confusing: `half := t.width / 2; if half < 20 { half = max(t.width/2, 1) }` can never widen the right pane — `max(t.width/2, 1)` is always ≥ `t.width/2`, so the branch is a no-op for `t.width >= 2` and only upgrades `half` from 0 to 1 when `t.width <= 1`. Either drop the branch or implement the apparent intent (e.g. give the right pane more room on narrow terminals) so the next reader doesn't have to prove the branch is inert.

**现有代码**

```
	half := t.width / 2
	if half < 20 {
		half = max(t.width/2, 1)
	}
```

**建议改法**

```
	half := t.width / 2
	if half < 1 {
		half = 1
	}
```
