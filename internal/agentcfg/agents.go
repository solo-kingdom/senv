// Package agentcfg owns the coding-agent config targets senv writes into and
// the format-specific merge primitives those writes need. Both
// `senv mcp install` (senv's own MCP server) and `senv mcp export` (user
// profiles) go through this package so the two write paths cannot drift.
package agentcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Format enumerates the configuration file formats installers and exporters
// can write.
type Format int

const (
	// FormatJSON is the Claude/Cursor/ZCode/Kimi/PI style: mcpServers in JSON.
	FormatJSON Format = iota
	// FormatTOML is the Codex style: [mcp_servers.<name>] in TOML.
	FormatTOML
)

// Target describes one supported agent and how to write into it.
type Target struct {
	// ID is the canonical identifier passed to `senv mcp install <id>`.
	ID string
	// Name is the display name shown in listings.
	Name string
	// Format is the config file format.
	Format Format
	// ConfigPath returns the absolute config file path. scope is "user" or
	// "project"; targets that ignore scope treat both as user.
	ConfigPath func(home, scope string) string
	// JSONServersKey is the dotted path within the JSON config under which MCP
	// servers live (e.g. "mcpServers"). Used only for FormatJSON.
	JSONServersKey string
	// TOMLTableName is the table name for the server (e.g. "mcp_servers").
	// Used only for FormatTOML.
	TOMLTableName string
	// Note is extra guidance printed after a write (e.g. "restart Cursor").
	Note string
	// Prerequisite is an external component the user must have for what senv
	// writes to take effect (nil when the agent reads its config natively).
	// senv attempts to install it best-effort before writing; see
	// internal/agentext. A failed install is reported and never blocks the
	// write, so install and export output echo it too.
	Prerequisite *Prerequisite
	// Remote describes which remote (http/sse) entries this target accepts.
	// Only transports verified against the agent's current documentation are
	// enabled; anything else must produce an explicit plan error rather than a
	// config the agent may silently fail to load.
	Remote RemoteRender
}

// RemoteRender is a target's verified capability for remote MCP entries.
type RemoteRender struct {
	// HTTP accepts streamable-HTTP entries.
	HTTP bool
	// SSE accepts legacy SSE entries.
	SSE bool
	// Headers accepts per-entry custom headers on remote entries.
	Headers bool
	// HeaderKey is the config key those headers live under. Empty means the
	// cross-agent default "headers" (the JSON family). Codex spells the same
	// capability "http_headers" in its TOML config, so the key is per-target
	// rather than hardcoded at the render site.
	HeaderKey string
	// TypeKey renders the transport type key ("type" in JSON configs,
	// "transport" in TOML configs) on remote entries. Targets that auto-detect
	// the transport and have no documented type key set this false.
	TypeKey bool
	// Reason explains a missing capability, for export plan errors.
	Reason string
}

// HeadersKey returns the config key custom headers are stored under, falling
// back to the cross-agent "headers" when the target names no other key.
func (t Target) HeadersKey() string {
	if t.Remote.Headers && t.Remote.HeaderKey != "" {
		return t.Remote.HeaderKey
	}
	return "headers"
}

// Prerequisite is an external component a target needs for senv's config to
// take effect: an agent without built-in MCP support reads its config only
// through an extension. senv installs it best-effort (internal/agentext): a
// missing installer, a failed install or a timeout is reported and the config
// write still happens. senv never removes the component afterwards.
type Prerequisite struct {
	// Display is the human-readable requirement echoed in install/export
	// output, e.g. "pi-mcp-adapter extension (`pi install npm:pi-mcp-adapter`)".
	Display string
	// Package is the package name used to detect an existing install in the
	// agent's settings file.
	Package string
	// SettingsPath returns the agent's settings file to inspect for an existing
	// install. Empty disables detection, so senv always attempts the install.
	SettingsPath func(home string) string
	// Command is the installer executable; empty means the target ID.
	Command string
	// Args are the installer arguments.
	Args []string
}

// Normalize returns srv in the form this target stores and reads back. A
// target with no transport type key cannot distinguish http from sse in its
// file, so the transport drops out of the comparable form. Plan and ledger
// checks must normalize both the profile side and the file side, otherwise
// every export of such a target would look stale and flip to drift.
func (t Target) Normalize(srv Server) Server {
	if t.Remote.TypeKey || srv.URL == "" {
		return srv
	}
	srv.Transport = ""
	return srv
}

