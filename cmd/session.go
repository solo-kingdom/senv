package cmd

import (
	"fmt"
	"os"
	"strings"
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
Caches are per vault: one slot per data path, so switching vaults never
overwrites another vault's session.

Session timeout can be configured as:
  - Duration: 30m, 8h, 1d, 1y
  - Special: restart (until system restart)

Security considerations:
  - Only the derived key is cached, not your password
  - The cache lives in a platform-verified secure store: the macOS Keychain on
    Darwin, or a verified memory-backed filesystem (tmpfs/ramfs) elsewhere
  - XDG_RUNTIME_DIR is preferred on Linux; fallback is allowed only on another
    verified memory-backed filesystem, in a random 0700 directory
  - The cache is 0600 and never written to persistent storage unless you
    explicitly opt in with --insecure-cache for headless/CI environments
  - Cache includes a hash of your data path for validation
  - Use 'session clear' to clear this vault's cache, or 'session clear --all'
    to clear every vault's cache and legacy residue`,
}

// sessionTimeoutFromSettings resolves the default timeout for credential-based
// session creation (session start) and opt-in auto rebuild.
func sessionTimeoutFromSettings(configPath, dataPath string) (*session.SessionTimeout, error) {
	timeoutStr := "8h"
	if settings, err := storage.NewManager(configPath, dataPath).LoadSettings(); err == nil {
		if configured := strings.TrimSpace(settings.Session.Timeout); configured != "" {
			timeoutStr = configured
		}
	}
	return session.ParseTimeout(timeoutStr)
}

// timeoutForRenewal resolves the timeout used when extending an existing
// session: an explicit flag wins, otherwise the session's current policy is
// preserved instead of silently switching to the configured default.
func timeoutForRenewal(cache *session.SessionCache, flagValue, configPath, dataPath string) (*session.SessionTimeout, error) {
	if strings.TrimSpace(flagValue) != "" {
		return session.ParseTimeout(flagValue)
	}
	if cache != nil {
		switch cache.TimeoutType {
		case string(session.TimeoutRestart):
			return &session.SessionTimeout{Type: session.TimeoutRestart}, nil
		case string(session.TimeoutDuration):
			if cache.TimeoutSeconds > 0 {
				return &session.SessionTimeout{
					Type:  session.TimeoutDuration,
					Value: time.Duration(cache.TimeoutSeconds) * time.Second,
				}, nil
			}
		}
	}
	return sessionTimeoutFromSettings(configPath, dataPath)
}

var sessionStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start or extend a session",
	Long: `Start a new session with a specified timeout.

If no timeout is specified, uses the default from settings (8h). When a valid
session already exists for this vault, the command extends it directly from the
cached key and does NOT prompt for a password; otherwise it prompts once and
writes a fresh session. All timeout modes require a platform-verified secure
store (macOS Keychain, or a verified memory-backed filesystem on Linux);
otherwise the command fails without writing the derived key unless
--insecure-cache is explicitly set.

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
		autoPull(cmd, refreshRequested(cmd))
		storageManager := getStorage()

		if !storageManager.IsInitialized() {
			return fmt.Errorf("project not initialized. Run 'senv init' first")
		}

		configPath := getConfigPath()
		dataPath := getDataPath()

		// Get timeout from flag or settings
		timeoutStr, _ := cmd.Flags().GetString("timeout")
		timeout, err := parseTimeoutValue(timeoutStr, configPath, dataPath)
		if err != nil {
			return err
		}

		insecureCache, _ := cmd.Flags().GetBool("insecure-cache")
		if insecureCache {
			fmt.Fprintln(os.Stderr, session.InsecureCacheWarning)
			session.EnableInsecureCache()
		}

		sessionManager := session.NewManager(configPath, dataPath)
		defer sessionManager.Close()

		// A valid session can be extended from the cached key: no password.
		if sessionManager.HasValidSession() {
			existing, _ := sessionManager.LoadCache()
			timeout, err = timeoutForRenewal(existing, timeoutStr, configPath, dataPath)
			if err != nil {
				return err
			}
			if timeout == nil {
				return fmt.Errorf("session cache is disabled in configuration")
			}
			if err := sessionManager.RenewSession(timeout); err != nil {
				return err
			}
			printSessionStarted(timeout, true)
			return nil
		}

		// Prompt for password
		password, err := promptPassword("Senv - Enter password: ")
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}

		if err := sessionManager.StartSession(password, timeout); err != nil {
			return err
		}
		printSessionStarted(timeout, false)
		return nil
	},
}

// parseTimeoutValue parses an explicit --timeout or falls back to settings.
func parseTimeoutValue(flagValue, configPath, dataPath string) (*session.SessionTimeout, error) {
	var (
		timeout *session.SessionTimeout
		err     error
	)
	if strings.TrimSpace(flagValue) == "" {
		timeout, err = sessionTimeoutFromSettings(configPath, dataPath)
	} else {
		timeout, err = session.ParseTimeout(flagValue)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid timeout: %w", err)
	}
	if timeout == nil {
		return nil, fmt.Errorf("session cache is disabled in configuration")
	}
	return timeout, nil
}

func printSessionStarted(timeout *session.SessionTimeout, refreshed bool) {
	verb := "Session started"
	if refreshed {
		verb = "Session refreshed"
	}
	switch timeout.Type {
	case session.TimeoutDuration:
		fmt.Printf("✓ %s (expires in %s)\n", verb, timeout.String())
	case session.TimeoutRestart:
		fmt.Printf("✓ %s (valid until system restart)\n", verb)
	}
}

var sessionRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Extend the current vault's session without a password",
	Long: `Extend an already-valid session using the cached derived key.

