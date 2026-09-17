package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/exportfile"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/text"
)

var textShorthandFile string

var textCmd = &cobra.Command{
	Use:   "text",
	Short: "Manage text blocks",
	Long:  `Manage encrypted text blocks organized by groups. Supports long text, multi-line content, and cross-references with env.`,
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		group, key, ok := parseAddress(args[0])
		if !ok {
			return cmd.Help()
		}
		return runTextShorthand(group, key, textShorthandFile, args[1:])
	},
}

var textGroup string

func init() {
	rootCmd.AddCommand(textCmd)
	textCmd.PersistentFlags().StringVarP(&textGroup, "group", "g", "default", "text block group")
	textCmd.Flags().StringVar(&textShorthandFile, "file", "", "read value from file (shorthand)")
}

// getTextManager creates a text manager, reusing session cache when available
func getTextManager() (*text.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	if auth.hasKey() {
		return text.NewManagerWithKey(auth.storage, auth.key), nil
	}
	return text.NewManager(auth.storage, auth.password), nil
}

// isPipe checks if stdin is a pipe (not a terminal)
func isPipe() bool {
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// --- text set ---

var textSetFile string
var textSetDescription string

var textSetCmd = &cobra.Command{
	Use:   "set <key|group:key> [value]",
	Short: "Set a text block",
	Long: `Set a text block. Input priority: --file > stdin pipe > argument > editor.
When no value is provided and stdin is a terminal, opens an editor.
If the key already exists, the editor will be pre-filled with the existing content.
The key may be a group:key address (e.g. feg:ACCOUNT); address group takes precedence over -g/--group.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], textGroup)
		target := "text:" + group + ":" + key

		var desc *string
		if cmd.Flags().Changed("description") {
			desc = &textSetDescription
		}

		var setErr error
		var via string
		switch {
		case textSetFile != "":
			via = "set --file"
			setErr = textManager.SetFromFileWithDescription(group, key, textSetFile, desc)
		case isPipe():
			via = "set stdin"
			setErr = textManager.SetFromReaderWithDescription(group, key, os.Stdin, desc)
		case len(args) >= 2:
			via = "set"
			if desc != nil {
				setErr = textManager.SetWithDescription(group, key, args[1], desc)
			} else {
				setErr = textManager.Set(group, key, args[1])
			}
		default:
			via = "set editor"
			setErr = textManager.SetViaEditor(group, key)
			if setErr == nil && desc != nil {
				value, getErr := textManager.Get(group, key)
				if getErr != nil {
					setErr = getErr
				} else {
					setErr = textManager.SetWithDescription(group, key, value, desc)
				}
			}
		}
		if setErr != nil {
			auditOp(session.AuditOpText, target, false, via+" 失败")
			return setErr
		}
		auditOp(session.AuditOpText, target, true, via)
		return nil
	},
}

// --- text import ---

var textImportFile string
var textImportDescription string

var textImportCmd = &cobra.Command{
	Use:   "import <key|group:key>",
	Short: "Import a text block from a file (upsert)",
	Long: `Import a text block from a file, encrypting the content into the vault.
The source file is left untouched. If the key already exists, its value is
overwritten and updated_at refreshes (same semantics as the TUI import);
there is no overwrite confirmation. --file is required: import never falls
back to stdin or an editor.
The key may be a group:key address (e.g. feg:ACCOUNT); address group takes precedence over -g/--group.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if textImportFile == "" {
			return fmt.Errorf("--file is required")
		}
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], textGroup)
		var desc *string
		if cmd.Flags().Changed("description") {
			desc = &textImportDescription
		}
		if err := textManager.SetFromFileWithDescription(group, key, textImportFile, desc); err != nil {
			auditOp(session.AuditOpText, "text:"+group+":"+key, false, "import 失败")
			return err
		}
		auditOp(session.AuditOpText, "text:"+group+":"+key, true, "import "+textImportFile)
		fmt.Printf("✓ Imported text %s into group %s\n", key, group)
		return nil
	},
}

// --- text get ---

var (
	textGetDecode bool
	textGetLoose  bool
	textGetOutput string
	textGetMode   string
	textGetCopy   bool
)

