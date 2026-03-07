package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environment variables",
	Long:  `Manage encrypted environment variables organized by groups.`,
}

var envGroup string

func init() {
	rootCmd.AddCommand(envCmd)
	envCmd.PersistentFlags().StringVarP(&envGroup, "group", "g", "default", "environment variable group")
}

func getEnvManager() (*env.Manager, error) {
	configPath := getConfigPath()
	dataPath := getDataPath()
	manager := storage.NewManager(configPath, dataPath)

	if !manager.IsInitialized() {
		return nil, fmt.Errorf("project not initialized. Run 'senv init' first")
	}

	// Try to get cached key from session
	sessionManager := session.NewManager(configPath, dataPath)
	key, err := sessionManager.GetCachedKey()
	if err == nil {
		// Cache is valid, use it
		return env.NewManagerWithKey(manager, key), nil
	}

	// Cache is invalid or doesn't exist, prompt for password
	password, err := promptPassword("Enter password: ")
	if err != nil {
		return nil, fmt.Errorf("failed to read password: %w", err)
	}

	// Verify password
	valid, err := manager.VerifyPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to verify password: %w", err)
	}

	if !valid {
		return nil, fmt.Errorf("invalid password")
	}

	// Check if session cache is enabled and save cache
	settings, err := manager.LoadSettings()
	if err == nil && settings.Session.Enabled {
		timeout, err := session.ParseTimeout(settings.Session.Timeout)
		if err == nil && timeout != nil {
			// Start session with configured timeout
			if err := sessionManager.StartSession(password, timeout); err == nil {
				// Session started successfully, show message only for export command
				// (to avoid cluttering other command outputs)
			}
		}
	}

	return env.NewManager(manager, password), nil
}

// envGetCmd represents the env get command
var envGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get an environment variable",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		key := args[0]
		value, err := envManager.Get(envGroup, key)
		if err != nil {
			return err
		}

		fmt.Println(value)
		return nil
	},
}

// envSetCmd represents the env set command
var envSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set an environment variable",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		key := args[0]
		value := args[1]

		if err := envManager.Set(envGroup, key, value); err != nil {
			return err
		}

		fmt.Printf("✓ Set %s in group %s\n", key, envGroup)
		return nil
	},
}

// envDeleteCmd represents the env delete command
var envDeleteCmd = &cobra.Command{
	Use:   "delete <key>",
	Short: "Delete an environment variable",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		key := args[0]

		if err := envManager.Delete(envGroup, key); err != nil {
			return err
		}

		fmt.Printf("✓ Deleted %s from group %s\n", key, envGroup)
		return nil
	},
}

// envListCmd represents the env list command
var envListCmd = &cobra.Command{
	Use:   "list [group]",
	Short: "List environment variables",
	Long: `List environment variables. If group is specified, list only that group.
Otherwise, list all groups.`,
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

		for group, variables := range vars {
			fmt.Printf("\n[%s]\n", group)
			if len(variables) == 0 {
				fmt.Println("  (empty)")
				continue
			}
			for key, value := range variables {
				// Mask long values
				displayValue := value
				if len(value) > 50 {
					displayValue = value[:47] + "..."
				}
				fmt.Printf("  %s=%s\n", key, displayValue)
			}
		}

		return nil
	},
}

// envExportCmd represents the env export command
var envExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export environment variables for shell",
	Long: `Export environment variables from active groups.
This command outputs shell-compatible export statements.
	
Usage:
  eval $(senv env export)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		envManager, err := getEnvManager()
		if err != nil {
			return err
		}

		exports, err := envManager.Export()
		if err != nil {
			return err
		}

		if exports == "" {
			return nil
		}

		fmt.Println(exports)
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

		if len(groups) == 0 {
			fmt.Println("No groups found")
			return nil
		}

		fmt.Println("Environment variable groups:")
		for _, group := range groups {
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
}
