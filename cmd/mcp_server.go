package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// getMCPManager authenticates once and returns a manager over the vault key
// (or the password, when no session cache is available).
func getMCPManager() (*mcp.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	if auth.hasKey() {
		return mcp.NewManagerWithKey(auth.storage, auth.key), nil
	}
	return mcp.NewManager(auth.storage, auth.password), nil
}

// parseMCPEnv parses repeated KEY=VALUE flags. Values may be empty and may
// contain '=', so only the first separator is significant.
func parseMCPEnv(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid --env %q: expected KEY=VALUE", pair)
		}
		if _, dup := env[key]; dup {
			return nil, fmt.Errorf("duplicate --env key %q", key)
		}
		env[key] = value
	}
	return env, nil
}

var mcpAddCmd = &cobra.Command{
	Use:   "add <alias>",
	Short: "Add an MCP server profile",
	Long: `Store an MCP server definition in the vault so it can be exported into
coding-agent global configs with ` + "`senv mcp export`" + `.

Only the stdio transport is supported; env values may embed {{env:...}} or
{{text:...}} references, which are stored raw and resolved at export time.

Examples:
  senv mcp add github --command npx \
    --arg -y --arg @modelcontextprotocol/server-github \
    --env GITHUB_TOKEN={{env:secrets:GH_TOKEN}}`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := parseMCPEnv(mcpAddEnv)
		if err != nil {
			return err
		}
		mgr, err := getMCPManager()
		if err != nil {
			return err
		}
		entry := &storage.MCPServerEntry{
			Alias:       args[0],
			Transport:   mcpAddTransport,
			Command:     mcpAddCommand,
			Args:        mcpAddArgs,
			Env:         env,
			Description: mcpAddDescription,
		}
		if err := mgr.Add(entry); err != nil {
			auditOp(session.AuditOpMCPServer, "mcp:"+args[0], false, "add 失败")
			return err
		}
		auditOp(session.AuditOpMCPServer, "mcp:"+args[0], true, "add")
		fmt.Printf("✓ Added MCP server %s\n", args[0])
		return nil
	},
}

var mcpGetCmd = &cobra.Command{
	Use:   "get <alias>",
	Short: "Show an MCP server profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getMCPManager()
		if err != nil {
			return err
		}
		entry, err := mgr.Get(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("MCP server %s\n", entry.Alias)
		fmt.Printf("  transport: %s\n", entry.Transport)
		fmt.Printf("  command: %s\n", entry.Command)
		if len(entry.Args) > 0 {
			fmt.Printf("  args: %s\n", strings.Join(entry.Args, " "))
		}
		if entry.Description != "" {
			fmt.Printf("  description: %s\n", entry.Description)
		}
		for _, key := range sortedMCPEnvKeys(entry.Env) {
			fmt.Printf("  env %s=%s\n", key, entry.Env[key])
		}
		return nil
	},
}

var (
	mcpEditCommand     string
	mcpEditArgs        []string
	mcpEditEnv         []string
	mcpEditUnsetEnv    []string
	mcpEditDescription string
)

