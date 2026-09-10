package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
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
		autoPull(cmd, refreshRequested(cmd))
		envMgr, textMgr, configMgr, err := getManagers()
		if err != nil {
			return err
		}
		sshMgr, err := getSSHManager()
		if err != nil {
			return err
		}
		// LLM 管理器在 vault 可用时注入；不可用（如 git 模式）时 AI Tab
		// 不注册，TUI 其余功能不受影响。
		llmMgr, llmErr := getAIProviderManager()
		var llmPointer string
		var llmHome string
		if llmErr == nil {
			llmPointer = filepath.Join(getConfigPath(), "agent-pointers.json")
			llmHome, err = agentHomeDir()
			if err != nil {
				return err
			}
		}

		m := tui.New(tui.Managers{
			Env:        envMgr,
			Text:       textMgr,
			Config:     configMgr,
			SSH:        sshMgr,
			LLM:        llmMgr,
			LLMPointer: llmPointer,
			LLMHome:    llmHome,
			History:    buildTUIHistorySource(),
			Audit:      tuiAuditSource{},
		})
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run TUI: %w", err)
		}
		return nil
	},
}

// tuiHistorySource 把 server provider 与已认证 key 适配为 TUI 的 History
// 数据源（server 模式才注入；git 模式返回 nil，TUI 不注册该 Tab）。
type tuiHistorySource struct {
	sp  *provider.ServerProvider
	key []byte
}

func (s *tuiHistorySource) History(ctx context.Context, f provider.HistoryFilter) ([]provider.HistoryVersion, error) {
	return s.sp.History(ctx, f)
}

func (s *tuiHistorySource) DecryptHistory(v provider.HistoryVersion) (string, error) {
	plaintext, err := decryptHistoryValue(v, s.key)
	if err != nil {
		return "", err
	}
	return renderDecryptedHistory(v.Kind, plaintext), nil
}

func (s *tuiHistorySource) Restore(ctx context.Context, v provider.HistoryVersion) error {
	return s.sp.RestoreEntry(ctx, v.Kind, v.Grp, v.Key, v.Ciphertext)
}

// tuiAuditSource 把本机审计文件读取器适配为 TUI 的 Audit 数据源
type tuiAuditSource struct{}

func (tuiAuditSource) LoadAuditEvents() ([]session.AuditEntry, int, error) {
	return loadAuditEntries()
}

// buildTUIHistorySource 构造 History 数据源；任何一步不可用（git 模式、
// 未认证、server 不支持）都返回 nil，让 TUI 优雅降级为无 History Tab。
func buildTUIHistorySource() tui.HistorySource {
	p, err := getSyncProvider()
	if err != nil {
		return nil
	}
	sp, ok := p.(*provider.ServerProvider)
	if !ok {
		return nil
	}
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil
	}
	key, err := resolveKeyForAuth(auth)
	if err != nil {
		return nil
	}
	return &tuiHistorySource{sp: sp, key: key}
}

func init() {
	rootCmd.AddCommand(tuiCmd)
	addRefreshFlag(tuiCmd)
}

// getManagers authenticates the user and returns all three domain managers.
//
// Startup validation:
//   - project not initialized -> error, the command exits without entering TUI
//   - valid session cache     -> reuse derived key, no password prompt
//   - no session + wrong pwd  -> error, the command exits without entering TUI
//   - no session + correct pwd -> temporary auth only (does not write session cache)
func getManagers() (*env.Manager, *text.Manager, *config.Manager, error) {
	return getManagersAt(getConfigPath(), getDataPath(), authPrompt)
}

// passwordPrompter returns a password for the given prompt. Tests inject a
// stub instead of reading from the terminal.
type passwordPrompter func(prompt string) (string, error)

// getManagersAt is the path/prompter-injectable core of getManagers, used by
// tests to drive the startup-validation paths deterministically.
func getManagersAt(configPath, dataPath string, prompt passwordPrompter) (*env.Manager, *text.Manager, *config.Manager, error) {
	auth, err := resolveAuth(configPath, dataPath, prompt)
	if err != nil {
		return nil, nil, nil, err
	}
	if auth.hasKey() {
		return env.NewManagerWithKey(auth.storage, auth.key),
			text.NewManagerWithKey(auth.storage, auth.key),
			config.NewManagerWithKey(auth.storage, auth.key),
			nil
	}
	return env.NewManager(auth.storage, auth.password),
		text.NewManager(auth.storage, auth.password),
		config.NewManager(auth.storage, auth.password),
		nil
}

// errNotInitialized / errInvalidPassword are sentinel errors used by tests.
var (
	errNotInitialized  = fmt.Errorf("project not initialized. Run 'senv init' first")
	errInvalidPassword = fmt.Errorf("invalid password")
)
