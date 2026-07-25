package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// agentFormat enumerates the configuration file formats installers can write.
type agentFormat int

const (
	formatJSON agentFormat = iota // Claude/Cursor/ZCode/Kimi/PI style: mcpServers in JSON
	formatTOML                    // Codex style: [mcp_servers.<name>] in TOML
)

// mcpServerSpec is the canonical description of the senv MCP server that gets
// embedded in every target agent's config. command is resolved to an absolute
// path at install time so the agent can spawn it regardless of its own PATH.
type mcpServerSpec struct {
	Command string
	Args    []string
	Env     map[string]string
}

// defaultServerSpec builds the spec, resolving the senv binary to an absolute
// path when possible. When resolve is false (e.g. --print), the command stays
// as "senv" for readability in pasted snippets.
func defaultServerSpec(resolve bool) mcpServerSpec {
	cmd := "senv"
	if resolve {
		if abs, err := absExecutable(); err == nil && abs != "" {
			cmd = abs
		}
	}
	return mcpServerSpec{Command: cmd, Args: []string{"mcp", "serve"}}
}

// absExecutable returns the absolute path to the running senv binary, or an
// error if it cannot be determined.
func absExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	return exe, nil
}

// agentTarget describes one supported agent and how to install into it.
type agentTarget struct {
	// id is the canonical identifier passed to `senv mcp install <id>`.
	id string
	// display name shown in listings.
	name string
	// format of the config file.
	format agentFormat
	// configPath returns the absolute config file path. scope is "user" or
	// "project"; targets that ignore scope treat both as user.
	configPath func(home, scope string) string
	// jsonServersKey is the dotted path within the JSON config under which MCP
	// servers live (e.g. "mcpServers"). Used only for formatJSON.
	jsonServersKey string
	// tomlTableName is the table name for the server (e.g. "mcp_servers").
	// Used only for formatTOML.
	tomlTableName string
	// note is extra guidance printed after install (e.g. "restart Cursor").
	note string
}

// supportedAgents is the registry of installable agents, keyed by id.
func supportedAgents() []agentTarget {
	return []agentTarget{
		{
			id:             "claude-code",
			name:           "Claude Code",
			format:         formatJSON,
			configPath:     func(home, _ string) string { return filepath.Join(home, ".claude.json") },
			jsonServersKey: "mcpServers",
			note:           "Restart Claude Code (or run `claude`) for the server to load.",
		},
		{
			id:             "claude-desktop",
			name:           "Claude Desktop",
			format:         formatJSON,
			configPath:     claudeDesktopConfigPath,
			jsonServersKey: "mcpServers",
			note:           "Quit and reopen Claude Desktop to load the server.",
		},
		{
			id:     "cursor",
			name:   "Cursor",
			format: formatJSON,
			configPath: func(home, scope string) string {
				if scope == "project" {
					return ".cursor/mcp.json"
				}
				return filepath.Join(home, ".cursor", "mcp.json")
			},
			jsonServersKey: "mcpServers",
			note:           "Restart Cursor (or reload the window) for the server to load.",
		},
		{
			id:            "codex",
			name:          "Codex (OpenAI)",
			format:        formatTOML,
			configPath:    func(home, _ string) string { return filepath.Join(home, ".codex", "config.toml") },
			tomlTableName: "mcp_servers",
			note:          "Restart Codex for the server to load.",
		},
		{
			id:             "zcode",
			name:           "ZCode",
			format:         formatJSON,
			configPath:     func(home, _ string) string { return filepath.Join(home, ".zcode", "config.json") },
			jsonServersKey: "mcpServers",
			note:           "Restart ZCode for the server to load.",
		},
		{
			id:             "kimi",
			name:           "Kimi CLI",
			format:         formatJSON,
			configPath:     func(home, _ string) string { return filepath.Join(home, ".kimi", "mcp.json") },
			jsonServersKey: "mcpServers",
			note:           "Restart Kimi CLI for the server to load.",
		},
		{
			id:             "pi",
			name:           "PI",
			format:         formatJSON,
			configPath:     func(home, _ string) string { return filepath.Join(home, ".pi", "config.json") },
			jsonServersKey: "mcpServers",
			note:           "Restart PI for the server to load.",
		},
	}
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

// findAgent looks up an agent by id (case-insensitive).
func findAgent(id string) (agentTarget, bool) {
	lower := strings.ToLower(id)
	for _, a := range supportedAgents() {
		if strings.ToLower(a.id) == lower {
			return a, true
		}
	}
	return agentTarget{}, false
}

// resolveConfigPath returns the target's config path for the given scope,
// expanding "~" if home is empty (defensive; callers pass a real home).
func (a agentTarget) resolveConfigPath(home, scope string) string {
	return a.configPath(home, scope)
}

// supportedAgentIDs returns the list of agent ids, for listing/errors.
func supportedAgentIDs() []string {
	agents := supportedAgents()
	ids := make([]string, len(agents))
	for i, a := range agents {
		ids[i] = a.id
	}
	return ids
}

// formatAgentList renders the agent table for `senv mcp install` with no args.
func formatAgentList() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Supported agents:\n")
	for _, a := range supportedAgents() {
		fmt.Fprintf(&b, "  %-16s %s\n", a.id, a.name)
	}
	fmt.Fprintf(&b, "\nRun: senv mcp install <agent> [--scope user|project] [--print]")
	return b.String()
}
