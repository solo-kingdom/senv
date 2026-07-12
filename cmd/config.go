package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration files",
	Long:  `Manage encrypted configuration files with target path mapping.`,
}

var (
	configTargetPath string
	configSourcePath string
)

func init() {
	rootCmd.AddCommand(configCmd)
}

func getConfigManager() (*config.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), promptPassword)
	if err != nil {
		return nil, err
	}
	if auth.hasKey() {
		return config.NewManagerWithKey(auth.storage, auth.key), nil
	}
	return config.NewManager(auth.storage, auth.password), nil
}

// configCreateCmd represents the config create command
var configCreateCmd = &cobra.Command{
	Use:   "create <name> --source <file> --target <path>",
	Short: "Create a new configuration file",
	Long: `Create a new encrypted configuration file from a source file.
You must specify both source file path and target path where the file will be restored on export.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		name := args[0]

		if configSourcePath == "" {
			return fmt.Errorf("source path is required. Use --source flag")
		}

		if configTargetPath == "" {
			return fmt.Errorf("target path is required. Use --target flag")
		}

		if err := configManager.Create(name, configSourcePath, configTargetPath); err != nil {
			return err
		}

		fmt.Printf("✓ Created config %s\n", name)
		fmt.Printf("  Source: %s\n", configSourcePath)
		fmt.Printf("  Target: %s\n", configTargetPath)
		return nil
	},
}

func init() {
	configCreateCmd.Flags().StringVar(&configSourcePath, "source", "", "source file path")
	configCreateCmd.Flags().StringVar(&configTargetPath, "target", "", "target file path for export")
	configCreateCmd.MarkFlagRequired("source")
	configCreateCmd.MarkFlagRequired("target")
}

// configEditCmd represents the config edit command
var configEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a configuration file",
	Long: `Edit a configuration file in your default editor ($EDITOR).
The file will be decrypted, edited, and re-encrypted automatically.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		name := args[0]

		return configManager.Edit(name)
	},
}

// configExportCmd represents the config export command
var configExportCmd = &cobra.Command{
	Use:   "export <name> [--path <target>]",
	Short: "Export a configuration file",
	Long: `Export a configuration file to its target path.
You can override the target path with --path flag.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		name := args[0]

		return configManager.Export(name, configTargetPath)
	},
}

func init() {
	configExportCmd.Flags().StringVar(&configTargetPath, "path", "", "override target path for export")
}

// configDeleteCmd represents the config delete command
var configDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a configuration file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		name := args[0]

		if err := configManager.Delete(name); err != nil {
			return err
		}

		fmt.Printf("✓ Deleted config %s\n", name)
		return nil
	},
}

// configListCmd represents the config list command
var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration files",
	RunE: func(cmd *cobra.Command, args []string) error {
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		configs, err := configManager.List()
		if err != nil {
			return err
		}

		if len(configs) == 0 {
			fmt.Println("No configuration files found")
			return nil
		}

		fmt.Println("Configuration files:")
		for _, cfg := range configs {
			fmt.Printf("  %s\n", cfg.Name)
			fmt.Printf("    Target: %s\n", cfg.TargetPath)
			fmt.Printf("    Updated: %s\n", cfg.UpdatedAt)
		}

		return nil
	},
}

// configGetCmd represents the config get command
var configGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get information about a configuration file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		name := args[0]

		cfg, err := configManager.Get(name)
		if err != nil {
			return err
		}

		fmt.Printf("Name: %s\n", cfg.Name)
		fmt.Printf("Target: %s\n", cfg.TargetPath)
		fmt.Printf("Created: %s\n", cfg.CreatedAt)
		fmt.Printf("Updated: %s\n", cfg.UpdatedAt)

		return nil
	},
}

func init() {
	configCmd.AddCommand(configCreateCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configExportCmd)
	configCmd.AddCommand(configDeleteCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
}
