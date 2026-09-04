package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/exportfile"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration files",
	Long:  `Manage encrypted configuration files with target path mapping.`,
}

var (
	configTargetPath  string
	configSourcePath  string
	configGroup       string
	configDescription string
	configListGroup   string
	configInstallAll  bool
	configDryRun      bool
	configYes         bool
	configExportMode  string
	configInstallMode string
)

func init() {
	rootCmd.AddCommand(configCmd)
}

func getConfigManager() (*config.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
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

		if err := configManager.Create(name, configSourcePath, configTargetPath, configGroup, configDescription); err != nil {
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
	configCreateCmd.Flags().StringVar(&configGroup, "group", "", "group name (default: default)")
	configCreateCmd.Flags().StringVar(&configDescription, "description", "", "human-readable description")
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
You can override the target path with --path. New plaintext files default to
0600; use --mode 0644 only to explicitly share non-secret output. Existing
files with stricter permissions are not widened.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := exportfile.ParseFileMode(configExportMode)
		if err != nil {
			return err
		}
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		name := args[0]

		return configManager.ExportWithMode(name, configTargetPath, mode)
	},
}

func init() {
	configExportCmd.Flags().StringVar(&configTargetPath, "path", "", "override target path for export")
	configExportCmd.Flags().StringVar(&configExportMode, "mode", "0600", "output file permissions in strict octal form (0000-0777)")
}

// configInstallCmd represents the config install command
var configInstallCmd = &cobra.Command{
	Use:   "install [name] [--group g | --all]",
	Short: "Install configuration files to their target paths",
	Long: `Install configuration files to the target paths recorded in their meta.
Shows a plan first and asks for confirmation before writing. Use --dry-run to
only show the plan, --yes to skip the confirmation prompt. New plaintext files
default to 0600; --mode is an explicit per-command permission choice.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, err := exportfile.ParseFileMode(configInstallMode)
		if err != nil {
			return err
		}
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		scope := config.Scope{Group: configListGroup, All: configInstallAll}
		if len(args) == 1 {
			scope.Name = args[0]
		}

		plan, err := configManager.PlanInstall(scope)
		if err != nil {
			return err
		}

		printInstallPlan(plan)
		if configDryRun {
			return nil
		}

		if !configYes && !confirmPrompt("执行以上安装计划？(y/N): ") {
			fmt.Println("已取消安装")
			return nil
		}

		return configManager.ExecuteInstallWithMode(plan, mode)
	},
}

func printInstallPlan(plan *config.InstallPlan) {
	fmt.Println("Install plan:")
	for _, item := range plan.Items {
		if item.Action == config.ActionError {
			fmt.Printf("  [error] %s: %s\n", item.Name, item.Reason)
		} else {
			fmt.Printf("  [%s] %s -> %s (%s)\n", item.Action, item.Name, item.TargetPath, item.Reason)
		}
	}
}

// confirmPrompt prints prompt and returns true only for y/yes.
func confirmPrompt(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

// configUninstallCmd represents the config uninstall command
var configUninstallCmd = &cobra.Command{
	Use:   "uninstall [name] [--group g | --all]",
	Short: "Remove installed configuration files from their target paths",
	Long: `Remove the files installed at the target paths recorded in config meta.
Shows a plan first and asks for confirmation. Files modified locally are
marked as changed and require explicit per-item confirmation. The encrypted
storage entries are never touched.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		scope := config.Scope{Group: configListGroup, All: configInstallAll}
		if len(args) == 1 {
			scope.Name = args[0]
		}

		plan, err := configManager.PlanUninstall(scope)
		if err != nil {
			return err
		}

		printUninstallPlan(plan)
		if configDryRun {
			return nil
		}

		if !configYes && !confirmPrompt("执行以上卸载计划？(y/N): ") {
			fmt.Println("已取消卸载")
			return nil
		}

		// --yes confirms everything including changed items; otherwise ask
		// per changed item.
		confirmChanged := func(item config.UninstallItem) bool {
			return confirmPrompt(fmt.Sprintf("目标文件已被本地修改，确认删除 %s? [y/N]: ", item.TargetPath))
		}
		if configYes {
			confirmChanged = func(config.UninstallItem) bool { return true }
		}

		return configManager.ExecuteUninstall(plan, confirmChanged)
	},
}

func printUninstallPlan(plan *config.UninstallPlan) {
	fmt.Println("Uninstall plan:")
	for _, item := range plan.Items {
		switch item.Action {
		case config.ActionError:
			fmt.Printf("  [error] %s: %s\n", item.Name, item.Reason)
		case config.ActionChanged:
			fmt.Printf("  [CHANGED] %s -> %s (%s)\n", item.Name, item.TargetPath, item.Reason)
		default:
			fmt.Printf("  [%s] %s -> %s (%s)\n", item.Action, item.Name, item.TargetPath, item.Reason)
		}
	}
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
		autoPull(cmd, refreshRequested(cmd))
		configManager, err := getConfigManager()
		if err != nil {
			return err
		}

		configs, err := configManager.List(configListGroup)
		if err != nil {
			return err
		}

		if len(configs) == 0 {
			fmt.Println("No configuration files found")
			return nil
		}

		fmt.Println("Configuration files:")
		for _, cfg := range configs {
			fmt.Printf("  %s [%s]\n", cfg.Name, cfg.Group)
			if cfg.Description != "" {
				fmt.Printf("    Description: %s\n", cfg.Description)
			}
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
		autoPull(cmd, refreshRequested(cmd))
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
		fmt.Printf("Group: %s\n", cfg.Group)
		fmt.Printf("Description: %s\n", cfg.Description)
		fmt.Printf("Target: %s\n", cfg.TargetPath)
		fmt.Printf("Created: %s\n", cfg.CreatedAt)
		fmt.Printf("Updated: %s\n", cfg.UpdatedAt)

		return nil
	},
}

func init() {
	configListCmd.Flags().StringVar(&configListGroup, "group", "", "filter by group")
	addRefreshFlag(configListCmd)

	configInstallCmd.Flags().StringVar(&configListGroup, "group", "", "install all configs in a group")
	configInstallCmd.Flags().BoolVar(&configInstallAll, "all", false, "install all configs")
	configInstallCmd.Flags().BoolVar(&configDryRun, "dry-run", false, "show the plan without executing")
	configInstallCmd.Flags().BoolVar(&configYes, "yes", false, "skip confirmation prompt")
	configInstallCmd.Flags().StringVar(&configInstallMode, "mode", "0600", "output file permissions in strict octal form (0000-0777)")

	configUninstallCmd.Flags().StringVar(&configListGroup, "group", "", "uninstall all configs in a group")
	configUninstallCmd.Flags().BoolVar(&configInstallAll, "all", false, "uninstall all configs")
	configUninstallCmd.Flags().BoolVar(&configDryRun, "dry-run", false, "show the plan without executing")
	configUninstallCmd.Flags().BoolVar(&configYes, "yes", false, "skip confirmation prompts (including modified files)")

	configCmd.AddCommand(configCreateCmd)
	configCmd.AddCommand(configEditCmd)
	configCmd.AddCommand(configExportCmd)
	configCmd.AddCommand(configDeleteCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configInstallCmd)
	configCmd.AddCommand(configUninstallCmd)
	configCmd.AddCommand(configGetCmd)
	addRefreshFlag(configGetCmd)
}
