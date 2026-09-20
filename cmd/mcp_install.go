package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/agentext"
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

// installInto performs the merge-and-write for one agent target.
func installInto(t agentTarget, scope string, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}
	scope, err = agentcfg.ResolveScope(scope)
	if err != nil {
		return err
	}
	cfgPath := t.ResolveConfigPath(home, scope)
	spec := defaultServerSpec(!printOnly).server()

	// Best-effort prerequisite install: an agent without built-in MCP support
	// (pi) cannot read the config senv writes without its adapter. A failed
	// install is reported and the write still happens.
	if !printOnly {
		if result, attempted := agentext.Ensure(t, home); attempted {
			fmt.Fprintf(out, "  %s\n", result.Message)
		}
	}

	switch t.Format {
	case formatJSON:
		return installJSON(t, cfgPath, spec, printOnly, out)
	case formatTOML:
		return installTOML(t, cfgPath, spec, printOnly, out)
	default:
		return fmt.Errorf("unsupported format for agent %q", t.ID)
	}
}

// installAll installs into every supported agent, collecting per-agent errors
// so a single failure doesn't abort the rest.
func installAll(scope string, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	var errs []string
	for _, t := range supportedAgents() {
		if err := installInto(t, scope, printOnly, out); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", t.ID, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// installJSON reads (or creates) the JSON config, upserts the senv entry under
// the agent's servers key while preserving everything else, then writes back.
func installJSON(t agentTarget, cfgPath string, spec agentcfg.Server, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	root, err := agentcfg.ReadJSONRoot(cfgPath)
	if err != nil {
		return err
	}
	agentcfg.SetJSONServer(root, t.JSONServersKey, "senv", spec, t.Remote.TypeKey)

	if printOnly {
		entry, _ := json.MarshalIndent(map[string]any{
			t.JSONServersKey: map[string]any{"senv": agentcfg.JSONServers(root, t.JSONServersKey)["senv"]},
		}, "", "  ")
		fmt.Fprintf(out, "# %s — add to %s\n%s\n", t.Name, cfgPath, entry)
		printRequirement(out, t)
		return nil
	}

	data, err := agentcfg.EncodeJSON(root)
	if err != nil {
		return err
	}
	if err := agentcfg.WriteWithBackup(cfgPath, data); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Installed senv MCP server into %s\n  %s\n", t.Name, cfgPath)
	if t.Note != "" {
		fmt.Fprintf(out, "  %s\n", t.Note)
	}
	printRequirement(out, t)
	return nil
}

// printRequirement echoes a target's external prerequisite (e.g. an MCP
// adapter extension) so a successful write is not mistaken for a working
// setup.
func printRequirement(out interface{ Write([]byte) (int, error) }, t agentTarget) {
	if display := t.PrerequisiteDisplay(); display != "" {
		fmt.Fprintf(out, "  Requires: %s\n", display)
	}
}

// installTOML handles the Codex-style [mcp_servers.<name>] config. It preserves
// all other tables and only upserts the senv server block.
func installTOML(t agentTarget, cfgPath string, spec agentcfg.Server, printOnly bool, out interface{ Write([]byte) (int, error) }) error {
	block := agentcfg.RenderTOMLServerBlock(t.TOMLTableName, "senv", spec, t.Remote.TypeKey, t.HeadersKey())
	if printOnly {
		fmt.Fprintf(out, "# %s — add to %s\n%s", t.Name, cfgPath, block)
		printRequirement(out, t)
		return nil
	}

	existing, _ := os.ReadFile(cfgPath)
	merged, err := agentcfg.UpsertTOMLServer(string(existing), t.TOMLTableName, "senv", block)
	if err != nil {
		return err
	}
	if err := agentcfg.WriteWithBackup(cfgPath, []byte(merged)); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Installed senv MCP server into %s\n  %s\n", t.Name, cfgPath)
	if t.Note != "" {
		fmt.Fprintf(out, "  %s\n", t.Note)
	}
	printRequirement(out, t)
	return nil
}

// upsertTomlServer is the Codex-table special case of the shared TOML upsert,
// kept for the install-side tests.
func upsertTomlServer(src, name, newBlock string) (string, error) {
	return agentcfg.UpsertTOMLServer(src, "mcp_servers", name, newBlock)
}

func init() {
	mcpCmd.AddCommand(mcpInstallCmd)
	mcpInstallCmd.Flags().StringVar(&mcpInstallScope, "scope", "user", "config scope: user or project (project only honored by some agents)")
	mcpInstallCmd.Flags().BoolVar(&mcpInstallPrint, "print", false, "print the config snippet instead of writing the file")
	mcpInstallCmd.Flags().BoolVar(&mcpInstallAll, "all", false, "install into every supported agent")
}
