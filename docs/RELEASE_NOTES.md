# Release Notes

## Unreleased: security and reliability hardening

This release closes the P1 vault-boundary gaps identified in the 2026-09-03 review.

### Security behavior

- New vaults and successful `senv passwd` operations use PBKDF2-SHA256 with 600,000 iterations. Legacy metadata with a missing or zero `kdf_iterations` remains readable at 100,000 iterations. Explicit current-format values outside 100,000–1,000,000 are rejected before PBKDF2.
- Session caches now use platform-verified secure stores: the macOS Keychain on Darwin (encrypted at rest, silent reads for per-request MCP validation) and an operating-system-verified memory-backed filesystem on Linux (tmpfs/ramfs). Disk-backed or unknown `XDG_RUNTIME_DIR` and fallback filesystems still fail closed for every timeout, including `never`, with actionable guidance. Trusted system symlinks such as `/var` are resolved before validation instead of rejecting macOS runtime paths. A new explicit `senv session start --insecure-cache` escape hatch stores the key unencrypted at 0600 under `${XDG_CACHE_HOME:-~/.cache}/senv/session-<slot>.json` for headless macOS/CI use and prints a prominent warning.
- Rekey is recoverable. Vault access automatically rolls back or completes a safely identifiable interrupted transaction. Ambiguous or failed recovery preserves `.senv-rekey-*` materials, blocks normal access, and reports `unfinished rekey requires recovery` with `senv doctor` guidance.
- MCP authorization is checked on every request against the vault, not a session instance. The fingerprint is `keyHash + saltHash + dataPathHash` (the session ID is audit metadata only), so a running MCP server keeps working after a same-vault `senv session start`; only expiry, `session clear`, or a salt change (`passwd` / rekey) revoke it.

### Session renewal, invalidation reasons, and vault-bound identity

Re-authentication is now demanded only when it is actually required. Invalidation is split into three non-overlapping classes, and only two of them destroy the cache:

- **Expired** — a `duration` session whose `expires_at` has passed. Cache is cleared on next use.
- **Invalidated** — the premise is gone: a `restart` session whose boot ID changed, or a cache belonging to a different vault. Cache is cleared on next use.
- **Unverifiable** — the environment prevented verification (boot ID or secure store unreadable, corrupt or duplicated cache). The cache is **kept**; an actionable error is returned instead of deleting what may be the only key able to decrypt the user's data.

Behavior changes:

- `duration` sessions are no longer invalidated by boot ID changes; only `restart` sessions are. A `duration` session lives until `expires_at` (memory-backed stores still do not survive a reboot).
- Caches are per vault: one slot per normalized data path (`Abs` + `Clean` + symlink resolution of the longest existing prefix), so equivalent `--path` spellings share a session and different vaults never overwrite each other. macOS keychain accounts are now `senv.v1.<slot>`.
- Legacy single-slot caches (`session-<uid>`, `~/.cache/senv/session.json`, keychain `senv.v1`) are adopted when their `dataPathHash` matches the current vault; otherwise they are preserved with a one-time `senv session clear --all` hint. Legacy `timeout_type: "never"` is adopted as `restart`.
- `duration` sessions slide their expiry on every business command that reuses the cached key, capped at `created_at + max(max_lifetime, timeout)` with `session.max_lifetime` defaulting to `24h`. Read-only commands (`session status`, `doctor`) do not renew.
- New `senv session refresh` extends a valid session without ever prompting for a password; on an expired/invalidated/unverifiable session it reports the reason and next step without creating a session or deleting the cache. `senv session start` on a valid session also renews without a password and preserves the existing timeout policy.
- `senv session clear` now clears only the current vault; `--all` clears every slot plus legacy residue.
- Cross-process auto-rebuild is opt-in via `session.auto_start` (default `false`): a one-off password prompt still leaves no session behind. The existing `--insecure-cache` escape hatch remains explicit and prints a warning.
- Audit events `session_expire`, `session_invalidated`, and `session_unverifiable` now record the reason (session ID, timeout type, and reason text only — never keys, salts, or plaintext).

### Breaking plaintext-export default

New plaintext files created by `senv text get -o`, `senv config export`, or `senv config install` now default to `0600`; newly created parent directories default to `0700`. TUI text export remains fixed at `0600`. Existing files with stricter permissions are not widened.

Scripts that intentionally publish non-secret output must opt in per invocation:

```bash
senv text get public:CERT -o ./cert.pem --mode 0644
senv config export public-config --path ./config.yaml --mode 0644
senv config install public-config --mode 0644
```

`--mode` accepts only strict four-digit octal permissions from `0000` through `0777`; special bits and forms such as `600` or `0o600` are rejected. The selected mode is not saved as a future default.

### Upgrade and recovery

1. Upgrade senv on every machine before running `senv passwd`; clients hard-coded to 100,000 iterations cannot unlock a vault upgraded to 600,000.
2. Let the new version finish or recover any active rekey before downgrading. Do not delete rekey journal or sidecar files manually.
3. If automatic recovery fails, keep the recovery files and backups intact, run a current `senv doctor`, and restore matching metadata/data generations rather than forcing an old client to open the vault.
4. Upgrading leaves legacy sessions usable: single-slot caches are adopted per vault, so no re-authentication is needed after the upgrade. Use `senv session clear --all` only when you deliberately want to discard every vault's session.
5. Scripts that relied on `senv session start` revoking an already-running MCP server must switch to `senv session clear`; same-vault `session start` no longer revokes it.
