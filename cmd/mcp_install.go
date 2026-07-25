package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// mcpInstallCmd writes the senv MCP server config into a target agent's config
// file. It preserves existing configuration (including other MCP servers) by
// merging rather than overwriting, and writes a .bak backup before modifying.
var (
	mcpInstallScope string // user | project
	mcpInstallPrint bool   // print the snippet instead of writing
	mcpInstallAll   bool   // install into every supported agent
)

var mcpInstallCmd = &cobra.Command{
	Use:   "install [agent]",
	Short: "Install the senv MCP server into an agent's config",
	Long: `Write the senv MCP server (senv mcp serve) into a target agent's
configuration file. Existing config and other MCP servers are preserved; a
.bak backup is created before the file is modified.

Supported agents: claude-code, claude-desktop, cursor, codex, zcode, kimi, pi.

Examples:
  senv mcp install cursor
  senv mcp install cursor --scope project   # Cursor only: writes .cursor/mcp.json
  senv mcp install codex --print            # print the snippet, don't write
  senv mcp install --all                    # install into every supported agent`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpInstallAll {
			return installAll(mcpInstallScope, mcpInstallPrint, cmd.OutOrStdout())
		}
		if len(args) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), formatAgentList())
			return nil
		}
		target, ok := findAgent(args[0])
		if !ok {
			return fmt.Errorf("unknown agent %q; supported: %s", args[0], strings.Join(supportedAgentIDs(), ", "))
		}
		return installInto(target, mcpInstallScope, mcpInstallPrint, cmd.OutOrStdout())
	},
}

// serverEntryJSON is the per-server object every JSON-format agent expects.
type serverEntryJSON struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// installInto performs the merge-and-write for one agent target.
func installInto(t agentTarget, scope string, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}
	if scope == "" {
		scope = "user"
	}
	if scope != "user" && scope != "project" {
		return fmt.Errorf("invalid scope %q: must be \"user\" or \"project\"", scope)
	}
	cfgPath := t.resolveConfigPath(home, scope)
	spec := defaultServerSpec(!printOnly)

	switch t.format {
	case formatJSON:
		return installJSON(t, cfgPath, spec, printOnly, out)
	case formatTOML:
		return installTOML(t, cfgPath, spec, printOnly, out)
	default:
		return fmt.Errorf("unsupported format for agent %q", t.id)
	}
}

// installAll installs into every supported agent, collecting per-agent errors
// so a single failure doesn't abort the rest.
func installAll(scope string, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	var errs []string
	for _, t := range supportedAgents() {
		if err := installInto(t, scope, printOnly, out); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", t.id, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// installJSON reads (or creates) the JSON config, upserts the senv entry under
// the agent's servers key while preserving everything else, then writes back.
func installJSON(t agentTarget, cfgPath string, spec mcpServerSpec, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	// Load existing config as generic JSON to preserve unknown keys verbatim.
	root := map[string]any{}
	if existing, err := os.ReadFile(cfgPath); err == nil && len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return fmt.Errorf("parse %s: %w", cfgPath, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", cfgPath, err)
	}

	servers, _ := root[t.jsonServersKey].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["senv"] = serverEntryJSON{Command: spec.Command, Args: spec.Args, Env: spec.Env}
	root[t.jsonServersKey] = servers

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if printOnly {
		entry, _ := json.MarshalIndent(map[string]any{
			t.jsonServersKey: map[string]any{"senv": servers["senv"]},
		}, "", "  ")
		fmt.Fprintf(out, "# %s — add to %s\n%s\n", t.name, cfgPath, entry)
		return nil
	}

	if err := writeWithBackup(cfgPath, data); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Installed senv MCP server into %s\n  %s\n", t.name, cfgPath)
	if t.note != "" {
		fmt.Fprintf(out, "  %s\n", t.note)
	}
	return nil
}

// installTOML handles the Codex-style [mcp_servers.<name>] config. It preserves
// all other tables and only upserts the senv server block.
func installTOML(t agentTarget, cfgPath string, spec mcpServerSpec, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	block := renderCodexServerBlock(spec)
	if printOnly {
		fmt.Fprintf(out, "# %s — add to %s\n%s", t.name, cfgPath, block)
		return nil
	}

	existing, _ := os.ReadFile(cfgPath)
	merged, err := upsertTomlServer(string(existing), "senv", block)
	if err != nil {
		return err
	}
	if err := writeWithBackup(cfgPath, []byte(merged)); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Installed senv MCP server into %s\n  %s\n", t.name, cfgPath)
	if t.note != "" {
		fmt.Fprintf(out, "  %s\n", t.note)
	}
	return nil
}

// renderCodexServerBlock renders the TOML block for the senv server, matching
// Codex's [mcp_servers.<name>] convention with command/args/env keys.
func renderCodexServerBlock(spec mcpServerSpec) string {
	var b strings.Builder
	b.WriteString("[mcp_servers.senv]\n")
	fmt.Fprintf(&b, "command = %q\n", spec.Command)
	fmt.Fprintf(&b, "args = [\"%s\"]\n", strings.Join(spec.Args, "\", \""))
	if len(spec.Env) > 0 {
		b.WriteString("[mcp_servers.senv.env]\n")
		for k, v := range spec.Env {
			fmt.Fprintf(&b, "%s = %q\n", k, v)
		}
	}
	return b.String()
}

// upsertTomlServer replaces the [mcp_servers.<name>] table (and its nested
// subtables) in src with newBlock, or appends newBlock if absent. Other tables
// and free-form content are preserved verbatim. This is deliberately simple:
// it splits on top-level table headers ([...] at column 0) and treats any
// [parent.child...] header belonging to <name> as part of the block.
func upsertTomlServer(src, name, newBlock string) (string, error) {
	tableHeader := fmt.Sprintf("[mcp_servers.%s]", name)
	subtablePrefix := fmt.Sprintf("[mcp_servers.%s.", name) // matches [mcp_servers.senv.env]

	lines := strings.Split(src, "\n")
	var out []string
	inBlock := false
	replaced := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeader := strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.HasPrefix(trimmed, "[[")
		if isHeader {
			if trimmed == tableHeader || strings.HasPrefix(trimmed, subtablePrefix) {
				inBlock = true
				if !replaced {
					out = append(out, strings.TrimRight(newBlock, "\n"))
					replaced = true
				}
				continue
			}
			inBlock = false
		}
		if inBlock {
			continue // drop the old block's content
		}
		out = append(out, line)
	}

	result := strings.Join(out, "\n")
	if !replaced {
		// Append: ensure separation from preceding content.
		if result != "" && !strings.HasSuffix(result, "\n\n") {
			if strings.HasSuffix(result, "\n") {
				result += "\n"
			} else {
				result += "\n\n"
			}
		}
		result += newBlock
	}
	// Normalize trailing newline.
	result = strings.TrimRight(result, "\n") + "\n"
	return result, nil
}

// writeWithBackup writes data to path, creating parent dirs as needed and
// backing up any existing file to path.bak first.
func writeWithBackup(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func init() {
	mcpCmd.AddCommand(mcpInstallCmd)
	mcpInstallCmd.Flags().StringVar(&mcpInstallScope, "scope", "user", "config scope: user or project (project only honored by some agents)")
	mcpInstallCmd.Flags().BoolVar(&mcpInstallPrint, "print", false, "print the config snippet instead of writing the file")
	mcpInstallCmd.Flags().BoolVar(&mcpInstallAll, "all", false, "install into every supported agent")
}
