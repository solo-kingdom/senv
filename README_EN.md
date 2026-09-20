# Senv

[简体中文](README.md) | English

🔐 **Senv** is a secure, encrypted storage manager for private configuration and environment variables.

## Features

- ✅ **Environment variable management** - Manage environment variables in groups, with get/set/list/export
- ✅ **Text snippet management** - Store long text (keys, certificates, templates, etc.) encrypted, editable in your editor
- ✅ **Cross-references** - env ↔ text support `{{env:group:key}}` / `{{text:group:key}}` references
- ✅ **Config file management** - Store config files encrypted, with create/edit/export
- ✅ **Encrypted storage** - AES-256-GCM + PBKDF2 encryption
- ✅ **Group management** - Control which environment variables take effect via activated groups
- ✅ **Shell integration** - Recommended: `session start` + `eval "$(senv env export --if-session)"`
- ✅ **Editor integration** - Edit config files and text snippets with your system default editor
- ✅ **TUI mode** - Full-screen terminal UI (`senv tui`) to browse/search/edit env/text/config/SSH/AI/MCP Server profiles in one place, with sensitive values masked by default against shoulder surfing
- ✅ **SSH asset management** - Manage existing host profiles and private keys encrypted, with OpenSSH config export, key materialization on disk, local default keypair, host/keypair CRUD and linking in the TUI (keypair rename cascades to hosts), plus read-only MCP queries
- ✅ **LLM model catalog** - `senv ai refresh` fetches the models.dev provider/model catalog and caches it locally for offline lookup (`senv ai catalog status`)
- ✅ **LLM provider management** - `senv ai provider add/edit/rename/list/show/remove` stores AI service profiles encrypted, supports `--api-shape` (openai-chat / openai-responses / anthropic) to declare the API shape, keeps credentials in the vault, assembles model sets automatically from the models.dev catalog, and validates context windows when adding or editing models

## Installation

### One-line install (recommended)

