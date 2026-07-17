package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/session"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environment variables",
	Long:  `Manage encrypted environment variables organized by groups.`,
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		group, key, ok := parseAddress(args[0])
		if !ok {
			return cmd.Help()
		}
		return runEnvShorthand(group, key, args[1:])
	},
}

var envGroup string

func init() {
	rootCmd.AddCommand(envCmd)
	envCmd.PersistentFlags().StringVarP(&envGroup, "group", "g", "default", "environment variable group")
}

func getEnvManager() (*env.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	if auth.hasKey() {
		return env.NewManagerWithKey(auth.storage, auth.key), nil
	}
	return env.NewManager(auth.storage, auth.password), nil
}

// envGetCmd represents the env get command
var (
	envGetDecode bool
	envGetLoose  bool
)

var envGetCmd = &cobra.Command{
	Use:   "get <key|group:key>",
	Short: "Get an environment variable",
	Long: `Get an environment variable value. By default outputs the raw value.
Use -d/--decode to resolve {{env:...}} and {{text:...}} references.
The key may be a group:key address (e.g. prod:API_KEY); address group takes precedence over -g/--group.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], envGroup)
		value, err := envManager.Get(group, key)
		if err != nil {
			return err
		}

		// Resolve references if -d flag is set
		if envGetDecode {
			resolved, err := resolveValue(value, envGetLoose, group)
			if err != nil {
				return err
			}
			value = resolved
		}

		fmt.Println(value)
		return nil
	},
}

// envSetCmd represents the env set command
var envSetCmd = &cobra.Command{
	Use:   "set <key|group:key> <value>",
	Short: "Set an environment variable",
	Long:  `Set an environment variable. The key may be a group:key address; address group takes precedence over -g/--group.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], envGroup)
		value := args[1]

		if err := envManager.Set(group, key, value); err != nil {
			return err
		}

		fmt.Printf("✓ Set %s in group %s\n", key, group)
		return nil
	},
}

// envDeleteCmd represents the env delete command
var envDeleteCmd = &cobra.Command{
	Use:   "delete <key|group:key>",
	Short: "Delete an environment variable",
	Long:  `Delete an environment variable. The key may be a group:key address; address group takes precedence over -g/--group.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		group, key := resolveAddressKey(args[0], envGroup)

		if err := envManager.Delete(group, key); err != nil {
			return err
		}

		fmt.Printf("✓ Deleted %s from group %s\n", key, group)
		return nil
	},
}

// envListCmd represents the env list command
var (
	envListDecode bool
	envListLoose  bool
)

var envListCmd = &cobra.Command{
	Use:   "list [group]",
	Short: "List environment variables",
	Long: `List environment variables. If group is specified, list only that group.
Otherwise, list all groups.
Use -d/--decode to resolve {{env:...}} and {{text:...}} references.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		// If a group is specified as an argument, use it
		listGroup := envGroup
		if len(args) > 0 {
			listGroup = args[0]
		}

		vars, err := envManager.List(listGroup)
		if err != nil {
			return err
		}

		if len(vars) == 0 {
			fmt.Println("No environment variables found")
			return nil
		}

		// Determine the default group name so empty non-default groups can be
		// hidden from the listing.
		defaultGroup := ""
		if infos, err := envManager.ListGroups(); err == nil {
			for _, gi := range infos {
				if gi.IsDefault {
					defaultGroup = gi.Name
					break
				}
			}
		}

		hasVisible := false
		for group, variables := range vars {
			// Hide groups that have no keys, except the default group.
			if len(variables) == 0 && group != defaultGroup {
				continue
			}
			hasVisible = true
			fmt.Printf("\n[%s]\n", group)
			if len(variables) == 0 {
				fmt.Println("  (empty)")
				continue
			}
			for key, value := range variables {
				// Resolve references if -d flag is set
				displayValue := value
				if envListDecode {
					resolved, err := resolveValue(value, envListLoose, group)
					if err != nil {
						displayValue = fmt.Sprintf("[ERROR: %v]", err)
					} else {
						displayValue = resolved
					}
				}
				// Mask long values
				if len(displayValue) > 50 {
					displayValue = displayValue[:47] + "..."
				}
				fmt.Printf("  %s=%s\n", key, displayValue)
			}
		}

		if !hasVisible {
			fmt.Println("No environment variables found")
		}

		return nil
	},
}

// envExportIfSession, when set, makes `env export` succeed with empty stdout
// when there is no valid session (for shell rc files).
var envExportIfSession bool