var textGetCmd = &cobra.Command{
	Use:   "get <key|group:key>",
	Short: "Get a text block",
	Long: `Get a text block value. By default outputs the raw value.
Use -d/--decode to resolve {{env:...}} and {{text:...}} references.
With -o/--output, new plaintext files default to 0600. Use --mode 0644 only
to explicitly share non-secret output; the choice is not saved as a default.
Existing files with stricter permissions are not widened.
The key may be a group:key address (e.g. feg:ACCOUNT); address group takes precedence over -g/--group.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := exportfile.ParseFileMode(textGetMode)
		if err != nil {
			return err
		}
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], textGroup)
		value, err := textManager.Get(group, key)
		if err != nil {
			return err
		}

		// Resolve references if -d flag is set
		if textGetDecode {
			resolved, err := resolveValue(value, textGetLoose, group)
			if err != nil {
				return err
			}
			value = resolved
		}

		// Output
		if textGetCopy {
			return textManager.GetToClipboard(group, key)
		}

		if textGetOutput != "" {
			return textManager.ExportValue(value, textGetOutput, mode)
		}

		fmt.Print(value)
		return nil
	},
}

// --- text export ---

var textExportPath string

var textExportCmd = &cobra.Command{
	Use:   "export <key|group:key>",
	Short: "Export a text block to a plaintext file (0600)",
	Long: `Export a text block's plaintext value to a file. The file is written
atomically with fixed 0600 permissions (overwriting an existing permissive
file tightens it; symlinks are rejected). The value is exported byte-for-byte
as stored — {{env:...}} references are NOT resolved (use "text get -d -o"
for decoded export). On success only the path is printed, never the value.
Export is a read-side operation and records no audit event.
The key may be a group:key address (e.g. feg:ACCOUNT); address group takes precedence over -g/--group.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if textExportPath == "" {
			return fmt.Errorf("--path is required")
		}
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], textGroup)
		if err := textManager.GetToFile(group, key, textExportPath); err != nil {
			return err
		}
		fmt.Printf("✓ Exported to %s\n", textExportPath)
		return nil
	},
}

// --- text delete ---

var textDeleteCmd = &cobra.Command{
	Use:   "delete <key|group:key>",
	Short: "Delete a text block",
	Long:  `Delete a text block. The key may be a group:key address; address group takes precedence over -g/--group.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], textGroup)
		if err := textManager.Delete(group, key); err != nil {
			auditOp(session.AuditOpText, "text:"+group+":"+key, false, "delete 失败")
			return err
		}

		auditOp(session.AuditOpText, "text:"+group+":"+key, true, "delete")
		fmt.Printf("✓ Deleted text %s from group %s\n", key, group)
		return nil
	},
}

// --- text list ---

var textListCmd = &cobra.Command{
	Use:   "list [group]",
	Short: "List text blocks",
	Long:  `List text blocks in a group. Shows key name, size, and last updated time.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		listGroup := textGroup
		if len(args) > 0 {
			listGroup = args[0]
		}

		infos, err := textManager.List(listGroup)
		if err != nil {
			return err
		}

		if len(infos) == 0 {
			fmt.Println("No text blocks found")
			return nil
		}

		fmt.Printf("\n[%s]\n", listGroup)
		for _, info := range infos {
			fmt.Printf("  %-20s %6d bytes  %s",
				info.Key,
				info.Size,
				info.UpdatedAt.Format("2006-01-02 15:04"))
			if info.Description != "" {
				fmt.Printf("  %s", info.Description)
			}
			fmt.Println()
		}

		return nil
	},
}

// --- text group ---

var textGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Manage text groups",
	Long:  `Manage text block groups. Groups are used to organize text blocks.`,
}

var textGroupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all text groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		groups, err := textManager.ListGroups()
		if err != nil {
			return err
		}

		// Hide groups that have no keys, except "default".
		visible := make([]text.GroupInfo, 0, len(groups))
		for _, g := range groups {
			if g.KeyCount == 0 && g.Name != "default" {
				continue
			}
			visible = append(visible, g)
		}

		if len(visible) == 0 {
			fmt.Println("No text groups found")
			return nil
		}

		fmt.Println("Text groups:")
		for _, g := range visible {
			fmt.Printf("  %s (%d keys)\n    %s\n", g.Name, g.KeyCount, g.Description)
		}

		return nil
	},
}

var textGroupAddDescription string

var textGroupAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new text group",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		name := args[0]
		if err := textManager.AddGroup(name, textGroupAddDescription); err != nil {
			return err
		}

		fmt.Printf("✓ Created text group %s\n", name)
		return nil
	},
}

var textGroupDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a text group and all its contents",
	Long:  `Delete a text group and all its contents. This action cannot be undone.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		name := args[0]

		// Confirmation prompt
		fmt.Printf("Are you sure you want to delete text group '%s' and all its contents? [y/N] ", name)
		var response string
		fmt.Scanln(&response)

		if !strings.EqualFold(response, "y") && !strings.EqualFold(response, "yes") {
			fmt.Println("Cancelled")
			return nil
		}

		if err := textManager.DeleteGroup(name); err != nil {
			auditOp(session.AuditOpText, "text:"+name, false, "delete group 失败")
			return err
		}

		auditOp(session.AuditOpText, "text:"+name, true, "delete group")
		fmt.Printf("✓ Deleted text group %s\n", name)
		return nil
	},
}

// resolveValue resolves references in a value using the ref package. It resolves
// via the CLI's auth-backed managers (getConfigPath/getDataPath). MCP and other
// callers that already hold managers should use resolveValueWith instead.
func resolveValue(value string, loose bool, currentGroup string) (string, error) {
	envMgr, err := getEnvManager()
	if err != nil {
		return "", err
	}
	textMgr, err := getTextManager()
	if err != nil {
		return "", err
	}
	return resolveValueWith(value, loose, currentGroup, envMgr, textMgr)
}

// textValueGetter 是引用解析需要的最小 text 读取面。除 *text.Manager 外，
// MCP 侧传入带保留组限制的包装（见 cmd/mcp.go 的 mcpTextManager）。
type textValueGetter interface {
	Get(group, key string) (string, error)
}

// resolveValueWith resolves references using explicitly-provided managers,
// avoiding a re-auth round trip. Used by the MCP server (which authenticates
// once at startup) and tests.
func resolveValueWith(value string, loose bool, currentGroup string, envMgr *env.Manager, textMgr textValueGetter) (string, error) {
	getter := newRefGetter(envMgr, textMgr)
	opts := ref.ResolveOptions{
		Loose:        loose,
		CurrentGroup: currentGroup,
	}
	result, warnings, err := ref.ResolveWithWarnings(value, getter, opts)
	if err != nil {
		return "", err
	}
	ref.PrintWarnings(warnings)
	return result, nil
}

// newRefGetter 组装 env/text 两路引用读取器；resolveValueWith 与
// mcp export 的宽松解析共用同一构造。
func newRefGetter(envMgr *env.Manager, textMgr textValueGetter) *combinedGetter {
	return &combinedGetter{envManager: envMgr, textManager: textMgr}
}

// combinedGetter implements ref.ValueGetter using env and text managers
type combinedGetter struct {
	envManager interface {
		Get(group, key string) (string, error)
	}
	textManager textValueGetter
}

func (g *combinedGetter) GetEnvValue(group, key string) (string, error) {
	return g.envManager.Get(group, key)
}

func (g *combinedGetter) GetTextValue(group, key string) (string, error) {
	return g.textManager.Get(group, key)
}

func init() {
	// text set flags
	textSetCmd.Flags().StringVar(&textSetFile, "file", "", "read value from file")
	textSetCmd.Flags().StringVar(&textSetDescription, "description", "", "optional note stored with the text block (omit to keep the existing note)")

	// text get flags
	textGetCmd.Flags().BoolVarP(&textGetDecode, "decode", "d", false, "resolve {{env:...}} and {{text:...}} references")
	addRefreshFlag(textGetCmd)
	textGetCmd.Flags().BoolVar(&textGetLoose, "loose", false, "keep unresolved references as-is instead of erroring")
	textGetCmd.Flags().StringVarP(&textGetOutput, "output", "o", "", "write output to file")
	textGetCmd.Flags().StringVar(&textGetMode, "mode", "0600", "output file permissions in strict octal form (0000-0777)")
	textGetCmd.Flags().BoolVar(&textGetCopy, "copy", false, "copy output to clipboard")
	addRefreshFlag(textListCmd)

	// Register subcommands
	textCmd.AddCommand(textSetCmd)
	textCmd.AddCommand(textGetCmd)
	textCmd.AddCommand(textDeleteCmd)
	textCmd.AddCommand(textListCmd)
	textCmd.AddCommand(textImportCmd)
	textCmd.AddCommand(textExportCmd)
	textCmd.AddCommand(textGroupCmd)

	// text import flags
	textImportCmd.Flags().StringVar(&textImportFile, "file", "", "read the value from this file (required)")
	textImportCmd.Flags().StringVar(&textImportDescription, "description", "", "optional note stored with the text block (omit to keep the existing note)")

	// text export flags
	textExportCmd.Flags().StringVar(&textExportPath, "path", "", "write the plaintext value to this file (required, fixed 0600)")

	textGroupCmd.AddCommand(textGroupListCmd)
	textGroupCmd.AddCommand(textGroupAddCmd)
	textGroupCmd.AddCommand(textGroupDeleteCmd)
	textGroupAddCmd.Flags().StringVar(&textGroupAddDescription, "description", "", "required note describing what this group is for")
	_ = textGroupAddCmd.MarkFlagRequired("description")
}
