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
		rows, warning, err := sm.Status()
		if err != nil {
			return aiLoadedMsg{err: err}
		}
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

	if t.flow != aiFlowNone {
		return stackWithOverlay(t.height, t.renderFlow(), t.viewBaseAt)
	}
	return t.viewBaseAt(t.height)
}

func (t *aiTab) viewBaseAt(height int) string {
	if len(t.providers) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("AI"),
			emptyStateStyle.Render("暂无 LLM Provider 档案；执行 senv ai provider add 添加后按 r 刷新"))
	}

	// Three bordered columns + two 1-column gaps consume 8 columns outside
	// the Width passed to lipgloss.
	leftW := t.width / 4
	if leftW > 26 {
		leftW = 26
	}
	if leftW < 16 {
		leftW = 16
	}
	remaining := t.width - leftW - 8
	middleW := remaining / 2
	if middleW > 32 {
		middleW = 32
	}
	if middleW < 16 {
		middleW = 16
	}
	rightW := remaining - middleW
	if rightW < 16 {
		rightW = 16
	}

	// 左栏：provider 列表。
	providerLines := make([]string, 0, len(t.providers))
	currentPointers := make(map[string][]string)
	for _, r := range t.rows {
		if r.Pointer != nil {
			currentPointers[r.Pointer.Provider] = append(currentPointers[r.Pointer.Provider], r.AgentID)
		}
	}
	for i, p := range t.providers {
		def := p.DefaultModel
		if def == "" {
			def = "-"
		}
		line := fmt.Sprintf("%s · 默认 %s · %d 模型", p.Alias, def, len(p.Models))
		if agents := currentPointers[p.Alias]; len(agents) > 0 {
			line += " ●"
		}
		providerLines = append(providerLines, cursorLine(line, i == t.providerIndex))
	}

	// 中栏：每个 coding agent 的当前指向。
	agentLines := make([]string, 0, len(t.rows))
	for _, r := range t.rows {
		state := "未切换"
		if !r.Supported {
			state = "不支持"
		} else if r.Pointer != nil {
			state = fmt.Sprintf("%s / %s", r.Pointer.Provider, r.Pointer.Model)
		}
		agentLines = append(agentLines, cursorPrefix(false)+fmt.Sprintf("%-12s %s", r.AgentID, state))
	}

	// 右栏：选中 provider 详情。
	p := t.providers[t.providerIndex]
	detail := []string{
		fmt.Sprintf("alias: %s", p.Alias),
		fmt.Sprintf("base_url: %s", p.BaseURL),
		fmt.Sprintf("凭据引用: %s", p.CredentialRef),
		fmt.Sprintf("目录来源: %s", orDash(p.CatalogProvider)),
		fmt.Sprintf("模型: %s", strings.Join(p.Models, ", ")),
	}
	if t.warning != "" {
		detail = append(detail, "", "⚠ "+t.warning)
	}
	if t.notice != "" {
		detail = append(detail, "", t.notice)
	}

	left := windowedPane("Providers", providerLines, t.providerIndex, height, leftW)
	middle := windowedPane("当前指向", agentLines, 0, height, middleW)
	right := windowedPane("Provider 详情", detail, 0, height, rightW)

	left = paneStyle.Width(leftW).Height(height).Render(left)
	middle = paneStyle.Width(middleW).Height(height).Render(middle)
	right = paneStyle.Width(rightW).Height(height).Render(right)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		left, strings.Repeat(" ", 1), middle, strings.Repeat(" ", 1), right)
}

// renderFlow makes the otherwise hidden switch wizard visible. All three
// dimensions stay side by side so the user can always see the pending choice
// and each agent's current pointer.
func (t *aiTab) renderFlow() string {
	if t.flow == aiFlowConfirm {
		agents := llm.SupportedAgents()
		model := "-"
		if t.modelIndex >= 0 && t.modelIndex < len(t.currentModels()) {
			model = t.currentModels()[t.modelIndex]
		}
		agent := "agent"
		if t.agentIndex >= 0 && t.agentIndex < len(agents) {
			agent = agents[t.agentIndex].Name
		}
		provider := "provider"
		if t.providerIndex >= 0 && t.providerIndex < len(t.providers) {
			provider = t.providers[t.providerIndex].Alias
		}
		return modalBox("确认切换", fmt.Sprintf("%s → %s / %s", agent, provider, model), "enter/y confirm · esc/n cancel")
	}

	agentTitle := "Agents"
	providerTitle := "Providers"
	modelTitle := "Models"
	switch t.flow {
	case aiFlowSelectAgent:
		agentTitle = "▸ Agents"
	case aiFlowSelectModel:
		modelTitle = "▸ Models"
	}

	agents := llm.SupportedAgents()
	current := make(map[string]llm.StatusRow, len(t.rows))
	for _, r := range t.rows {
		current[r.AgentID] = r
	}
	agentLines := make([]string, 0, len(agents))
	for i, a := range agents {
		line := a.ID
		if r, ok := current[a.ID]; ok && r.Pointer != nil {
			line += fmt.Sprintf(" · %s / %s", r.Pointer.Provider, r.Pointer.Model)
		} else {
			line += " · 未切换"
		}
		agentLines = append(agentLines, cursorLine(line, i == t.agentIndex))
	}

	providerLines := make([]string, 0, len(t.providers))
	for i, p := range t.providers {
		providerLines = append(providerLines, cursorLine(p.Alias, i == t.providerIndex))
	}

	models := t.currentModels()
	defaultModel := ""
	if t.providerIndex >= 0 && t.providerIndex < len(t.providers) {
		defaultModel = t.providers[t.providerIndex].DefaultModel
	}
	modelLines := make([]string, 0, len(models))
	for i, m := range models {
		label := m
		if m == defaultModel {
			label += " · 默认"
		}
		modelLines = append(modelLines, cursorLine(label, i == t.modelIndex))
	}

	third := max(t.width/3, 16)
	agentsPane := windowedPane(agentTitle, agentLines, t.agentIndex, t.height/2, third)
	providersPane := windowedPane(providerTitle, providerLines, t.providerIndex, t.height/2, third)
	modelsPane := windowedPane(modelTitle, modelLines, t.modelIndex, t.height/2, third)
	body := lipgloss.JoinHorizontal(lipgloss.Top, agentsPane, "  ", providersPane, "  ", modelsPane)

	title := "选择 Agent"
	hint := "↑↓/jk move · enter next · esc cancel"
	if t.flow == aiFlowSelectModel {
		title = "选择模型"
	}
	return modalBox(title, body, hint)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
