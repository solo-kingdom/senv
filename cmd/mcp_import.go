package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
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
		entries, err := readMCPImportFile(args[0])
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
		raws := make(map[string]map[string]any, len(entries))
		for alias, raw := range entries {
			aliases = append(aliases, alias)
			typed, _ := raw.(map[string]any)
			if typed == nil {
				typed = map[string]any{}
			}
			raws[alias] = typed
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
			entry, err := buildMCPImportEntry(alias, raws[alias])
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

// readMCPImportFile loads the server entries of an agent config. TOML is
// detected by extension; everything else is parsed as JSON with a
// "mcpServers" object.
func readMCPImportFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		servers, err := agentcfg.TOMLServers(string(data), "mcp_servers")
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		return servers, nil
	}
	root, err := agentcfg.ReadJSONRoot(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return agentcfg.JSONServers(root, "mcpServers"), nil
}

// buildMCPImportEntry converts one raw config entry into a vault profile,
// detecting the transport. Values are kept raw: templates must survive.
func buildMCPImportEntry(alias string, raw map[string]any) (*storage.MCPServerEntry, error) {
	transport, _ := raw["type"].(string)
	if transport == "" {
		// TOML configs (codex) spell the key "transport".
		transport, _ = raw["transport"].(string)
	}
	if transport == "streamable-http" {
		transport = storage.MCPTransportHTTP
	}
	url, hasURL := raw["url"].(string)
	command, hasCommand := raw["command"].(string)

	switch {
	case transport == storage.MCPTransportHTTP || transport == storage.MCPTransportSSE:
		if !hasURL || url == "" {
			return nil, fmt.Errorf("transport %s but no url", transport)
		}
		headers, err := stringMap(raw, "headers")
		if err != nil {
			return nil, err
		}
		return &storage.MCPServerEntry{Alias: alias, Transport: transport, URL: url, Headers: headers}, nil
	case transport == storage.MCPTransportStdio:
		if !hasCommand || command == "" {
			return nil, fmt.Errorf("transport stdio but no command")
		}
	case transport != "":
		return nil, fmt.Errorf("unsupported transport %q", transport)
	case hasURL && url != "":
		// The de-facto remote shape carries just a url; import it as http.
		headers, err := stringMap(raw, "headers")
		if err != nil {
			return nil, err
		}
		return &storage.MCPServerEntry{Alias: alias, Transport: storage.MCPTransportHTTP, URL: url, Headers: headers}, nil
	case hasCommand && command != "":
	default:
		return nil, fmt.Errorf("entry has neither url nor command")
	}

	env, err := stringMap(raw, "env")
	if err != nil {
		return nil, err
	}
	args, err := stringSlice(raw, "args")
	if err != nil {
		return nil, err
	}
	return &storage.MCPServerEntry{Alias: alias, Transport: storage.MCPTransportStdio, Command: command, Args: args, Env: env}, nil
}

// stringMap reads raw[field] as a map of strings, rejecting non-string values
// instead of silently dropping them: a dropped credential would export a
// server that cannot authenticate.
func stringMap(raw map[string]any, field string) (map[string]string, error) {
	switch value := raw[field].(type) {
	case nil:
		return nil, nil
	case map[string]string:
		return value, nil
	case map[string]any:
		out := make(map[string]string, len(value))
		for key, item := range value {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%q] is not a string", field, key)
			}
			out[key] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s is not a table", field)
	}
}

// stringSlice reads raw[field] as a list of strings, preserving order.
func stringSlice(raw map[string]any, field string) ([]string, error) {
	switch value := raw[field].(type) {
	case nil:
		return nil, nil
	case []string:
		return value, nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s contains a non-string item", field)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s is not a list", field)
	}
}

func init() {
	mcpCmd.AddCommand(mcpImportCmd)
	mcpImportCmd.Flags().BoolVar(&mcpImportDryRun, "dry-run", false, "show what would be imported without writing")
}
