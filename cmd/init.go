package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/storage"
	"golang.org/x/term"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new senv project",
	Long: `Initialize a new senv project with encrypted storage.
This will create the necessary directory structure and configuration files.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	configPath := getConfigPath()
	dataPath := getDataPath()

	// Check if already initialized
	manager := storage.NewManager(configPath, dataPath)
	if manager.IsInitialized() {
		return fmt.Errorf("project already initialized at %s", configPath)
	}

	// Guard: if encrypted data files already exist without metadata, refuse to
	// avoid minting a new key that would render them undecryptable.
	if manager.HasOrphanedData() {
		return fmt.Errorf("%w\n\nData directory %q already contains encrypted files but no metadata.json.\n"+
			"Re-running init will generate a new key and make them undecryptable.\n"+
			"Restore metadata.json from version control, or back up and remove the\n"+
			"existing data before initializing.",
			storage.ErrOrphanedData, dataPath)
	}

	// Prompt for password
	password, err := promptPassword("Senv - Enter password for encryption: ")
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	// Confirm password
	confirmPassword, err := promptPassword("Senv - Confirm password: ")
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	// Verify passwords match
	if password != confirmPassword {
		return fmt.Errorf("passwords do not match")
	}

	// Initialize
	fmt.Printf("Initializing senv project...\n")
	fmt.Printf("  Config path: %s\n", configPath)
	fmt.Printf("  Data path: %s\n", dataPath)
	if err := manager.Initialize(password); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	fmt.Println("✓ Project initialized successfully!")
	fmt.Println()
	fmt.Println("Quick start:")
	fmt.Println("  senv env set DATABASE_URL \"postgres://localhost/db\"")
	fmt.Println("  senv env set --group prod API_KEY \"sk-xxx\"")
	fmt.Println("  senv env list")
	fmt.Println("  eval $(senv env export)")

	return nil
}

func promptPassword(prompt string) (string, error) {
	// Check if stdin is a terminal
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr) // New line after password
		return string(password), err
	}

	// Fallback for non-terminal input
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, prompt)
	password, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(password), nil
}
