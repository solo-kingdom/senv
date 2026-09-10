package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
)

// aiTab 浏览 LLM provider 档案与各 coding agent 当前指向，并复用
// llm.SwitchManager 在 Tab 内完成切换（写路径不在 TUI，凭据明文只在
// SwitchManager 内部流动）。
type aiTab struct {
	mgr           Managers
	width, height int
	loaded        bool
	loadErr       string

	providers []*storage.LLMProviderEntry
	rows      []llm.StatusRow
	warning   string
	notice    string

	providerIndex int
	focusLeft     bool

	flow       aiFlow
	agentIndex int // llm.SupportedAgents() 下标
	modelIndex int // 选中档案 Models 下标
}

type aiFlow int

const (
	aiFlowNone aiFlow = iota
	aiFlowSelectAgent
	aiFlowSelectModel
	aiFlowConfirm
)

type aiLoadedMsg struct {
	providers []*storage.LLMProviderEntry
	rows      []llm.StatusRow
	warning   string
	err       error
}

type aiSwitchResultMsg struct {
	out *llm.SwitchOutput
	err error
}

func newAITab(mgr Managers) *aiTab {
	return &aiTab{mgr: mgr, focusLeft: true}
}

func (t *aiTab) Title() string { return "AI" }

func (t *aiTab) Help() string {
	if t.flow != aiFlowNone {
		return "↑↓/jk move · enter select · esc cancel"
	}
	return "↑↓/jk move · ←→/hl panes · s switch · r refresh · read-only (credentials masked)"
}

// InputMode 在切换选择流中为 true，避免顶层全局快捷键（数字/Tab/q）打断。
func (t *aiTab) InputMode() bool { return t.flow != aiFlowNone }

func (t *aiTab) SetSize(width, height int) {
	t.width, t.height = width, height
}

func (t *aiTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *aiTab) load() tea.Cmd {
	return func() tea.Msg {
		if t.mgr.LLM == nil {
			return aiLoadedMsg{}
		}
		providers, err := t.mgr.LLM.ListProviders()
		if err != nil {
			return aiLoadedMsg{err: err}
		}
		sm := llm.NewSwitchManager(t.mgr.LLM, t.mgr.LLMPointer, t.mgr.LLMHome)
		rows, warning := sm.Status()
		return aiLoadedMsg{providers: providers, rows: rows, warning: warning}
	}
}

func (t *aiTab) switchCmd() tea.Cmd {
	provider := t.providers[t.providerIndex]
	agent := llm.SupportedAgents()[t.agentIndex]
	model := provider.Models[t.modelIndex]
	sm := llm.NewSwitchManager(t.mgr.LLM, t.mgr.LLMPointer, t.mgr.LLMHome)
	return func() tea.Msg {
		out, err := sm.Switch(agent.ID, provider.Alias, model)
		return aiSwitchResultMsg{out: out, err: err}
	}
}

func (t *aiTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case aiLoadedMsg:
		t.loaded = true
		t.loadErr = ""
		if msg.err != nil {
			t.loadErr = msg.err.Error()
			return t, nil
		}
		t.providers = msg.providers
		t.rows = msg.rows
		t.warning = msg.warning
		t.clamp()
		return t, nil

	case aiSwitchResultMsg:
		if msg.err != nil {
			// 失败走顶层错误横幅；指针与配置由 SwitchManager 保证不变。
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		notice := fmt.Sprintf("✓ %s → %s（模型 %s）", msg.out.AgentName, msg.out.Provider, msg.out.Model)
		if msg.out.CredentialEnv != "" {
			notice += fmt.Sprintf("；%s 从环境变量 %s 读取凭据", msg.out.AgentName, msg.out.CredentialEnv)
		}
		t.notice = notice
		return t, t.load()

	case tea.KeyMsg:
		return t.updateKey(msg)
	}
	return t, nil
}