var mcpEditCmd = &cobra.Command{
	Use:   "edit <alias>",
	Short: "Edit an MCP server profile",
	Long: `Update fields of a stored MCP server profile. The alias is an identity
and cannot be changed here.

Supplying any --arg replaces the whole argument list; supplying any --env
replaces the whole env set (combine with --unset-env to drop single keys).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := parseMCPEnv(mcpEditEnv)
		if err != nil {
			return err
		}
		mgr, err := getMCPManager()
		if err != nil {
			return err
		}
		err = mgr.Update(args[0], func(entry *storage.MCPServerEntry) error {
			if cmd.Flags().Changed("command") {
				entry.Command = mcpEditCommand
			}
			if cmd.Flags().Changed("arg") {
				entry.Args = mcpEditArgs
			}
			if cmd.Flags().Changed("env") {
				entry.Env = env
			}
			for _, key := range mcpEditUnsetEnv {
				if entry.Env == nil {
					break
				}
				if _, ok := entry.Env[key]; !ok && !cmd.Flags().Changed("env") {
					return fmt.Errorf("env key %q is not set on %q", key, entry.Alias)
				}
				delete(entry.Env, key)
			}
			if cmd.Flags().Changed("description") {
				entry.Description = mcpEditDescription
			}
			return nil
		})
		if err != nil {
			auditOp(session.AuditOpMCPServer, "mcp:"+args[0], false, "edit 失败")
			return err
		}
		auditOp(session.AuditOpMCPServer, "mcp:"+args[0], true, "edit")
		fmt.Printf("✓ Updated MCP server %s\n", args[0])
		return nil
	},
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP server profiles",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getMCPManager()
		if err != nil {
			return err
		}
		servers, err := mgr.List()
		if err != nil {
			return err
		}
		if len(servers) == 0 {
			fmt.Println("No MCP servers stored.")
			return nil
		}
		fmt.Printf("%-20s %-8s %-28s %s\n", "ALIAS", "TRANSPORT", "COMMAND", "ENV KEYS")
		for _, s := range servers {
			fmt.Printf("%-20s %-8s %-28s %s\n", s.Alias, s.Transport, truncateCell(s.Command, 28), strings.Join(s.EnvKeys, ","))
		}
		return nil
	},
}

var mcpDeleteCmd = &cobra.Command{
	Use:   "delete <alias>",
	Short: "Delete an MCP server profile",
	Long: `Delete a stored MCP server profile. Agent config files are left alone:
use ` + "`senv mcp unexport`" + ` to remove already-exported entries.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getMCPManager()
		if err != nil {
			return err
		}
		if err := mgr.Delete(args[0]); err != nil {
			auditOp(session.AuditOpMCPServer, "mcp:"+args[0], false, "delete 失败")
			return err
		}
		auditOp(session.AuditOpMCPServer, "mcp:"+args[0], true, "delete")
		fmt.Printf("✓ Deleted MCP server %s\n", args[0])
		fmt.Printf("  Already-exported entries stay in agent configs; run `senv mcp unexport` to remove them.\n")
		return nil
	},
}

func sortedMCPEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func truncateCell(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit-1] + "…"
}

var (
	mcpAddTransport   string
	mcpAddCommand     string
	mcpAddArgs        []string
	mcpAddEnv         []string
	mcpAddDescription string
)

func init() {
	mcpCmd.AddCommand(mcpAddCmd)
	mcpCmd.AddCommand(mcpGetCmd)
	mcpCmd.AddCommand(mcpEditCmd)
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpDeleteCmd)

	mcpAddCmd.Flags().StringVar(&mcpAddTransport, "transport", storage.MCPTransportStdio, "transport: only stdio is supported")
	mcpAddCmd.Flags().StringVar(&mcpAddCommand, "command", "", "executable to launch (required)")
	mcpAddCmd.Flags().StringArrayVar(&mcpAddArgs, "arg", nil, "command argument (repeatable, order preserved)")
	mcpAddCmd.Flags().StringArrayVar(&mcpAddEnv, "env", nil, "environment entry KEY=VALUE (repeatable; value may embed {{env:...}} references)")
	mcpAddCmd.Flags().StringVar(&mcpAddDescription, "description", "", "free-form description")

	mcpEditCmd.Flags().StringVar(&mcpEditCommand, "command", "", "replace the command")
	mcpEditCmd.Flags().StringArrayVar(&mcpEditArgs, "arg", nil, "replace the whole argument list (repeatable)")
	mcpEditCmd.Flags().StringArrayVar(&mcpEditEnv, "env", nil, "replace the whole env set (repeatable, KEY=VALUE)")
	mcpEditCmd.Flags().StringArrayVar(&mcpEditUnsetEnv, "unset-env", nil, "remove a single env key (repeatable)")
	mcpEditCmd.Flags().StringVar(&mcpEditDescription, "description", "", "replace the description")

	addRefreshFlag(mcpGetCmd)
	addRefreshFlag(mcpListCmd)
}