Use the install script to automatically download and install the prebuilt binary for your platform (macOS / Linux, amd64 / arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/solo-kingdom/senv/main/scripts/install.sh | bash
```

The script installs to `~/.local/bin` by default and can be customized via environment variables:

```bash
# Specify the install directory
curl -fsSL https://raw.githubusercontent.com/solo-kingdom/senv/main/scripts/install.sh | INSTALL_DIR=/usr/local/bin bash

# Install a specific version
curl -fsSL https://raw.githubusercontent.com/solo-kingdom/senv/main/scripts/install.sh | VERSION=v0.1.0 bash
```

> The install script verifies SHA256 checksums to ensure the download is intact and trustworthy.

### Build from source

```bash
git clone https://github.com/solo-kingdom/senv.git
cd senv
make build
sudo mv senv /usr/local/bin/
```

After installation, check the version with:

```bash
senv version
```

## Quick Start

### 1. Initialize a project

```bash
# Use default paths
# Config files: ~/.config/senv
# Data files:   ~/.config/senv/data
senv init

# Or specify a custom data path
senv init --path /path/to/data
```

You will be prompted for an encryption password, which is used to encrypt all data. New vaults use PBKDF2-SHA256 with 600,000 iterations; for legacy metadata that is missing or records `kdf_iterations: 0`, senv keeps reading compatibly at 100,000 iterations.

### 2. Manage environment variables

```bash
# Set environment variables in the default group
senv env set DATABASE_URL "postgresql://localhost/mydb"
senv env set API_KEY "sk-1234567890"

# Create and set variables in a specific group
senv env set --group prod DATABASE_URL "postgresql://prod-server/db"
senv env set --group staging DATABASE_URL "postgresql://staging-server/db"

# Get environment variables
senv env get DATABASE_URL              # From the default group
senv env get --group prod DATABASE_URL # From the prod group
senv env get prod:DATABASE_URL         # group:key address, equivalent to the line above

# Shorthand writes (group:key address)
senv prod:API_KEY "sk-xxx"             # Equivalent to senv env set -g prod API_KEY "sk-xxx"
senv env prod:API_KEY "sk-xxx"         # Shorthand via the env subcommand

# List all environment variables
senv env list              # All groups
senv env list --group prod # Only the prod group

# Delete environment variables
senv env delete API_KEY
```

### 3. Manage groups

```bash
# List all groups
senv env group list

# Create a new group
senv env group add production

# Activate a group (its variables take effect on export)
senv env group activate production

# Deactivate a group
senv env group deactivate production
```

### 4. Export environment variables into your shell

Start a session explicitly first, then inject into the shell:

```bash
# Start once after login (lasts until system restart; or set a timeout as needed)
senv session start -t restart

# Export environment variables from all activated groups
eval "$(senv env export --if-session)"

# Add to your shell config (e.g. ~/.bashrc or ~/.zshrc)
# With no session, --if-session skips silently and never prompts for a password
echo 'eval "$(senv env export --if-session)"' >> ~/.zshrc
```

Without a session, `eval $(senv env export)` (stdout captured) **no longer** prompts for the password; instead it tells you to run `senv session start` first. Running `senv env export` directly in an interactive terminal still allows entering the password once temporarily (nothing is persisted unless `session.auto_start` is explicitly enabled).

Sessions are slotted per vault: each data path gets its own cache, so switching projects no longer overwrites the previous vault's session. `duration` sessions slide-renew whenever a business command reuses the key, but never beyond `session.max_lifetime` (default 24h). When a valid session already exists, you can run `senv session start` (keeping the original timeout) or `senv session refresh` to extend it without a password — neither prompts. `senv session status` distinguishes `Active` / `Expired` / `Invalidated` / `Unverifiable` and explains the reason and next step; undecidable caches are kept by default and never silently deleted. Clearing only affects the current vault by default; `senv session clear --all` clears all slots and legacy single-slot leftovers.

Session cache follows Unix filesystem selection: if tmpfs/ramfs can be proven, the cache is written to secure storage; otherwise on Linux `session start` fails closed, while stock Darwin defaults to a disk-based escape hatch with a one-time warning at write time and silent reads/renewals afterward (no clicks needed over remote SSH). On Linux/CI without tmpfs, pass `senv session start --insecure-cache` explicitly (the key is written to disk in plaintext with 0600). Entries written to the login keychain by older versions are no longer read or deleted; run `session start` again. Memory-backed storage never survives a restart, while `duration` sessions on the disk escape hatch expire solely by `expires_at`.

**Note**: the `default` group is activated by default and needs no manual activation.

### 5. Manage text snippets

```bash
# Set text (pass value directly)
senv text -g secrets set SSH_KEY "ssh-rsa AAAA..."

# Set text (import from a file)
senv text -g keys set TLS_CERT --file /path/to/cert.pem

# Set text (pipe from stdin)
cat ~/.ssh/id_rsa | senv text -g keys set SSH_PRIVATE_KEY

# Set text (open an editor, good for long text)
senv text -g templates set CLAUDE_MD
# Opens $VISUAL/$EDITOR/nano/vim; existing content is pre-filled

# Get text
senv text -g secrets get SSH_KEY
senv text get secrets:SSH_KEY          # group:key address, equivalent to the line above

# Shorthand writes (group:key address)
senv secrets:SSH_KEY "ssh-rsa AAAA..." # Equivalent to senv text set -g secrets SSH_KEY "..."
senv text secrets:SSH_KEY "ssh-rsa..." # Shorthand via the text subcommand

# Get text and write to a file (new files default to 0600)
senv text -g keys get TLS_CERT -o /tmp/cert.pem

# Only explicitly loosen permissions for non-secret content that truly needs sharing;
# it never becomes a later default
senv text get public:CERT -o /tmp/cert.pem --mode 0644

# Get text and copy to the clipboard
senv text -g secrets get SSH_KEY --copy

# List text snippets (shows key name, size, updated time)
senv text -g secrets list

# Delete a text snippet
senv text -g secrets delete SSH_KEY

# Manage text groups
senv text group list
senv text group add templates
senv text group delete templates  # Requires confirmation
```

### 6. Cross-references

env and text can reference each other with the `{{type:group:key}}` syntax:

```bash
# Reference text from env
senv env -g prod set DB_URL "postgres://user:{{text:secrets:DB_PASS}}@host/db"

# Reference env from text
senv text -g configs set APP_YAML "database:\n  url: {{env:prod:DATABASE_URL}}"

# Reference syntax
# {{env:key}}           → current group first, default as fallback
# {{env:group:key}}     → a specific env group
# {{text:key}}          → current group first, default as fallback
# {{text:group:key}}    → a specific text group
# \{{env:key}}          → escaped, outputs literal {{env:key}}

# Dereference on read
senv env get DB_URL                # Output as-is (keeps {{...}})
senv env get DB_URL -d             # Output after dereferencing
senv env get DB_URL -d --loose     # Loose mode (unresolved references stay as-is)

# env export dereferences automatically (requires session start; use --if-session in shell rc)
eval "$(senv env export --if-session)"

# text get also supports dereferencing
senv text -g configs get APP_YAML -d
```

### 7. Manage config files

```bash
# Create a config file (import from an existing file)
senv config create database --path ~/.config/myapp/database.json

# Edit a config file (automatically decrypts, edits, re-encrypts)
senv config edit database

# Export a config file to its target path
senv config export database

# Or export to a custom path
senv config export database --path /tmp/database.json

# New files default to 0600; loosen explicitly only when sharing is truly needed
senv config export public-config --path /tmp/config.json --mode 0644

# List all config files
senv config list

# Show config file info
senv config get database

# Delete a config file
senv config delete database
```

### 8. Manage SSH hosts and keypairs

SSH private keys and connection profiles can be stored encrypted in the vault. `keypair` only imports existing private keys and never generates new ones; `host` manages connections through structured fields and can generate OpenSSH config fragments.

```bash
# Import an existing private key; passphrase-encrypted keys can be imported too,
# but senv does not derive the public key
senv keypair import web-key --file ~/.ssh/id_ed25519
senv keypair list

# Reference an existing keypair; one-step import with --key-file ~/.ssh/id_ed25519 --keypair-name web-key
senv host add web --hostname 10.0.0.1 --user deploy --port 2222 \
  --keypair web-key

# Extra OpenSSH options are passed through as key=value
senv host add db --hostname 10.0.0.2 --user deploy \
  --proxy-jump web --attr ForwardAgent=yes --attr ServerAliveInterval=30

# View, edit, delete
senv host list
senv host get web
senv host edit web        # Edit structured fields and extra with $VISUAL/$EDITOR
senv keypair delete web-key
senv host delete web

# Rename a keypair: atomically rewrites identityKey on every referencing host
# within a single mutation
senv keypair rename web-key prod-key

# One-shot export (apply mode): maintains the whole ~/.ssh/senv/ tree per group
# and registers the Include; missing private keys are materialized automatically —
# ssh works right after export with no manual wiring
senv host export
senv host export --group prod     # Rebuild only the prod group fragment
senv host export --output -       # Render fragments to stdout only (no side effects)
senv host unexport                # Revert: remove the Include line + group fragments
                                  # (including _default.conf; never touches vault or keys)
senv keypair prune                # Clean up unreferenced materialized private keys
                                  # (list first, then delete; skips the current default key)

# Materialize a single private key: same path as host export, ~/.ssh/senv/keys/<group>/<name> (with .pub)
senv keypair export web-key
# Local default: Host * fallback (writes groups/_default.conf only; not synced)
senv keypair set-default web-key
senv keypair clear-default
```

The group layout of `~/.ssh/` after export (`groups/` holds group fragments, `keys/` holds materialized private keys with companion `.pub`, ungrouped items go to `_ungrouped`):

```text
~/.ssh/config                    ← one Include ~/.ssh/senv/groups/*.conf line at the top (idempotently registered by senv)
~/.ssh/senv/
├── groups/
│   ├── _default.conf            ← local default keypair (Host *); host apply never rewrites it
│   ├── _ungrouped.conf
│   └── prod.conf
└── keys/
    ├── _ungrouped/
    │   ├── old-key
    │   └── old-key.pub
    └── prod/
        ├── web-key
        └── web-key.pub
```

Security notes:

- After `keypair export` (alias `materialize`), private keys **persist on disk**: the directory is `0700`, private keys `0600`, public keys `.pub` `0644`. Deleting a keypair from the vault does not automatically delete materialized files; use `senv keypair prune` to clean up unreferenced files (except the current local default key).
- `senv keypair set-default` only changes the local `groups/_default.conf` and is **not synced**; `senv host unexport` removes that fragment as well.
- `--attr` / host `extra` is a deliberate OpenSSH pass-through and is written verbatim into exported fragments. Do not put untrusted text into values; watch out for side effects of keywords like `LocalCommand`.
- `senv host export` writes `~/.ssh/config` (only adds/removes senv's own Include line, leaving `~/.ssh/config.senv-bak` before writing) and materializes missing private keys; `--output` is a pure render mode with no side effects.
- `--group` on hosts and keypairs participates in export organization (group fragment ownership and key directories); group names must not contain `/`.
- Deleting a referenced keypair is rejected by default; `--force` clears the hosts' `identityKey` and then deletes the vault record.
- `senv keypair rename <old> <new>` rewrites the `identityKey` of referencing hosts within the same vault mutation; it refuses and writes nothing if the target name already exists.

### 9. TUI mode (full-screen UI)

Start the full-screen terminal UI with `senv tui`: browse, search and edit env/text/config in one interface; fully manage SSH hosts and keypairs (host CRUD and OpenSSH fragment export; keypair import, rename, grouping, deletion, export materialization, set local default); browse LLM provider profiles and switch what each coding agent points to; and manage MCP Server profiles with global-config export/revert to each agent.

```bash
senv tui   # Start the TUI (reuses the session when possible; prompts for password if none)
```

The project must be initialized before starting. With a valid session you enter without a password; otherwise you are prompted (valid for this run only, not written into the session). The TUI is a pure interaction layer — all persistence still goes through the existing encrypted storage.

Startup never waits on the network: the UI renders immediately from the local working copy (locally cached data) while the server fetch completes in the background; if remote changes are applied, the status bar shows "Updated N items from server" and each tab refreshes automatically. `--refresh` forces this background fetch to bypass the throttle window; when offline, local data remains browsable and editable, and sync failures show in the bottom error bar.

#### Keybindings

The bottom bar shows the keybindings for the current context (switching with focus pane / form / confirm / wizard / filter; when Search or the `?` overlay is open, the overlay keymap applies). Navigation and secondary verbs are not in the bottom bar — see `?` for the full list.

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Cycle tabs |
| `1`–`9` | Jump to the corresponding tab in registration order (out-of-range digits ignored; with 9 tabs: `5`=KeyPair, `6`=AI, `7`=MCP, `8`=History, `9`=Audit) |
| `↑` `↓` / `j` `k` | List navigation |
| `←` `→` / `h` `l` | Switch focus between panes (Env / Text / Config / SSH / KeyPair / AI / MCP tabs) |
| `enter` | Open the detail overlay (Config / SSH / AI / MCP) / reveal the current env value in plaintext |
| `v` | Env: toggle the current value plaintext/masked for a single item (re-masks when the cursor moves away); KeyPair: preview the private key on demand |
| `e` | Edit (env=inline input, text/config=vim, SSH=host structured form, KeyPair=keypair grouping, AI=provider form, MCP=profile form) |
| `n` | New entry (SSH host pane=new host, KeyPair pane=import keypair; AI=new provider; MCP=new profile) |
| `d` | Delete (with confirmation); deletes the whole group when focus is on the group pane (Env / Text); in the KeyPair tab, deleting a referenced keypair is rejected with the referrer list — press `F` to force delete and clear references |
| `r` | Rename: rename a group in the group pane, rename key/name in the entry pane (Env / Text / Config / KeyPair; the default group cannot be renamed; keypair rename cascades to host `identityKey` within a single mutation) |
| `m` | Edit metadata (Config tab: group and description, via `config.Manager.SetMeta`) |
| `x` | SSH tab: export OpenSSH fragments (host pane=selected host, group pane focus=all), preview first (scroll with ↑↓/PgUp/PgDn when tall), then `w` to fill the target file and write; the export form rejects paths inside `~/.ssh/senv` (that tree is owned by apply export — it suggests using `A`); MCP tab: export the current profile to the current agent (`X`=all agents), plan page first |
| `A` | SSH tab: apply export (equivalent to `senv host export`) — host pane rebuilds the group of the cursor host, group pane rebuilds the selected group (All=full rebuild plus ghost-fragment cleanup); the confirm dialog lists group fragments / keys to materialize / Include registration / warning counts, `y` to run, `esc`/`n` to cancel, result toast summary. KeyPair tab: materialize the current key (equivalent to `senv keypair export`; after confirmation writes to `~/.ssh/senv/keys/<group>/<name>` 0600 and `<name>.pub` 0644) |
| `u` / `U` | MCP tab: revert the current profile from the current/all agents (plan page confirmation; changed entries confirmed one by one) |
| `t` | Activate/deactivate an env group (Env tab only; default cannot be deactivated) |
| `+` | New group (Env / Text tabs) |
| `i` | Import from file (Text=text snippet, with `group`/`key`/source file path; KeyPair tab=import keypair name + private key path + group) |
| `i` / `u` | Install/uninstall config (`I`/`U` for batch, confirmed on the plan page) |
| `D` | Env tab: toggle dereference view; KeyPair tab: set/unset the current key as the local default (`Host *`) |
| `y` | Copy value to clipboard |
| `x` | Export text to a file |
| `/` | Filter within the current tab (matches key/name case-insensitively; Audit tab matches event type/target/detail) |
| `f` | Audit tab: cycle preset filters (all / operations / sessions) |
| `s` | AI tab: switch the agent selected in the right pane to the provider selected in the left pane (multi-select the Agent model set, all selected on entry → pick the default model → confirm; codex credentials go through environment variables, not written into config) |
| `M` | AI tab: for agents already pointing at a provider, change only the default model, candidates limited to the Agent model set already written for that agent (provider and model set unchanged); if not pointing anywhere, it suggests pressing `s` first |
| `ctrl+r` | Refresh the current tab (`r` on the entry pane = rename) |
| `S` | Global cross-type search overlay: covers Env/Text/Config/SSH/AI/MCP, matches identifiers only (key/name, host alias/hostname, provider alias, MCP alias/command) — never values |
| `?` | Keymap overview overlay (global keys + current tab keys) |
| `esc` | Close overlay / cancel |
| `q` | Quit the TUI (prompts once more if there are pending pushes; press again to exit) |

The SSH tab manages hosts (group sidebar → host list, two panes): `n/e/d` edit hosts (alias, hostname, user, port, proxyJump / identityKey via pickers, group, tags; `extra` via `$EDITOR`), `x` exports OpenSSH fragments, `A` applies the export (same orchestration as CLI `senv host export`; the target is inferred automatically — no more manual entry: host pane rebuilds the whole group fragment of the cursor host, group pane rebuilds the selected group, All=full rebuild with ghost group-fragment cleanup). `A` opens a confirm dialog listing the group fragments to rebuild, keys to materialize, Include registration status and warning counts — `enter`/`y` to run, `esc`/`n` to cancel (no side effects), then a toast summarizes the result (rebuild/materialize/skip/registration/warning counts). Batch export directories and single export target files pointing inside `~/.ssh/senv/` are rejected inline by the form (that tree is fully maintained by apply export; foreign files are ghost-cleaned) — it suggests using `A` or another user-owned path. The host list inline-shows the keypair name and fingerprint digest in use (`key:name(fp)`); editing a host with a non-existent keypair/proxyJump reference fails inline in the form without writing.

The KeyPair tab (a separate tab right after SSH) manages keypairs (group sidebar → keypair list, two panes; group semantics match hosts: All pinned top → alphabetical → "ungrouped" pinned bottom): `i` import (name + private key path + group), `r` rename (cascades to host `identityKey` within a single mutation), `e` edit group, `d` delete (referenced keys are rejected by default with the referrer list; `F` force-deletes and clears references), `A` materialize (after confirmation writes to `~/.ssh/senv/keys/<group>/<name>` 0600 and `<name>.pub` 0644), `D` set/unset the local default (`Host *`, writes `groups/_default.conf`), `enter` details (public key unindented, showing the OpenSSH comment/email), `v` preview the private key on demand (detail overlay, discarded on close); rows inline-show fingerprint digests, reference counts (`referenced by N hosts`) and a `default` badge, with zero-reference rows greyed as "unreferenced". The list never renders private keys; exported fragments follow the existing rule: a dangling `proxyJump` is an error.

The AI tab is likewise an editable two-pane layout: left pane providers (`n` new, `e` edit, `r` rename, `d` delete, `enter` details), right pane agents (`↑↓` to select, `s` switch with the selected provider, `m` change only the default model). The `s` model-set step uses `space` to check/uncheck items one by one, all Provider models selected on entry; an empty set cannot be submitted; then pick the default model (defaulting to the profile's default model) and confirm. Agent rows display the same as `senv ai status`: `provider / default model (N models)`, with a `⚠` drift badge when a model in the pointer is no longer in the profile (the check compares pointer vs. profile only; it does not parse the agent's config file). The provider form covers base_url, `api_shape`, catalog source, model set, default model and credential source; credentials default to picking from existing env/text entries, or "create own credentials" writes via a masked input into `text:llm-keys/<alias>` — plaintext never enters TUI state or rendered text. Enum/reference fields list candidates below when focused; left/right keys cycle through them.

The MCP tab is another independent two-pane layout: left pane MCP Server profiles (`n` new, `e` edit with alias read-only, `d` delete without auto-revert, `enter` details), right pane all export target agents (same as `senv mcp export`, including claude-desktop / cursor) with the current profile's not-exported / exported / drift status. `x`/`u` target the current profile × current agent, `X`/`U` the current profile × all agents; a plan page comes first (marking "plaintext env" and paths, never rendering parsed values), `y`/`enter` confirms before writing, `esc`/`n` cancels. Drift defaults to skip; the plan page `F` force-overrides; reverting changed entries asks `y/n` one by one. The list is a summary showing only env key counts (remote shows only `scheme://host`); the detail overlay shows full field values: url with query, headers as `Name: Value`, env as `KEY=value` (template references verbatim). In forms, values only appear while editing in `$EDITOR`. `senv mcp install` / `serve` / `list-tools`, `--print`, `--scope project` remain CLI-only.

Rename and group management go through atomic renames in the storage layer (a single `renameat`, not "create + delete"): values/content, permissions and timestamps are preserved; rename conflicts fail inline in the form without writing. Multi-field editing (rename, metadata, group name) uses a unified reusable form: `tab`/`↑↓` to switch fields, `enter` to submit, `esc` to cancel (no side effects); on validation failure the form stays open without losing input; the `$EDITOR` loop remains for multi-line/free-form fields.

Panel content is always truncated to fit the width (overlong content ends with `…`; long `base_url`/model lists/paths never wrap); press `enter` to view full content in the detail overlay. Operation results in all tabs go through the bottom status bar: error > warning > success; success messages auto-dismiss after a timeout.

Write operations in the TUI (env/text/config/SSH/AI/MCP) are recorded in the local operation audit (`senv audit`), with no values included. In server mode with `auto_sync` not disabled, the bottom bar persistently shows the pending push count and last sync time; remote changes are fetched in the background on startup (2-second budget, `--refresh` bypasses the throttle window) and writes are pushed asynchronously in the background after completion (2-second budget). Git mode shows none of this status and performs no background fetch.

#### Security design

- **Shoulder-surfing protection**: env values are always masked in lists (`prefix***`); press `v` to reveal a single item in plaintext, and it re-masks as the cursor moves away. SSH/KeyPair lists show only fingerprints and metadata; KeyPair previews the private key on demand with `v` (detail overlay, discarded on close).
- **Search never leaks**: global search (`S`, including SSH host alias/hostname, provider alias, and MCP profile alias/command) and in-tab filtering (`/`) **match identifier fields only — never values, private key contents, credentials, or MCP env values** — avoiding bulk secret exposure in result lists.
- **Reused vim loop**: text/config editing reuses the existing "decrypt → temp file (600) → edit → re-encrypt → delete temp file" flow, with no new attack surface.

> Note: the TUI does not offer `env export` (it exists for shell-startup injection via `eval $(...)`, and a TUI running as a child process cannot eval back into the parent shell). Keep using the command line for export.

### 10. MCP integration (let AI agents call senv)

senv ships a built-in **stdio MCP server** that exposes env/text/backup/config capabilities as tools for local AI agents (Claude Code/Desktop, Cursor, Codex, ZCode, Kimi, PI, etc.) to call directly.

**How it works**: the MCP server runs as a child process of the agent, carrying JSON-RPC over stdin/stdout, and **cannot pop up a password prompt**. Therefore the server reuses senv's session authentication at startup — open a session in the terminal first; afterwards every tool request re-verifies via `keyHash + saltHash + dataPathHash` that the credential is still bound to the same vault (the session ID only goes into the audit log). **Re-running `senv session start` on the same vault does not revoke a running MCP**; only `senv session clear`, expiry, or a salt change (`passwd` / rekey) rejects requests — in that case open a new session and restart the MCP server.

```bash
# 1) Authenticate once in a terminal (30-minute timeout by default; -t restart lasts until reboot)
senv session start

# 2) Write senv into the target agent's config (keeps other servers, backs up .bak automatically)
senv mcp install claude-code     # or cursor / codex / claude-desktop / zcode / kimi / pi
senv mcp install --all           # Write all supported agents in one go

# 3) Restart the agent; the senv_* tools become available
```

Common options:

```bash
senv mcp install cursor --scope project   # Cursor project scope: writes .cursor/mcp.json
senv mcp install codex --print            # Print a paste-ready config snippet only, no file writes
senv mcp list-tools                       # Show the tools exposed via MCP
```

Exposed tools (25 total): `senv_env_get/set/delete/list/export`, `senv_text_get/set/delete/list`, `senv_backup_get/set/delete/list`, `senv_config_list/get/export`, `senv_group_list/add/activate/deactivate`, plus read-only `ssh_host_list/get`, `llm_provider_list`, `llm_agent_status`, `mcp_server_list`. Keys accept the `group:key` shorthand address; env/text `get` supports `decode=true` to dereference `{{env:...}}`/`{{text:...}}` (backup has no decode; `backup list` never returns values).

> SSH MCP tools return only whitelisted host connection metadata and fingerprints — **no tool ever returns private key plaintext**.

> Security note: writing into an agent's config hands senv's read/write capabilities to the model in that agent's context. Use as needed; for sensitive writes, check the audit log (`~/.log/senv/audit.log`).

### 11. Agent Skill (teach coding agents how to use senv)

The repo ships an [Agent Skills](https://skills.sh)-standard skill at `.agents/skills/senv-cli/`: it teaches coding agents to drive the senv CLI/MCP safely and non-interactively (session rules, `group:key` addressing, reference resolution, no implicit group creation, credentials never in argv, etc.). Install it into supported agents (Claude Code, Codex, Cursor, …) with the [skills CLI](https://github.com/vercel-labs/skills):

```bash
npx skills add solo-kingdom/senv --skill senv-cli            # into the current project
npx skills add solo-kingdom/senv --skill senv-cli --global   # user-level, available across projects
```

The skill is maintained in lockstep with the commands under `cmd/`; an installed copy may lag behind the latest release — re-run the command above to update.

## How It Works

### Encryption scheme

- **Algorithm**: AES-256-GCM (authenticated encryption)
- **Key derivation**: PBKDF2-SHA256 (600,000 iterations for new vaults and after a successful `senv passwd`; legacy 100,000 when metadata is missing/0)
- **Salt**: a 32-byte random salt per project
- **Nonce**: a 12-byte random nonce per encryption

### Group activation mechanism

1. The `default` group is always activated
2. Other groups must be activated via `senv env group activate`
3. `senv env export` exports environment variables from activated groups only
4. Activation state is stored in the `settings.json` file

### Data storage

```
~/.config/senv/                 # Config directory
├── metadata.json              # Project metadata (salt, encrypted password hash)
├── settings.json              # User settings (activated groups)
└── config_index.json          # Config file index

~/.config/senv/data/            # Data directory (default; customizable)
├── env_default.json.enc       # default group environment variables (encrypted)
├── env_prod.json.enc          # prod group environment variables (encrypted)
├── texts/                     # Text snippets (encrypted, one file per key)
│   ├── secrets/               # secrets group
│   │   ├── DB_PASS.enc
│   │   └── API_KEY.enc
│   └── keys/                  # keys group
│       ├── SSH.enc
│       └── TLS.enc
├── database.enc               # Config files (encrypted)
├── hosts/                     # SSH host profiles (encrypted, one file per alias)
│   └── web.enc
└── keypairs/                  # SSH private keys (encrypted, one file per name)
    └── web-key.enc

~/.log/senv/                    # Log directory
└── audit.log                  # Audit log
```

## Use Cases

### Case 1: Development environment management

```bash
# Initialize
senv init

# Set development environment variables
senv env set DATABASE_URL "postgresql://localhost/dev"
senv env set REDIS_URL "redis://localhost:6379"

# Create a production group
senv env group add prod
senv env set --group prod DATABASE_URL "postgresql://prod-server/db"
senv env set --group prod REDIS_URL "redis://prod-server:6379"

# Add to shell config
echo 'eval "$(senv env export --if-session)"' >> ~/.zshrc
source ~/.zshrc
```

### Case 2: Encrypted config file management

```bash
# Encrypt a sensitive config file
senv config create aws_credentials \
  --path ~/.aws/credentials \
  --target ~/.aws/credentials

# Export when needed
senv config export aws_credentials

# Or edit the encrypted file directly
senv config edit aws_credentials
```

### Case 3: Multi-project environment management

```bash
# Project A
cd project-a
senv init --path ./.senv-data
senv env set DATABASE_URL "postgres://localhost/project_a"

# Project B
cd ../project-b
senv init --path ./.senv-data
senv env set DATABASE_URL "postgres://localhost/project_b"

# Use within each project (run senv session start first; each data path has its own session slot)
eval "$(senv env export --if-session)"
```

## Security Recommendations

1. **Use a strong password** - Choose a complex password, preferably generated by a password manager
2. **Back up regularly** - Back up the entire data directory (`~/.config/senv/data/`)
3. **Never commit the data directory** - Add the data directory to `.gitignore`
4. **Restrict file permissions** - The data directory is automatically set to 700, files to 600
5. **Secure transfer** - Use encrypted channels when transferring between machines

## FAQ

### Q: What if I forget the password?

A: The password cannot be recovered. If you forget it, all data becomes undecryptable. Recommendations:
- Store the password in a password manager
- Back up the data directory regularly
- Keep a password hint

### Q: How do I sync across multiple machines?

A: You can:
1. Use encrypted cloud storage (e.g. Cryptomator, Syncthing)
2. Manually copy the entire data directory
3. Make sure the same password is used

If you see `metadata and encrypted data are out of sync` after syncing, git/sync tools usually overwrote `metadata.json` without syncing the data files (or vice versa). Run `senv doctor` to locate the out-of-sync files and restore the matching `metadata.json` from version control.

### Q: Where is the data stored?

A:
- **Config files**: stored in `~/.config/senv/` (including metadata.json, settings.json, config_index.json)
- **Data files**: stored in `~/.config/senv/data/` by default; specify another location via `--path`
- **Log files**: stored in `~/.log/senv/audit.log`

This separation means:
1. Config and data files are managed separately, easier to back up and migrate
2. The data path is customizable, supporting sharing data across projects or machines
3. Logs live independently, convenient for auditing and troubleshooting

### Q: How do I change the password?

A: Run `senv passwd`. The command re-salts and re-encrypts all env/text/config with the current 600,000-iteration KDF parameters; even with an unchanged password it upgrades legacy 100,000-iteration vaults. After upgrading, older senv binaries hardcoded to 100,000 iterations cannot unlock the vault — upgrade the clients on all machines first.

`passwd` uses a recoverable transaction. Normal access automatically rolls back or completes decidable unfinished transactions; if the journal is corrupted or the I/O state cannot be judged safely, it keeps the recovery sidecar, blocks normal access and returns `unfinished rekey requires recovery`. In that case do not delete the `.senv-rekey-*` files — follow the error message and use the latest `senv doctor` to investigate and restore from backup.

> Note: if the data directory already contains encrypted files but `metadata.json` was deleted, `senv init` refuses to run, to avoid generating a new key that would make the old ciphertext undecryptable. Restore `metadata.json` from backup/git first.

### Q: It says "wrong password" but the password is correct — what now?

A: Most likely `metadata.json` and the encrypted data files are not from the same key set (multi-machine git sync, an accidental `senv init`, improper merge conflict resolution, etc. can all trigger this). The system reports `metadata and encrypted data are out of sync` rather than a generic wrong-password error. Troubleshooting steps:
1. Run `senv doctor` to see which files are out of sync.
2. Restore the `metadata.json` that matches the data files from version control.
3. Never run `senv session clear` before restoring — the key in the session cache may be the only recovery key that can still decrypt the data.

## Command Reference

### Global options

```
--path string   Data storage path (default ~/.config/senv/data)
                Config files always live in ~/.config/senv
                Log files always live in ~/.log/senv
```

### Command list

#### LLM Provider

```bash
# Enter the API key interactively (no echo; HTTPS required by default)
senv ai provider add acme \
  --base-url https://api.acme.com/v1 \
  --catalog-provider acme \
  --default-model m1

# Provide the API key from stdin in scripts, keeping credentials
# out of argv and shell history
printf '%s' "$ACME_KEY" | senv ai provider add acme \
  --base-url https://api.acme.com/v1 \
  --api-key-stdin \
  --catalog-provider acme \
  --default-model m1

# Local model services that truly need HTTP must be exempted explicitly
senv ai provider add local \
  --base-url http://127.0.0.1:11434/v1 \
  --allow-http \
  --key-ref env:llm/LOCAL_KEY \
  --model qwen3 \
  --model-context qwen3=32768
```

> `senv ai provider add` does not support `--api-key`; use the TTY prompt, `--api-key-stdin`, or `--key-ref`.

> When adding or replacing a model set, every model must have a resolvable context window: `--catalog-provider` reads `limit.context` from models.dev; for custom models or missing catalog fields, provide `--model-context <model>=<tokens>` explicitly, otherwise the command fails and writes nothing. Existing profiles are never force-completed and remain readable; use `senv ai provider edit <alias> --model-context ...` to backfill metadata when needed.

```bash
# Edit in place (alias cannot change; only passed fields change, omitted ones keep their values)
senv ai provider edit acme --base-url https://new.acme.com/v1 --default-model m2

# Rename (cascades to own credentials and local agent pointers; does not rewrite
# the agents' native configs — re-run switch)
senv ai provider rename acme acme-prod

# Declare the API shape: empty means undeclared, inferred from the target agent's protocol family on switch
senv ai provider edit acme --api-shape anthropic
senv ai provider edit acme --api-shape ""     # Clear the field, back to inference

# Backfill or fix context windows for existing custom models
senv ai provider edit acme --model-context m1=1000000 --model-context m2=200000

# Rotate own credentials (TTY prompt; --api-key-stdin in scripts), or switch to an external reference
senv ai provider edit acme --rotate-key
senv ai provider edit acme --key-ref env:llm/ACME_KEY
```

> Once `--api-shape` is declared it becomes a compatibility check for `senv ai switch`: when the shape does not match the target agent's protocol family, the write is rejected with "change the profile shape or switch provider". Valid values: `openai-chat`, `openai-responses`, `anthropic`.

```bash
# Switch a coding agent's pointer: writes all of the provider's models + the profile default model
senv ai switch claude-code acme

# Write a subset and set the starting model (comma-separated or repeated --models, order preserved)
senv ai switch codex acme --models m1,m2 --default-model m2

# Show each agent's current pointer (provider / default model (N models); drift hints when the vault is unlocked)
senv ai status
```

> The switch writes the **Agent model set** through each agent's native mechanism, after which models can be changed in the agent's own model picker: claude-code writes `modelPicker` (replacing the built-in lineup, mapping custom models via `behavesAs`), codex generates `~/.codex/model-catalogs/senv-<alias>.json` and points `model_catalog_json` at it, kimi gets one `[models.*]` entry per model, and pi/opencode get the provider's model table; when pi's existing `enabledModels` is non-empty, this run's default model is also pinned to the top to avoid the first scope item being selected at startup. `--default-model` only overrides the starting model written this time and does not change the profile. Switching cleans up entries and stale catalogs written by the previous senv switch that are no longer needed this time; user-owned entries and files stay untouched. `--model` has been removed: use `--models` for the model set and `--default-model` for the starting model.

```
senv init                          Initialize a project
senv tui                           Start the full-screen TUI (browse/search/edit env·text·config·ssh)
senv env get <key|group:key> [-d]    Get an environment variable (-d dereferences)
senv env set <key|group:key> <value> Set an environment variable
senv env delete <key|group:key>      Delete an environment variable
senv env list [group] [-d]         List environment variables (-d dereferences)
senv env export [--if-session]     Export environment variables to the shell (dereferences automatically; --if-session skips silently with no session)
senv env group list                List all groups
senv env group add <name>          Create a group
senv env group activate <name>     Activate a group
senv env group deactivate <name>   Deactivate a group
senv text set <key|group:key> [value]  Set a text snippet (opens the editor when no value)
senv text get <key|group:key> [-d] [-o path] [--mode 0600]  Get/export a text snippet
senv text delete <key|group:key>         Delete a text snippet
senv text list [group]             List text snippets
senv text group list               List all text groups
senv text group add <name>         Create a text group
senv text group delete <name>      Delete a text group
senv config create <name>          Create a config file
senv config edit <name>            Edit a config file
senv config export <name> [--path path] [--mode 0600]  Export a config file
senv config install [name] [--mode 0600]              Install a config file
senv config list                   List all config files
senv config get <name>             Show config file info
senv config delete <name>          Delete a config file
senv keypair import <name> --file <path> [--force]  Import an existing SSH private key
senv keypair list                  List SSH keypair fingerprints/metadata
senv keypair rename <old> <new>    Rename a keypair, cascading to host identityKey
senv keypair export <name> [--force]       Materialize to ~/.ssh/senv/keys/<group>/<name> (materialize is an alias)
senv keypair set-default <name>            Local Host * default key (writes groups/_default.conf)
senv keypair clear-default                 Unset the local default key
senv keypair delete <name> [--force]       Delete a keypair (referenced keys are rejected by default)
senv host add <alias> [flags]      Create an SSH host profile, optionally linked to a keypair
senv host get <alias>              Show an SSH host profile
senv host edit <alias>             Edit an SSH host profile in an editor
senv host list                     List SSH host profiles
senv host delete <alias>           Delete an SSH host profile
senv host export [--group g]       Apply export (group fragments + materialize + registration; --output renders only)
senv host unexport                 Revert registration and group fragments
senv keypair prune [--force]       Clean up unreferenced materialized private keys
senv ai provider add <alias> [flags]      Save an LLM provider profile (credentials via TTY/--api-key-stdin/--key-ref)
senv ai provider edit <alias> [flags]     Edit a profile in place (alias cannot change; --api-shape declares/clears the API shape)
senv ai provider rename <old> <new>       Rename a profile (cascades to own credentials and pointers; re-run switch)
senv sync                          Sync git/server providers (server conflicts enter a TTY resolver)
senv sync --no-interactive         Print a sanitized conflict summary without the interactive UI
senv sync --accept-remote          Take remote on server conflicts
senv sync --force-push             Take local on server conflicts
senv doctor                        Check metadata vs. data file consistency
senv mcp serve                     Run as a stdio MCP server (for local agents)
senv mcp install <agent>           Write the senv MCP server into an agent config (claude-code/claude-desktop/cursor/codex/zcode/kimi/pi)
senv mcp list-tools                List tools exposed via MCP
```

## Development

### Build

```bash
go build -o senv
```

### Test

```bash
go test ./...
```

### Dependencies

- [github.com/spf13/cobra](https://github.com/spf13/cobra) - CLI framework
- [github.com/charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework (Elm architecture)
- [github.com/charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) - TUI components (textinput, etc.)
- [github.com/charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) - TUI styling
- [golang.org/x/crypto](https://golang.org/x/crypto) - Cryptography (PBKDF2)
- [golang.org/x/term](https://golang.org/x/term) - Terminal password input

## License

MIT License

## Contributing

Issues and Pull Requests are welcome!

## Changelog

### Unreleased

- 🔄 Enhanced `senv sync` server conflict experience: shows revision/size/hash/time summaries, allows comparing plaintext and choosing local/remote per item in the TTY, and manually merging compatible entries via `VISUAL`/`EDITOR`; `--no-interactive` keeps the script path.
- 🔐 See [Release Notes](docs/RELEASE_NOTES.md) for security and compatibility migrations: 600k/legacy KDF, memory-backed session limitations, recoverable rekey, and plaintext export defaulting to `0600` with explicit `--mode`.

### v1.0.0 (2026-03-06)

- ✨ Initial release
- ✅ Environment variable management
- ✅ Config file management
- ✅ AES-256-GCM encryption
- ✅ Group activation mechanism
- ✅ Shell integration