// ResolveConfigPath resolves the target's config path for a scope.
func (t Target) ResolveConfigPath(home, scope string) string {
	return t.ConfigPath(home, scope)
}

// PrerequisiteDisplay returns the target's human-readable prerequisite, empty
// when it has none.
func (t Target) PrerequisiteDisplay() string {
	if t.Prerequisite == nil {
		return ""
	}
	return t.Prerequisite.Display
}

// Supported is the registry of writable agents, in stable display order.
func Supported() []Target {
	return []Target{
		{
			ID:             "claude-code",
			Name:           "Claude Code",
			Format:         FormatJSON,
			ConfigPath:     func(home, _ string) string { return filepath.Join(home, ".claude.json") },
			JSONServersKey: "mcpServers",
			Note:           "Restart Claude Code (or run `claude`) for the server to load.",
			Remote:         RemoteRender{HTTP: true, SSE: true, Headers: true, TypeKey: true},
		},
		{
			ID:             "claude-desktop",
			Name:           "Claude Desktop",
			Format:         FormatJSON,
			ConfigPath:     claudeDesktopConfigPath,
			JSONServersKey: "mcpServers",
			Note:           "Quit and reopen Claude Desktop to load the server.",
			Remote: RemoteRender{
				Reason: "claude-desktop config files only support stdio servers; add remote servers via the in-app Connectors UI",
			},
		},
		{
			ID:     "cursor",
			Name:   "Cursor",
			Format: FormatJSON,
			ConfigPath: func(home, scope string) string {
				if scope == "project" {
					return ".cursor/mcp.json"
				}
				return filepath.Join(home, ".cursor", "mcp.json")
			},
			JSONServersKey: "mcpServers",
			Note:           "Restart Cursor (or reload the window) for the server to load.",
			Remote:         RemoteRender{HTTP: true, SSE: true, Headers: true, TypeKey: true},
		},
		{
			ID:            "codex",
			Name:          "Codex (OpenAI)",
			Format:        FormatTOML,
			ConfigPath:    func(home, _ string) string { return filepath.Join(home, ".codex", "config.toml") },
			TOMLTableName: "mcp_servers",
			Note:          "Restart Codex for the server to load.",
			// Codex stores remote-entry headers under "http_headers" (verified
			// against codex-cli 0.153.4: `codex mcp get` reports the parsed
			// entries). "bearer_token_env_var" is codex's dedicated shorthand for
			// a single Authorization: Bearer credential, but it names an env var
			// rather than carrying a value, so senv writes the literal header and
			// leaves that key to whoever wants it.
			Remote: RemoteRender{
				HTTP: true, SSE: true, TypeKey: true,
				Headers: true, HeaderKey: "http_headers",
				Reason: "codex remote entries are written as streamable HTTP tables",
			},
		},
		{
			// ZCode reads its config from ~/.zcode/cli/config.json with servers
			// under "mcp.servers" (verified against a live install); the older
			// ~/.zcode/config.json path does not exist in current versions.
			ID:             "zcode",
			Name:           "ZCode",
			Format:         FormatJSON,
			ConfigPath:     func(home, _ string) string { return filepath.Join(home, ".zcode", "cli", "config.json") },
			JSONServersKey: "mcp.servers",
			Note:           "Restart ZCode for the server to load.",
			Remote:         RemoteRender{HTTP: true, Headers: true, TypeKey: true, Reason: "sse entries are not verified for zcode"},
		},
		{
			// Kimi Code CLI reads ~/.kimi-code/mcp.json (verified against a live
			// install); the old ~/.kimi/mcp.json path belongs to a retired
			// product and is never read.
			ID:             "kimi",
			Name:           "Kimi Code",
			Format:         FormatJSON,
			ConfigPath:     func(home, _ string) string { return filepath.Join(home, ".kimi-code", "mcp.json") },
			JSONServersKey: "mcpServers",
			Note:           "Restart Kimi Code for the server to load.",
			Remote:         RemoteRender{HTTP: true, Headers: true, Reason: "sse entry shape is not verified for Kimi Code"},
		},
		{
			// PI has no built-in MCP: the `~/.pi/agent/mcp.json` Pi global
			// override is read by the pi-mcp-adapter extension
			// (github.com/nicobailon/pi-mcp-adapter, verified against its
			// README 2026-09). Servers live under "mcpServers" as
			// {command,args,env} or {url,headers}; there is no transport
			// "type" key (the adapter infers it from command vs url) and SSE
			// is a fallback of url, so http and sse profiles share one shape.
			ID:             "pi",
			Name:           "PI",
			Format:         FormatJSON,
			ConfigPath:     piMCPConfigPath,
			JSONServersKey: "mcpServers",
			Note:           "Run /reload in PI (or restart) to load the server.",
			Prerequisite: &Prerequisite{
				Display:      "pi-mcp-adapter extension (`pi install npm:pi-mcp-adapter`)",
				Package:      "pi-mcp-adapter",
				SettingsPath: func(home string) string { return filepath.Join(PiAgentDir(home), "settings.json") },
				Args:         []string{"install", "npm:pi-mcp-adapter"},
			},
			Remote: RemoteRender{HTTP: true, SSE: true, Headers: true},
		},
	}
}