This never prompts for a password. When the session is expired, invalidated or
unverifiable it reports the reason and next step instead; the cache is not
deleted and no new session is created.

Examples:
  senv session refresh
  senv session refresh --timeout 12h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		storageManager := getStorage()
		if !storageManager.IsInitialized() {
			return fmt.Errorf("project not initialized. Run 'senv init' first")
		}
		configPath := getConfigPath()
		dataPath := getDataPath()

		timeoutStr, _ := cmd.Flags().GetString("timeout")

		sessionManager := session.NewManager(configPath, dataPath)
		defer sessionManager.Close()

		status := sessionManager.DescribeCache()
		switch status.State {
		case session.StateNoSession:
			return fmt.Errorf("no active session for this vault; run: senv session start")
		case session.StateActive:
			// continue
		case session.StateUnverifiable:
			return fmt.Errorf("cannot refresh: session is unverifiable (%s); cache retained. %s",
				sessionReasonText(status.Reason), status.Detail)
		default:
			return fmt.Errorf("cannot refresh: session %s (%s); run: senv session start",
				status.State, sessionReasonText(status.Reason))
		}

		timeout, err := timeoutForRenewal(status.Cache, timeoutStr, configPath, dataPath)
		if err != nil {
			return err
		}
		if timeout == nil {
			return fmt.Errorf("session cache is disabled in configuration")
		}
		if err := sessionManager.RenewSession(timeout); err != nil {
			return err
		}
		printSessionStarted(timeout, true)
		return nil
	},
}

