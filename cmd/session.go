package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage session cache",
	Long: `Manage session cache to avoid repeated password prompts.

The session cache stores a derived encryption key in a secure temporary file,
allowing you to run multiple senv commands without re-entering your password.

Session timeout can be configured as:
  - Duration: 30m, 8h, 1d, 1y
  - Special: restart (until system restart), never (not recommended)

Security considerations:
  - Only the derived key is cached, not your password
  - Cache file has 0600 permissions (only your user can read it)
  - Cache includes a hash of your data path for validation
  - Use 'session clear' to manually clear the cache`,
}

var sessionStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a new session",
	Long: `Start a new session with a specified timeout.

If no timeout is specified, uses the default from settings (8h).

Examples:
  # Start session with default timeout
  senv session start

  # Start session with 12 hour timeout
  senv session start --timeout 12h

  # Start session that lasts until system restart
  senv session start --timeout restart

  # Start session with 1 day timeout
  senv session start --timeout 1d`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := getDataPath()
		storageManager := storage.NewManager(path)

		if !storageManager.IsInitialized() {
			return fmt.Errorf("project not initialized. Run 'senv init' first")
		}

		// Get timeout from flag or settings
		timeoutStr, _ := cmd.Flags().GetString("timeout")
		if timeoutStr == "" {
			// Load default timeout from settings
			settings, err := storageManager.LoadSettings()
			if err == nil {
				timeoutStr = settings.Session.Timeout
			} else {
				timeoutStr = "8h" // Fallback to 8h
			}
		}

		// Parse timeout
		timeout, err := session.ParseTimeout(timeoutStr)
		if err != nil {
			return fmt.Errorf("invalid timeout: %w", err)
		}

		if timeout == nil {
			return fmt.Errorf("session cache is disabled in configuration")
		}

		// Prompt for password
		password, err := promptPassword("Enter password: ")
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}

		// Start session
		sessionManager := session.NewManager(path)
		if err := sessionManager.StartSession(password, timeout); err != nil {
			return err
		}

		// Print success message
		switch timeout.Type {
		case session.TimeoutDuration:
			fmt.Printf("✓ Session started (expires in %s)\n", timeout.String())
		case session.TimeoutRestart:
			fmt.Println("✓ Session started (valid until system restart)")
		case session.TimeoutNever:
			fmt.Println("✓ Session started (never expires)")
		}

		return nil
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show session status",
	Long:  `Display information about the current session cache.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := getDataPath()
		sessionManager := session.NewManager(path)

		cache, err := sessionManager.LoadCache()
		if err != nil {
			fmt.Println("Session: Error loading cache")
			return err
		}

		if cache == nil {
			fmt.Println("Session: No active session")
			return nil
		}

		valid, err := sessionManager.IsCacheValid(cache)
		if err != nil {
			fmt.Printf("Session: Invalid (%v)\n", err)
			return nil
		}

		if !valid {
			fmt.Println("Session: Expired")
			fmt.Printf("Session ID: %s\n", cache.SessionID)
			fmt.Printf("Created: %s\n", cache.CreatedAt.Format("2006-01-02 15:04:05"))
			return nil
		}

		fmt.Println("Session: Active")
		fmt.Printf("Session ID: %s\n", cache.SessionID)
		fmt.Printf("Created: %s\n", cache.CreatedAt.Format("2006-01-02 15:04:05"))

		switch cache.TimeoutType {
		case string(session.TimeoutDuration):
			remaining := time.Until(cache.ExpiresAt)
			if remaining > 0 {
				fmt.Printf("Timeout: %s\n", cache.TimeoutType)
				fmt.Printf("Expires: %s (in %s)\n",
					cache.ExpiresAt.Format("2006-01-02 15:04:05"),
					remaining.Round(time.Minute))
			} else {
				fmt.Println("Status: Expired")
			}
		case string(session.TimeoutRestart):
			fmt.Println("Timeout: until system restart")
			fmt.Printf("Boot ID: %s\n", cache.BootID)
		case string(session.TimeoutNever):
			fmt.Println("Timeout: never expires")
		}

		return nil
	},
}

var sessionClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear session cache",
	Long: `Clear the session cache, requiring password re-entry on next command.

This is useful when:
  - You want to ensure your credentials are cleared
  - You suspect the cache may be compromised
  - You're switching between different projects`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := getDataPath()
		sessionManager := session.NewManager(path)

		err := sessionManager.ClearSession()
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No active session to clear")
				return nil
			}
			return err
		}

		fmt.Println("✓ Session cache cleared")
		return nil
	},
}

func init() {
	// Add flags
	sessionStartCmd.Flags().StringP("timeout", "t", "",
		"Session timeout (e.g., 30m, 8h, 1d, 1y, restart, never)")

	// Add subcommands
	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionClearCmd)

	// Add to root
	rootCmd.AddCommand(sessionCmd)
}
