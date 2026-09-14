package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/session"
)

var mcpImportDryRun bool

// Import outcome actions, as printed per entry.
const (
	importCreate   = "create"
	importConflict = "conflict"
	importFailed   = "failed"
)

var mcpImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import MCP server entries from an agent config file",
	Long: `Create MCP server profiles from the entries of an existing agent
configuration file. JSON configs are read from the "mcpServers" object (the
Claude/Cursor/ZCode/Kimi convention); TOML configs from the
[mcp_servers.<alias>] tables (the Codex convention).

Each entry's transport is detected: an explicit "type" of http/sse wins, a url
without a type is imported as http, and a command imports as stdio. Values are
stored raw, so {{env:...}} references survive the import.

Aliases that already exist in the vault are reported as conflicts and left
alone — import never overwrites. Use --dry-run to preview without writing.

Examples:
  senv mcp import ~/.claude.json --dry-run
  senv mcp import ~/.codex/config.toml`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := mcp.ParseImportFile(args[0])
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("no MCP server entries found in %s", args[0])
		}

		mgr, err := getMCPManager()
		if err != nil {
			return err
		}

		aliases := make([]string, 0, len(entries))
		for alias := range entries {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)

		created, conflicts, failures := 0, 0, 0
		fmt.Println("Import plan:")
		for _, alias := range aliases {
			if _, err := mgr.Get(alias); err == nil {
				conflicts++
				fmt.Printf("  %-24s %-9s already exists\n", alias, importConflict)
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				failures++
				fmt.Printf("  %-24s %-9s — %v\n", alias, importFailed, err)
				continue
			}
			entry, err := mcp.BuildImportEntry(alias, entries[alias])
			if err != nil {
				failures++
				fmt.Printf("  %-24s %-9s — %v\n", alias, importFailed, err)
				continue
			}
			if mcpImportDryRun {
				fmt.Printf("  %-24s %-9s %s\n", alias, importCreate, entry.Transport)
				created++
				continue
			}
			if err := mgr.Add(entry); err != nil {
				failures++
				fmt.Printf("  %-24s %-9s — %v\n", alias, importFailed, err)
				continue
			}
			created++
			fmt.Printf("  %-24s %-9s %s\n", alias, importCreate, entry.Transport)
		}

		label := "Imported"
		if mcpImportDryRun {
			label = "Dry run"
		}
		fmt.Printf("%s: %d created, %d conflict, %d failed (from %s)\n", label, created, conflicts, failures, args[0])
		if mcpImportDryRun {
			return nil
		}
		auditOp(session.AuditOpMCPServer, "mcp:import", failures == 0, fmt.Sprintf("import %d 项 (%d created, %d conflict, %d failed)", len(aliases), created, conflicts, failures))
		if failures > 0 {
			return fmt.Errorf("%d 项导入失败", failures)
		}
		return nil
	},
}

func init() {
	mcpCmd.AddCommand(mcpImportCmd)
	mcpImportCmd.Flags().BoolVar(&mcpImportDryRun, "dry-run", false, "show what would be imported without writing")
}
