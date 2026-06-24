package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
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
		return text.NewManagerWithKey(manager, key), nil
	}

	// Prompt for password
	password, err := promptPassword("Senv - Enter password: ")
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

	// Save session cache if enabled
	settings, err := manager.LoadSettings()
	if err == nil && settings.Session.Enabled {
		timeout, err := session.ParseTimeout(settings.Session.Timeout)
		if err == nil && timeout != nil {
			sessionManager.StartSession(password, timeout)
		}
	}

	return text.NewManager(manager, password), nil
}

// isPipe checks if stdin is a pipe (not a terminal)
func isPipe() bool {
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// --- text set ---

var textSetFile string

var textSetCmd = &cobra.Command{
	Use:   "set <key> [value]",
	Short: "Set a text block",
	Long: `Set a text block. Input priority: --file > stdin pipe > argument > editor.
When no value is provided and stdin is a terminal, opens an editor.
If the key already exists, the editor will be pre-filled with the existing content.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		key := args[0]

		// Priority: --file > stdin > args > editor
		if textSetFile != "" {
			return textManager.SetFromFile(textGroup, key, textSetFile)
		}

		if isPipe() {
			return textManager.SetFromReader(textGroup, key, os.Stdin)
		}

		if len(args) >= 2 {
			return textManager.Set(textGroup, key, args[1])
		}

		// Open editor
		return textManager.SetViaEditor(textGroup, key)
	},
}

// --- text get ---

var (
	textGetDecode bool
	textGetLoose  bool
	textGetOutput string
	textGetCopy   bool
)

var textGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a text block",
	Long: `Get a text block value. By default outputs the raw value.
Use -d/--decode to resolve {{env:...}} and {{text:...}} references.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		key := args[0]
		value, err := textManager.Get(textGroup, key)
		if err != nil {
			return err
		}

		// Resolve references if -d flag is set
		if textGetDecode {
			resolved, err := resolveValue(value, textGetLoose, textGroup)
			if err != nil {
				return err
			}
			value = resolved
		}

		// Output
		if textGetCopy {
			return textManager.GetToClipboard(textGroup, key)
		}

		if textGetOutput != "" {
			return textManager.GetToFile(textGroup, key, textGetOutput)
		}

		fmt.Print(value)
		return nil
	},
}

// --- text delete ---

var textDeleteCmd = &cobra.Command{
	Use:   "delete <key>",
	Short: "Delete a text block",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		textManager, err := getTextManager()
		if err != nil {
			return err
		}

		key := args[0]
		if err := textManager.Delete(textGroup, key); err != nil {
			return err
		}

		fmt.Printf("✓ Deleted text %s from group %s\n", key, textGroup)
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
			fmt.Printf("  %-20s %6d bytes  %s\n",
				info.Key,
				info.Size,
				info.UpdatedAt.Format("2006-01-02 15:04"))
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
			fmt.Printf("  %s (%d keys)\n", g.Name, g.KeyCount)
		}

		return nil
	},
}

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
		if err := textManager.AddGroup(name); err != nil {
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
			return err
		}

		fmt.Printf("✓ Deleted text group %s\n", name)
		return nil
	},
}

// resolveValue resolves references in a value using the ref package
func resolveValue(value string, loose bool, currentGroup string) (string, error) {
	// We need both env and text getters
	// Create a combined getter using the current session
	getter, err := newCombinedGetter()
	if err != nil {
		return "", err
	}

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

// combinedGetter implements ref.ValueGetter using env and text managers
type combinedGetter struct {
	envManager interface {
		Get(group, key string) (string, error)
	}
	textManager *text.Manager
}

func newCombinedGetter() (*combinedGetter, error) {
	envMgr, err := getEnvManager()
	if err != nil {
		return nil, err
	}

	textMgr, err := getTextManager()
	if err != nil {
		return nil, err
	}

	return &combinedGetter{
		envManager:  envMgr,
		textManager: textMgr,
	}, nil
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

	// text get flags
	textGetCmd.Flags().BoolVarP(&textGetDecode, "decode", "d", false, "resolve {{env:...}} and {{text:...}} references")
	textGetCmd.Flags().BoolVar(&textGetLoose, "loose", false, "keep unresolved references as-is instead of erroring")
	textGetCmd.Flags().StringVarP(&textGetOutput, "output", "o", "", "write output to file")
	textGetCmd.Flags().BoolVar(&textGetCopy, "copy", false, "copy output to clipboard")

	// Register subcommands
	textCmd.AddCommand(textSetCmd)
	textCmd.AddCommand(textGetCmd)
	textCmd.AddCommand(textDeleteCmd)
	textCmd.AddCommand(textListCmd)
	textCmd.AddCommand(textGroupCmd)

	textGroupCmd.AddCommand(textGroupListCmd)
	textGroupCmd.AddCommand(textGroupAddCmd)
	textGroupCmd.AddCommand(textGroupDeleteCmd)
}