func (t *aiTab) updateKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	agents := llm.SupportedAgents()
	models := t.currentModels()
	switch t.flow {
	case aiFlowSelectAgent:
		switch msg.String() {
		case "up", "k":
			if t.agentIndex > 0 {
				t.agentIndex--
			}
		case "down", "j":
			if t.agentIndex < len(agents)-1 {
				t.agentIndex++
			}
		case "enter":
			t.modelIndex = t.defaultModelIndex()
			t.flow = aiFlowSelectModel
		case "esc":
			t.flow = aiFlowNone
		}
		return t, nil
	case aiFlowSelectModel:
		switch msg.String() {
		case "up", "k":
			if t.modelIndex > 0 {
				t.modelIndex--
			}
		case "down", "j":
			if t.modelIndex < len(models)-1 {
				t.modelIndex++
			}
		case "enter":
			if len(models) > 0 {
				t.flow = aiFlowConfirm
			}
		case "esc":
			t.flow = aiFlowNone
		}
		return t, nil
	case aiFlowConfirm:
		switch msg.String() {
		case "y", "enter":
			t.flow = aiFlowNone
			return t, t.switchCmd()
		case "n", "esc":
			t.flow = aiFlowNone
		}
		return t, nil
	}

	switch msg.String() {
	case "up", "k":
		if t.focusLeft && t.providerIndex > 0 {
			t.providerIndex--
		}
	case "down", "j":
		if t.focusLeft && t.providerIndex < len(t.providers)-1 {
			t.providerIndex++
		}
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "s":
		if len(t.providers) > 0 {
			t.agentIndex = 0
			t.modelIndex = t.defaultModelIndex()
			t.flow = aiFlowSelectAgent
		}
	case "r":
		t.loaded = false
		return t, t.load()
	case "esc":
		t.notice = ""
	}
	return t, nil
}

func (t *aiTab) currentModels() []string {
	if t.providerIndex < 0 || t.providerIndex >= len(t.providers) {
		return nil
	}
	return t.providers[t.providerIndex].Models
}

// defaultModelIndex 返回 default_model 在模型集中的下标；缺省时取 0。
func (t *aiTab) defaultModelIndex() int {
	models := t.currentModels()
	if t.providerIndex >= len(t.providers) {
		return 0
	}
	want := t.providers[t.providerIndex].DefaultModel
	for i, m := range models {
		if m == want {
			return i
		}
	}
	return 0
}

func (t *aiTab) clamp() {
	if t.providerIndex >= len(t.providers) {
		t.providerIndex = len(t.providers) - 1
	}
	if t.providerIndex < 0 {
		t.providerIndex = 0
	}
}

func (t *aiTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("AI") + "\n" + truncateRunes("⚠ "+t.loadErr, max(t.width, 1))
	}
	if len(t.providers) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("AI"),
			emptyStateStyle.Render("暂无 LLM Provider 档案；执行 senv ai provider add 添加后按 r 刷新"))
	}

	// 左栏：provider 列表。
	providerLines := make([]string, 0, len(t.providers))
	for _, p := range t.providers {
		def := p.DefaultModel
		if def == "" {
			def = "-"
		}
		providerLines = append(providerLines, fmt.Sprintf("%s · 默认 %s · %d 模型", p.Alias, def, len(p.Models)))
	}

	// 右栏：详情 + 指针区 + 提示。
	p := t.providers[t.providerIndex]
	detail := []string{
		fmt.Sprintf("alias: %s", p.Alias),
		fmt.Sprintf("base_url: %s", p.BaseURL),
		fmt.Sprintf("凭据引用: %s", p.CredentialRef),
		fmt.Sprintf("目录来源: %s", orDash(p.CatalogProvider)),
		fmt.Sprintf("模型: %s", strings.Join(p.Models, ", ")),
	}
	pointerLines := make([]string, 0, len(t.rows))
	for _, r := range t.rows {
		pointerLines = append(pointerLines, t.pointerLine(r))
	}
	right := lipgloss.JoinVertical(lipgloss.Left,
		paneTitleStyle.Render("Provider 详情"),
		strings.Join(detail, "\n"),
		"",
		paneTitleStyle.Render("当前指向"),
		strings.Join(pointerLines, "\n"),
	)
	if t.warning != "" {
		right = lipgloss.JoinVertical(lipgloss.Left, right, "⚠ "+t.warning)
	}
	if t.notice != "" {
		right = lipgloss.JoinVertical(lipgloss.Left, right, t.notice)
	}

	half := t.width / 2
	if half < 20 {
		half = max(t.width/2, 1)
	}
	left := windowedPane("Providers", providerLines, t.providerIndex, t.height, half)
	rightPane := windowedPane("详情与指向", wrapPlain(right, half), 0, t.height, t.width-half)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 2), rightPane)
}

// pointerLine 渲染一行指针状态，语义与 senv ai status 一致。
func (t *aiTab) pointerLine(r llm.StatusRow) string {
	if !r.Supported {
		return fmt.Sprintf("%-12s 不支持", r.AgentID)
	}
	if r.Pointer == nil {
		return fmt.Sprintf("%-12s 未切换", r.AgentID)
	}
	when := ""
	if ts, err := r.Pointer.SwitchedAtTime(); err == nil {
		when = ts.Local().Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%-12s %s / %s（%s）", r.AgentID, r.Pointer.Provider, r.Pointer.Model, when)
}

// wrapPlain 把纯文本按显式换行拆为行切片（供 windowedPane 装配）。
func wrapPlain(s string, width int) []string {
	limit := width
	if limit < 20 {
		limit = 20
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = truncateRunes(line, limit)
	}
	return lines
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
