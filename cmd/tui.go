package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

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
password (does not write session). Startup never waits on the network: local
data renders first and the server sync completes in the background; --refresh
forces that background pull past the throttle window. See "TUI mode" in the
README for the keybinding reference.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		auditMgr := session.NewManager(getConfigPath(), getDataPath())
		defer auditMgr.Close()

		m := tui.New(tui.Managers{
			Env:         envMgr,
			Text:        textMgr,
			Config:      configMgr,
			SSH:         sshMgr,
			LLM:         llmMgr,
			LLMPointer:  llmPointer,
			LLMHome:     llmHome,
			LLMCatalog:  catalogCachePath(),
			History:     buildTUIHistorySource(),
			Audit:       tuiAuditSource{},
			AuditWriter: newTUIAuditWriter(auditMgr),
			// Refresh 透传 --refresh：启动后台拉取绕过节流窗口（TUI 内不阻塞）。
			Refresh: refreshRequested(cmd),
			Sync:    newTUISyncSource(),
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

// tuiSyncSource 把 server provider 的自动同步适配为 TUI 底部常驻状态与写后
// 推送。只在 server 模式且未关闭 auto_sync 时构造；其余情况返回 nil，TUI
// 不显示同步状态也不触发 push。
type tuiSyncSource struct {
	sp *provider.ServerProvider

	mu       sync.Mutex
	lastPush time.Time // 最近一次真正执行了网络 push 的时间
}

func newTUISyncSource() tui.SyncSource {
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		return nil
	}
	return &tuiSyncSource{sp: sp}
}

// Status 只读本地状态：待推送条目数与最近一次 pull 时间（零网络）。
func (s *tuiSyncSource) Status() tui.SyncState {
	dirty, lastPull, err := s.sp.LocalSyncSnapshot()
	st := tui.SyncState{Dirty: dirty, Err: err}
	if !lastPull.IsZero() {
		st.Last = lastPull
	}
	s.mu.Lock()
	lastPush := s.lastPush
	s.mu.Unlock()
	if lastPush.After(st.Last) {
		st.Last = lastPush
	}
	return st
}

// Push 在 autoSyncPushBudget 内做一次 best-effort 推送；失败只反映在状态里，
// 不阻塞界面也不影响已落盘的本地数据。
func (s *tuiSyncSource) Push() tui.SyncState {
	ctx, cancel := context.WithTimeout(context.Background(), autoSyncPushBudget)
	defer cancel()
	out, err := s.sp.AutoPush(ctx, autoSyncPushBudget)
	if err == nil && out != nil && out.Skip == provider.AutoSyncRan {
		s.mu.Lock()
		s.lastPush = time.Now()
		s.mu.Unlock()
	}
	st := s.Status()
	if err != nil {
		st.Err = err
	}
	return st
}

// Pull 在 autoSyncPullBudget 内做一次 best-effort 拉取。与命令行的 autoPull
// 不同：结果只体现在返回的 outcome 里（错误栏/toast 由 TUI 决定），不打印、
// 不退出进程——被屏蔽的提示经错误栏可见。
func (s *tuiSyncSource) Pull(refresh bool) tui.PullOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), autoSyncPullBudget)
	defer cancel()
	res, _, err := s.sp.AutoPull(ctx, s.sp.SyncThrottleWindow(), refresh)
	if err != nil {
		if errors.Is(err, provider.ErrClientBlocked) {
			auditOp(session.AuditOpSync, "vault:"+syncVaultName(), false, "auto pull 被屏蔽拦截")
		} else {
			auditOp(session.AuditOpSync, "vault:"+syncVaultName(), false, "auto pull 失败")
		}
		return tui.PullOutcome{Err: err}
	}
	out := tui.PullOutcome{}
	if res != nil {
		out.Applied = res.Applied
		out.MetadataUpdated = res.MetadataUpdated
		if out.Applied > 0 || out.MetadataUpdated {
			auditOp(session.AuditOpSync, "vault:"+syncVaultName(), true, fmt.Sprintf("auto pull %d 条", out.Applied))
		}
	}
	return out
}

// tuiAuditWriter 把 session.AuditLogger 适配为 TUI 写路径审计。日志写入是
// best-effort 的：失败不影响 TUI 内操作。nil logger 时返回 nil，让 TUI 完全
// 不记录（接口为 nil 而非包着 nil 指针的接口）。
type tuiAuditWriter struct {
	al *session.AuditLogger
}

func newTUIAuditWriter(mgr *session.Manager) tui.AuditWriter {
	al := mgr.GetAuditLogger()
	if al == nil {
		return nil
	}
	return tuiAuditWriter{al: al}
}

func (w tuiAuditWriter) Record(eventType session.AuditEventType, target string, success bool, detail string) {
	_ = w.al.LogOp(eventType, target, success, detail)
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