// envExportCmd represents the env export command
var envExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export environment variables for shell",
	Long: `Export environment variables from active groups.
This command outputs shell-compatible export statements.
References ({{env:...}} and {{text:...}}) are automatically resolved.

When stdout is not a TTY (e.g. eval $(senv env export)) and there is no
active session, export refuses to prompt and asks you to run
senv session start. Use --if-session in shell rc files to skip silently.

Usage:
  senv session start -t never
  eval "$(senv env export --if-session)"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if envExportIfSession {
			sm := session.NewManager(getConfigPath(), getDataPath())
			if _, err := sm.GetCachedKey(); err != nil {
				return nil
			}
		}

		// Captured stdout (eval / pipes) must not prompt for a password.
		prev := activeAuthOpts
		activeAuthOpts = authOptions{requireStdoutTTY: true}
		defer func() { activeAuthOpts = prev }()

		envManager, err := getEnvManager()
		if err != nil {
			if envExportIfSession && errors.Is(err, ErrNeedSession) {
				return nil
			}
			return err
		}

		exports, err := envManager.Export()
		if err != nil {
			return err
		}

		if exports == "" {
			return nil
		}

		// Auto-resolve references in export
		resolved, err := resolveValue(exports, false, envGroup)
		if err != nil {
			return fmt.Errorf("failed to resolve references in export: %w", err)
		}

		fmt.Println(resolved)
		return nil
	},
}

// envGroupCmd represents the env group command
var envGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Manage environment variable groups",
}

// envGroupListCmd represents the env group list command
var envGroupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		groups, err := envManager.ListGroups()
		if err != nil {
			return err
		}

		// Hide groups that have no keys, except the default group.
		visible := make([]env.GroupInfo, 0, len(groups))
		for _, group := range groups {
			if group.VarCount == 0 && !group.IsDefault {
				continue
			}
			visible = append(visible, group)
		}

		if len(visible) == 0 {
			fmt.Println("No groups found")
			return nil
		}

		fmt.Println("Environment variable groups:")
		for _, group := range visible {
			status := "inactive"
			if group.IsActive {
				status = "active"
			}
			defaultMark := ""
			if group.IsDefault {
				defaultMark = " (default)"
			}
			fmt.Printf("  %s%s - %s - %d variables\n", group.Name, defaultMark, status, group.VarCount)
		}

		return nil
	},
}

// envGroupAddCmd represents the env group add command
var envGroupAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new group",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		name := args[0]
		if err := envManager.AddGroup(name); err != nil {
			return err
		}

		fmt.Printf("✓ Created group %s\n", name)
		return nil
	},
}

// envGroupActivateCmd represents the env group activate command
var envGroupActivateCmd = &cobra.Command{
	Use:   "activate <name>",
	Short: "Activate a group",
	Long: `Activate a group so its variables are included in 'env export'.
The default group is always active.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		name := args[0]
		if err := envManager.ActivateGroup(name); err != nil {
			return err
		}

		fmt.Printf("✓ Activated group %s\n", name)
		return nil
	},
}

// envGroupDeactivateCmd represents the env group deactivate command
var envGroupDeactivateCmd = &cobra.Command{
	Use:   "deactivate <name>",
	Short: "Deactivate a group",
	Long: `Deactivate a group so its variables are not included in 'env export'.
The default group cannot be deactivated.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		name := args[0]
		if err := envManager.DeactivateGroup(name); err != nil {
			return err
		}

		fmt.Printf("✓ Deactivated group %s\n", name)
		return nil
	},
}

func init() {
	envCmd.AddCommand(envGetCmd)
	envCmd.AddCommand(envSetCmd)
	envCmd.AddCommand(envDeleteCmd)
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envExportCmd)
	envCmd.AddCommand(envGroupCmd)

	envGroupCmd.AddCommand(envGroupListCmd)
	envGroupCmd.AddCommand(envGroupAddCmd)
	envGroupCmd.AddCommand(envGroupActivateCmd)
	envGroupCmd.AddCommand(envGroupDeactivateCmd)

	// Add -d/--decode and --loose flags to env get and list
	envGetCmd.Flags().BoolVarP(&envGetDecode, "decode", "d", false, "resolve {{env:...}} and {{text:...}} references")
	envGetCmd.Flags().BoolVar(&envGetLoose, "loose", false, "keep unresolved references as-is instead of erroring")
	envListCmd.Flags().BoolVarP(&envListDecode, "decode", "d", false, "resolve {{env:...}} and {{text:...}} references")
	envListCmd.Flags().BoolVar(&envListLoose, "loose", false, "keep unresolved references as-is instead of erroring")
	envExportCmd.Flags().BoolVar(&envExportIfSession, "if-session", false,
		"if no active session, print nothing and exit 0 (for shell rc files)")
}