var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show session status",
	Long:  `Display information about the current vault's session cache, including the reason when it is not usable.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := getConfigPath()
		dataPath := getDataPath()
		sessionManager := session.NewManager(configPath, dataPath)
		defer sessionManager.Close()

		status := sessionManager.DescribeCache()
		cache := status.Cache

		switch status.State {
		case session.StateNoSession:
			fmt.Println("Session: No active session")
			return nil
		case session.StateActive:
			fmt.Println("Session: Active")
			printSessionIdentity(cache)
			switch cache.TimeoutType {
			case string(session.TimeoutDuration):
				remaining := time.Until(cache.ExpiresAt)
				fmt.Printf("Timeout: %s\n", cache.TimeoutType)
				fmt.Printf("Expires: %s (in %s)\n",
					cache.ExpiresAt.Format("2006-01-02 15:04:05"),
					remaining.Round(time.Minute))
				if cache.TimeoutSeconds > 0 {
					fmt.Printf("Sliding window: %s, session cap: %s\n",
						(time.Duration(cache.TimeoutSeconds) * time.Second).String(),
						(cache.CreatedAt.Add(session.DefaultMaxLifetime)).Format("2006-01-02 15:04:05"))
				}
			case string(session.TimeoutRestart):
				fmt.Println("Timeout: until system restart")
				fmt.Printf("Boot ID: %s\n", cache.BootID)
			}
			return nil
		case session.StateExpired:
			fmt.Println("Session: Expired")
			printSessionIdentity(cache)
			fmt.Println("Reason: internal timeout elapsed")
			fmt.Println("Cache: will be cleared on next use")
			fmt.Println("Next: senv session start")
			return nil
		case session.StateInvalidated:
			fmt.Println("Session: Invalidated")
			printSessionIdentity(cache)
			fmt.Printf("Reason: %s\n", sessionReasonText(status.Reason))
			fmt.Println("Cache: will be cleared on next use")
			fmt.Println("Next: senv session start")
			return nil
		default:
			fmt.Println("Session: Unverifiable")
			printSessionIdentity(cache)
			fmt.Printf("Reason: %s\n", sessionReasonText(status.Reason))
			if status.Detail != "" {
				fmt.Printf("Detail: %s\n", status.Detail)
			}
			fmt.Println("Cache: retained (not deleted)")
			fmt.Println("Next: resolve the cause above, then retry; `senv session clear --all` discards it")
			return nil
		}
	},
}

func printSessionIdentity(cache *session.SessionCache) {
	if cache == nil {
		return
	}
	fmt.Printf("Session ID: %s\n", cache.SessionID)
	fmt.Printf("Created: %s\n", cache.CreatedAt.Format("2006-01-02 15:04:05"))
}

func sessionReasonText(reason session.InvalidReason) string {
	switch reason {
	case session.ReasonExpired:
		return "internal timeout elapsed"
	case session.ReasonRestarted:
		return "system rebooted since the session was created"
	case session.ReasonVaultChanged:
		return "cache belongs to a different vault"
	case session.ReasonUnknownType:
		return "unknown session timeout type; run: senv session clear --all"
	case session.ReasonUnreadable:
		return "environment prevented verification (boot ID or secure store unavailable)"
	default:
		return "unknown"
	}
}

var sessionClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear session cache",
	Long: `Clear the current vault's session cache, requiring password re-entry on next command.

By default only this vault's slot is removed; other vaults keep their sessions.
Use --all to remove every vault slot plus legacy single-cache residue.

This is useful when:
  - You want to ensure your credentials are cleared
  - You suspect the cache may be compromised
  - You're switching between different projects`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := getConfigPath()
		dataPath := getDataPath()
		sessionManager := session.NewManager(configPath, dataPath)
		defer sessionManager.Close()

		all, _ := cmd.Flags().GetBool("all")
		var err error
		if all {
			err = sessionManager.ClearAllSessions()
		} else {
			err = sessionManager.ClearSession()
		}
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No active session to clear")
				return nil
			}
			return err
		}

		if all {
			fmt.Println("✓ All session caches cleared")
		} else {
			fmt.Println("✓ Session cache cleared (current vault)")
		}
		return nil
	},
}

func init() {
	// Add flags
	sessionStartCmd.Flags().StringP("timeout", "t", "",
		"Session timeout (e.g., 30m, 8h, 1d, 1y, restart)")
	sessionStartCmd.Flags().Bool("insecure-cache", false,
		"store the session key on disk (0600) for headless/CI use; insecure")
	addRefreshFlag(sessionStartCmd)

	sessionRefreshCmd.Flags().StringP("timeout", "t", "",
		"Session timeout to extend with (defaults to settings)")

	sessionClearCmd.Flags().Bool("all", false,
		"clear every vault's session slot plus legacy residue")

	// Add subcommands
	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionRefreshCmd)
	sessionCmd.AddCommand(sessionStatusCmd)
	sessionCmd.AddCommand(sessionClearCmd)

	// Add to root
	rootCmd.AddCommand(sessionCmd)
}
