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
	// TypeKey renders the transport type key ("type" in JSON configs,
	// "transport" in TOML configs) on remote entries. Targets that auto-detect
	// the transport and have no documented type key set this false.
	TypeKey bool
	// Reason explains a missing capability, for export plan errors.
	Reason string
}

// ResolveConfigPath resolves the target's config path for a scope.
func (t Target) ResolveConfigPath(home, scope string) string {
	return t.ConfigPath(home, scope)
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
			Remote: RemoteRender{
				HTTP: true, SSE: true, TypeKey: true,
				Reason: "codex mcp_servers entries have no headers key; put the token into the url or use codex mcp login",
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
			ID:             "pi",
			Name:           "PI",
			Format:         FormatJSON,
			ConfigPath:     func(home, _ string) string { return filepath.Join(home, ".pi", "config.json") },
			JSONServersKey: "mcpServers",
			Note:           "Restart PI for the server to load.",
			Remote: RemoteRender{
				Reason: "pi has no built-in MCP support (extension adapters only); remote entries are not verified",
			},
		},
	}
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