// PiAgentDir resolves PI's config directory. PI and pi-mcp-adapter both honor
// $PI_CODING_AGENT_DIR (default ~/.pi/agent), including the `~` and `~/...`
// spellings; a relative value stays relative to the process working directory,
// as in the adapter. Only the stock `PI` name is honored — rebranded pi
// distributions derive <NAME>_CODING_AGENT_DIR from their package manifest,
// which senv cannot discover from here.
func PiAgentDir(home string) string {
	dir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR"))
	switch {
	case dir == "":
		return filepath.Join(home, ".pi", "agent")
	case dir == "~":
		return home
	case strings.HasPrefix(dir, "~/"):
		return filepath.Join(home, strings.TrimPrefix(dir, "~/"))
	default:
		return filepath.Clean(dir)
	}
}

// piMCPConfigPath is the Pi global MCP override read by pi-mcp-adapter.
func piMCPConfigPath(home, _ string) string {
	return filepath.Join(PiAgentDir(home), "mcp.json")
}

// RemoteError reports why srv cannot be exported to this target, or nil when
// the target accepts it. stdio entries always pass.
func (t Target) RemoteError(srv Server) error {
	if srv.URL == "" {
		return nil
	}
	transport := srv.Transport
	if transport == "" {
		transport = "http"
	}
	switch transport {
	case "http":
		if !t.Remote.HTTP {
			return fmt.Errorf("%s does not support http MCP entries: %s", t.Name, t.Remote.Reason)
		}
	case "sse":
		if !t.Remote.SSE {
			return fmt.Errorf("%s does not support sse MCP entries: %s", t.Name, t.Remote.Reason)
		}
	default:
		return fmt.Errorf("%s does not support %s MCP entries", t.Name, transport)
	}
	if len(srv.Headers) > 0 && !t.Remote.Headers {
		return fmt.Errorf("%s remote entries do not support headers: %s", t.Name, t.Remote.Reason)
	}
	return nil
}

// Find looks up a target by id (case-insensitive).
func Find(id string) (Target, bool) {
	lower := strings.ToLower(id)
	for _, target := range Supported() {
		if strings.ToLower(target.ID) == lower {
			return target, true
		}
	}
	return Target{}, false
}

// IDs returns the list of agent ids, for listing and error messages.
func IDs() []string {
	targets := Supported()
	ids := make([]string, len(targets))
	for i, target := range targets {
		ids[i] = target.ID
	}
	return ids
}

// ResolveScope validates a scope and returns its canonical value.
func ResolveScope(scope string) (string, error) {
	if scope == "" {
		return "user", nil
	}
	if scope != "user" && scope != "project" {
		return "", fmt.Errorf("invalid scope %q: must be \"user\" or \"project\"", scope)
	}
	return scope, nil
}

// claudeDesktopConfigPath returns the platform-specific Claude Desktop config.
func claudeDesktopConfigPath(home, _ string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json")
	default:
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
	}
}
