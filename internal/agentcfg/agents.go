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
		},
		{
			ID:             "claude-desktop",
			Name:           "Claude Desktop",
			Format:         FormatJSON,
			ConfigPath:     claudeDesktopConfigPath,
			JSONServersKey: "mcpServers",
			Note:           "Quit and reopen Claude Desktop to load the server.",
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
		},
		{
			ID:            "codex",
			Name:          "Codex (OpenAI)",
			Format:        FormatTOML,
			ConfigPath:    func(home, _ string) string { return filepath.Join(home, ".codex", "config.toml") },
			TOMLTableName: "mcp_servers",
			Note:          "Restart Codex for the server to load.",
		},
		{
			ID:             "zcode",
			Name:           "ZCode",
			Format:         FormatJSON,
			ConfigPath:     func(home, _ string) string { return filepath.Join(home, ".zcode", "config.json") },
			JSONServersKey: "mcpServers",
			Note:           "Restart ZCode for the server to load.",
		},
		{
			ID:             "kimi",
			Name:           "Kimi CLI",
			Format:         FormatJSON,
			ConfigPath:     func(home, _ string) string { return filepath.Join(home, ".kimi", "mcp.json") },
			JSONServersKey: "mcpServers",
			Note:           "Restart Kimi CLI for the server to load.",
		},
		{
			ID:             "pi",
			Name:           "PI",
			Format:         FormatJSON,
			ConfigPath:     func(home, _ string) string { return filepath.Join(home, ".pi", "config.json") },
			JSONServersKey: "mcpServers",
			Note:           "Restart PI for the server to load.",
		},
	}
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
