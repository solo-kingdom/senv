package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/exportfile"
	"github.com/wii/senv/internal/session"
)

var backupShorthandFile string

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage backup blocks",
	Long:  `Manage encrypted backup blocks organized by groups. Infrequently used data, isolated from text. No {{...}} references.`,
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		group, key, ok := parseAddress(args[0])
		if !ok {
			return cmd.Help()
		}
		return runBackupShorthand(group, key, backupShorthandFile, args[1:])
	},
}

var backupGroup string

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.PersistentFlags().StringVarP(&backupGroup, "group", "g", "default", "backup block group")
	backupCmd.Flags().StringVar(&backupShorthandFile, "file", "", "read value from file (shorthand)")
}

func getBackupManager() (*backup.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	var mgr *backup.Manager
	if auth.hasKey() {
		mgr = backup.NewManagerWithKey(auth.storage, auth.key)
	} else {
		mgr = backup.NewManager(auth.storage, auth.password)
	}
	if err := mgr.EnsureDefault(); err != nil {
		return nil, err
	}
	return mgr, nil
}

// --- backup set ---

var backupSetFile string
var backupSetDescription string

var backupSetCmd = &cobra.Command{
	Use:   "set <key|group:key> [value]",
	Short: "Set a backup block",
	Long: `Set a backup block. Input priority: --file > stdin pipe > argument > editor.
When no value is provided and stdin is a terminal, opens an editor.
If the key already exists, the editor will be pre-filled with the existing content.
The key may be a group:key address (e.g. feg:ACCOUNT); address group takes precedence over -g/--group.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], backupGroup)
		target := "backup:" + group + ":" + key

		var desc *string
		if cmd.Flags().Changed("description") {
			desc = &backupSetDescription
		}

		var setErr error
		var via string
		switch {
		case backupSetFile != "":
			via = "set --file"
			setErr = backupManager.SetFromFileWithDescription(group, key, backupSetFile, desc)
		case isPipe():
			via = "set stdin"
			setErr = backupManager.SetFromReaderWithDescription(group, key, os.Stdin, desc)
		case len(args) >= 2:
			via = "set"
			if desc != nil {
				setErr = backupManager.SetWithDescription(group, key, args[1], desc)
			} else {
				setErr = backupManager.Set(group, key, args[1])
			}
		default:
			via = "set editor"
			setErr = backupManager.SetViaEditor(group, key)
			if setErr == nil && desc != nil {
				value, getErr := backupManager.Get(group, key)
				if getErr != nil {
					setErr = getErr
				} else {
					setErr = backupManager.SetWithDescription(group, key, value, desc)
				}
			}
		}
		if setErr != nil {
			auditOp(session.AuditOpBackup, target, false, via+" 失败")
			return setErr
		}
		auditOp(session.AuditOpBackup, target, true, via)
		return nil
	},
}

// --- backup import ---

var backupImportFile string
var backupImportDescription string

var backupImportCmd = &cobra.Command{
	Use:   "import <key|group:key>",
	Short: "Import a backup block from a file (upsert)",
	Long: `Import a backup block from a file, encrypting the content into the vault.
The source file is left untouched. If the key already exists, its value is
overwritten and updated_at refreshes (same semantics as the TUI import);
there is no overwrite confirmation. --file is required: import never falls
back to stdin or an editor.
The key may be a group:key address (e.g. feg:ACCOUNT); address group takes precedence over -g/--group.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if backupImportFile == "" {
			return fmt.Errorf("--file is required")
		}
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], backupGroup)
		var desc *string
		if cmd.Flags().Changed("description") {
			desc = &backupImportDescription
		}
		if err := backupManager.SetFromFileWithDescription(group, key, backupImportFile, desc); err != nil {
			auditOp(session.AuditOpBackup, "backup:"+group+":"+key, false, "import 失败")
			return err
		}
		auditOp(session.AuditOpBackup, "backup:"+group+":"+key, true, "import "+backupImportFile)
		fmt.Printf("✓ Imported backup %s into group %s\n", key, group)
		return nil
	},
}

// --- backup get ---

var (
	backupGetOutput string
	backupGetMode   string
	backupGetCopy   bool
)

var backupGetCmd = &cobra.Command{
	Use:   "get <key|group:key>",
	Short: "Get a backup block",
	Long: `Get a backup block value. Outputs the stored bytes; references are never resolved.
With -o/--output, new plaintext files default to 0600. Use --mode 0644 only
to explicitly share non-secret output; the choice is not saved as a default.
Existing files with stricter permissions are not widened.
The key may be a group:key address (e.g. notes:DUMP); address group takes precedence over -g/--group.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := exportfile.ParseFileMode(backupGetMode)
		if err != nil {
			return err
		}
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], backupGroup)
		value, err := backupManager.Get(group, key)
		if err != nil {
			return err
		}

		// Output
		if backupGetCopy {
			return backupManager.GetToClipboard(group, key)
		}

		if backupGetOutput != "" {
			return backupManager.ExportValue(value, backupGetOutput, mode)
		}

		fmt.Print(value)
		return nil
	},
}

// --- backup export ---

var backupExportPath string

var backupExportCmd = &cobra.Command{
	Use:   "export <key|group:key>",
	Short: "Export a backup block to a plaintext file (0600)",
	Long: `Export a backup block's plaintext value to a file. The file is written
atomically with fixed 0600 permissions (overwriting an existing permissive
file tightens it; symlinks are rejected). The value is exported byte-for-byte
as stored. References are never resolved. On success only the path is printed, never the value.
Export is a read-side operation and records no audit event.
The key may be a group:key address (e.g. feg:ACCOUNT); address group takes precedence over -g/--group.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if backupExportPath == "" {
			return fmt.Errorf("--path is required")
		}
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], backupGroup)
		if err := backupManager.GetToFile(group, key, backupExportPath); err != nil {
			return err
		}
		fmt.Printf("✓ Exported to %s\n", backupExportPath)
		return nil
	},
}

