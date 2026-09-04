# Release Notes

## Unreleased: security and reliability hardening

This release closes the P1 vault-boundary gaps identified in the 2026-09-03 review.

### Security behavior

- New vaults and successful `senv passwd` operations use PBKDF2-SHA256 with 600,000 iterations. Legacy metadata with a missing or zero `kdf_iterations` remains readable at 100,000 iterations. Explicit current-format values outside 100,000–1,000,000 are rejected before PBKDF2.
- Session caches now use platform-verified secure stores: the macOS Keychain on Darwin (encrypted at rest, silent reads for per-request MCP validation) and an operating-system-verified memory-backed filesystem on Linux (tmpfs/ramfs). Disk-backed or unknown `XDG_RUNTIME_DIR` and fallback filesystems still fail closed for every timeout, including `never`, with actionable guidance. Trusted system symlinks such as `/var` are resolved before validation instead of rejecting macOS runtime paths. A new explicit `senv session start --insecure-cache` escape hatch stores the key unencrypted at 0600 under `${XDG_CACHE_HOME:-~/.cache}/senv/session.json` for headless macOS/CI use and prints a prominent warning.
- Rekey is recoverable. Vault access automatically rolls back or completes a safely identifiable interrupted transaction. Ambiguous or failed recovery preserves `.senv-rekey-*` materials, blocks normal access, and reports `unfinished rekey requires recovery` with `senv doctor` guidance.
- MCP authorization is checked on every request. Expiry, `session clear`, session replacement, boot changes, and rekey revoke an already-running MCP server.

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
