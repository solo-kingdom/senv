package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wii/senv/internal/codexdaemon"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ref"
)

// codexDaemonCheckMsg asks the top-level model whether a daemon is running, so
// a tab does not have to know about it. daemon 是否在跑与本机状态有关，与哪个
// Tab 发起的写入无关，因此探测和确认都落在顶层。
type codexDaemonCheckMsg struct {
	// source 说明触发它的写回，渲染在确认弹层正文里。
	source string
}

// codexDaemonStateMsg carries the probe result back to the top level.
type codexDaemonStateMsg struct {
	running bool
	err     error
}

// codexDaemonRestartMsg carries the restart outcome back to the top level.
type codexDaemonRestartMsg struct{ err error }

// codexDaemonPrompt 是顶层持有的一次确认请求。弹层期间所有按键归顶层处理，
// 与 search/help 覆盖层同样的所有权语义。
type codexDaemonPrompt struct {
	// source 说明是哪个写回触发的，用于弹层正文。
	source string
}

// checkCodexDaemon 异步探测本机 daemon。探测本身要拉起 codex 子进程，放主循环
// 里会卡住渲染。source 说明是哪个写回触发的，用于确认弹层的正文。
func (m *Model) checkCodexDaemon(source string) tea.Cmd {
	m.codexPrompt = &codexDaemonPrompt{source: source}
	m.codexPromptStage = codexPromptProbing
	return func() tea.Msg {
		status, err := codexdaemon.Probe(context.Background())
		return codexDaemonStateMsg{running: status.Running, err: err}
	}
}

// codexPromptStage tracks whether the overlay is still probing or awaiting a
// confirmation, so the modal can render an honest state.
type codexPromptStage int

const (
	codexPromptProbing codexPromptStage = iota
	codexPromptConfirm
)

// updateCodexPrompt 处理弹层期间的按键。只有确认态才接受 y/enter；探测态忽略
// 按键，避免用户在结果未知时打进来的键被当成放行。
func (m Model) updateCodexPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.codexPromptStage != codexPromptConfirm {
		return m, nil
	}
	switch msg.String() {
	case "y", "enter":
		m.codexPrompt = nil
		envMgr, getter := m.codexExportSource()
		return m, func() tea.Msg {
			err := codexdaemon.Restart(context.Background(), codexDaemonVars(envMgr, getter))
			return codexDaemonRestartMsg{err: err}
		}
	case "esc", "n":
		source := ""
		if m.codexPrompt != nil {
			source = m.codexPrompt.source
		}
		m.codexPrompt = nil
		return m, warnToast("skipped restarting codex daemon after " + source)
	}
	return m, nil
}

// codexExportSource picks the managers the restart should draw environment
// variables from. It returns nil getter when the vault pieces are unavailable;
// the restart then still runs, just without injected variables.
func (m Model) codexExportSource() (*env.Manager, ref.ValueGetter) {
	if m.mgr.Env == nil || m.mgr.Text == nil {
		return nil, nil
	}
	// 用快照读（与 deref 一致）：重启发生在写回之后，走内存视图不必再解锁。
	vars, _, err := envSnapshot(m.mgr)
	getter := tuiGetter{envMgr: m.mgr.Env, textMgr: m.mgr.Text}
	if err == nil {
		getter.envSnap = vars
	}
	return m.mgr.Env, getter
}

// codexDaemonVars resolves the export set, swallowing failures: a restart with
// no injected credentials is still better than refusing to restart.
func codexDaemonVars(envMgr *env.Manager, getter ref.ValueGetter) map[string]string {
	if envMgr == nil || getter == nil {
		return nil
	}
	vars, _, err := codexdaemon.ExportVars(envMgr, getter)
	if err != nil {
		return nil
	}
	return vars
}

// codexPromptBindings 是确认弹层期间的键位（与其它确认弹层同语义）。
func codexPromptBindings() []KeyAction {
	return confirmBindings()
}

// renderCodexPrompt 渲染确认弹层。
func (m Model) renderCodexPrompt() string {
	if m.codexPrompt == nil {
		return ""
	}
	if m.codexPromptStage == codexPromptProbing {
		return modalBox(m.width, m.height, "codex app-server daemon",
			"checking whether a daemon is running…", "")
	}
	return modalBox(m.width, m.height, "restart codex app-server daemon?",
		fmt.Sprintf("%s was written, but a running daemon keeps the configuration and environment it started with.", m.codexPrompt.source),
		"enter/y restart · esc/n skip")
}

// finishCodexProbe turns the probe result into either a confirmation prompt or
// silence: no daemon means there is nothing to restart, so the overlay closes
// without a word.
func (m Model) finishCodexProbe(msg codexDaemonStateMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || !msg.running {
		m.codexPrompt = nil
		m.codexPromptStage = codexPromptProbing
		return m, nil
	}
	m.codexPromptStage = codexPromptConfirm
	return m, nil
}