// --- backup delete ---

var backupDeleteCmd = &cobra.Command{
	Use:   "delete <key|group:key>",
	Short: "Delete a backup block",
	Long:  `Delete a backup block. The key may be a group:key address; address group takes precedence over -g/--group.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], backupGroup)
		if err := backupManager.Delete(group, key); err != nil {
			auditOp(session.AuditOpBackup, "backup:"+group+":"+key, false, "delete 失败")
			return err
		}

		auditOp(session.AuditOpBackup, "backup:"+group+":"+key, true, "delete")
		fmt.Printf("✓ Deleted backup %s from group %s\n", key, group)
		return nil
	},
}

// --- backup list ---

var backupListCmd = &cobra.Command{
	Use:   "list [group]",
	Short: "List backup blocks",
	Long:  `List backup blocks in a group. Shows key name, size, and last updated time.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		listGroup := backupGroup
		if len(args) > 0 {
			listGroup = args[0]
		}

		infos, err := backupManager.List(listGroup)
		if err != nil {
			return err
		}

		if len(infos) == 0 {
			fmt.Println("No backup blocks found")
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

// --- backup group ---

var backupGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Manage backup groups",
	Long:  `Manage backup block groups. Groups are used to organize backup blocks.`,
}

var backupGroupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all backup groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		groups, err := backupManager.ListGroups()
		if err != nil {
			return err
		}

		// Hide groups that have no keys, except "default".
		visible := make([]backup.GroupInfo, 0, len(groups))
		for _, g := range groups {
			if g.KeyCount == 0 && g.Name != "default" {
				continue
			}
			visible = append(visible, g)
		}

		if len(visible) == 0 {
			fmt.Println("No backup groups found")
			return nil
		}

		fmt.Println("Backup groups:")
		for _, g := range visible {
			fmt.Printf("  %s (%d keys)\n    %s\n", g.Name, g.KeyCount, g.Description)
		}

		return nil
	},
}

var backupGroupAddDescription string

var backupGroupAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new backup group",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		name := args[0]
		if err := backupManager.AddGroup(name, backupGroupAddDescription); err != nil {
			return err
		}

		fmt.Printf("✓ Created backup group %s\n", name)
		return nil
	},
}

var backupGroupDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a backup group and all its contents",
	Long:  `Delete a backup group and all its contents. This action cannot be undone.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupManager, err := getBackupManager()
		if err != nil {
			return err
		}

		name := args[0]

		// Confirmation prompt
		fmt.Printf("Are you sure you want to delete backup group '%s' and all its contents? [y/N] ", name)
		var response string
		_, _ = fmt.Scanln(&response)

		if !strings.EqualFold(response, "y") && !strings.EqualFold(response, "yes") {
			fmt.Println("Cancelled")
			return nil
		}

		if err := backupManager.DeleteGroup(name); err != nil {
			auditOp(session.AuditOpBackup, "backup:"+name, false, "delete group 失败")
			return err
		}

		auditOp(session.AuditOpBackup, "backup:"+name, true, "delete group")
		fmt.Printf("✓ Deleted backup group %s\n", name)
		return nil
	},
}

func init() {
	// backup set flags
	backupSetCmd.Flags().StringVar(&backupSetFile, "file", "", "read value from file")
	backupSetCmd.Flags().StringVar(&backupSetDescription, "description", "", "optional note stored with the backup block (omit to keep the existing note)")

	addRefreshFlag(backupGetCmd)
	backupGetCmd.Flags().StringVarP(&backupGetOutput, "output", "o", "", "write output to file")
	backupGetCmd.Flags().StringVar(&backupGetMode, "mode", "0600", "output file permissions in strict octal form (0000-0777)")
	backupGetCmd.Flags().BoolVar(&backupGetCopy, "copy", false, "copy output to clipboard")
	addRefreshFlag(backupListCmd)

	// Register subcommands
	backupCmd.AddCommand(backupSetCmd)
	backupCmd.AddCommand(backupGetCmd)
	backupCmd.AddCommand(backupDeleteCmd)
	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupImportCmd)
	backupCmd.AddCommand(backupExportCmd)
	backupCmd.AddCommand(backupGroupCmd)

	// backup import flags
	backupImportCmd.Flags().StringVar(&backupImportFile, "file", "", "read the value from this file (required)")
	backupImportCmd.Flags().StringVar(&backupImportDescription, "description", "", "optional note stored with the backup block (omit to keep the existing note)")

	// backup export flags
	backupExportCmd.Flags().StringVar(&backupExportPath, "path", "", "write the plaintext value to this file (required, fixed 0600)")

	backupGroupCmd.AddCommand(backupGroupListCmd)
	backupGroupCmd.AddCommand(backupGroupAddCmd)
	backupGroupCmd.AddCommand(backupGroupDeleteCmd)
	backupGroupAddCmd.Flags().StringVar(&backupGroupAddDescription, "description", "", "required note describing what this group is for")
	_ = backupGroupAddCmd.MarkFlagRequired("description")
}
