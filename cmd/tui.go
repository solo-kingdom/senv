package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
	"github.com/wii/senv/internal/tui"
)

// tuiCmd launches the full-screen TUI for browsing/searching/editing data.
var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the full-screen TUI",
	Long: `Launch the full-screen TUI to browse, search and edit env, text and config.

Reuses a valid session cache when available; otherwise prompts for a one-time
password (does not write session). See "TUI mode" in the README for the
keybinding reference.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		envMgr, textMgr, configMgr, err := getManagers()
		if err != nil {
			return err
		}

		m := tui.New(tui.Managers{
			Env:    envMgr,
			Text:   textMgr,
			Config: configMgr,
		})
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run TUI: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

// getManagers authenticates the user and returns all three domain managers.
//
// Startup validation:
//   - project not initialized -> error, the command exits without entering TUI
//   - valid session cache     -> reuse derived key, no password prompt
//   - no session + wrong pwd  -> error, the command exits without entering TUI
//   - no session + correct pwd -> temporary auth only (does not write session cache)
func getManagers() (*env.Manager, *text.Manager, *config.Manager, error) {
	return getManagersAt(getConfigPath(), getDataPath(), promptPassword)
}

// passwordPrompter returns a password for the given prompt. Tests inject a
// stub instead of reading from the terminal.
type passwordPrompter func(prompt string) (string, error)

// getManagersAt is the path/prompter-injectable core of getManagers, used by
// tests to drive the startup-validation paths deterministically.
func getManagersAt(configPath, dataPath string, prompt passwordPrompter) (*env.Manager, *text.Manager, *config.Manager, error) {
	storageMgr := storage.NewManager(configPath, dataPath)

	if !storageMgr.IsInitialized() {
		return nil, nil, nil, errNotInitialized
	}

	sessionManager := session.NewManager(configPath, dataPath)
	key, err := sessionManager.GetCachedKey()
	if err == nil {
		return env.NewManagerWithKey(storageMgr, key),
			text.NewManagerWithKey(storageMgr, key),
			config.NewManagerWithKey(storageMgr, key),
			nil
	}

	password, err := prompt("Senv - Enter password: ")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read password: %w", err)
	}

	valid, err := storageMgr.VerifyPassword(password)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to verify password: %w", err)
	}
	if !valid {
		return nil, nil, nil, errInvalidPassword
	}

	return env.NewManager(storageMgr, password),
		text.NewManager(storageMgr, password),
		config.NewManager(storageMgr, password),
		nil
}

// errNotInitialized / errInvalidPassword are sentinel errors used by tests.
var (
	errNotInitialized  = fmt.Errorf("project not initialized. Run 'senv init' first")
	errInvalidPassword = fmt.Errorf("invalid password")
)
