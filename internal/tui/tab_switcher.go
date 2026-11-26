package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tabSwitchMsg struct{ index int }
type tabSwitchCloseMsg struct{}

type tabSwitcherItem struct {
	index  int // -1 for Home/Welcome
	title  string
	desc   string
	hotkey string
	active bool
}

// tabSwitcher 是居中浮层 Tab 切换器（ctrl+p / ctrl+k 呼出），列出 Home 与全部已注册
// Tab，支持按名称/描述实时模糊过滤并一键跳转。
type tabSwitcher struct {
	width, height int
	frameOverhead int
	items         []tabSwitcherItem
	filtered      []tabSwitcherItem
	input         string
	cursor        int
}

func tabDescription(title string) string {
	switch title {
	case "Home":
		return "Welcome page & shortcuts"
	case "Env":
		return "Environment variables"
	case "Text":
		return "Encrypted text & secrets"
	case "Providers":
		return "LLM providers & agent routing"
	case "Skill":
		return "Agent skills vault & apply"
	case "SSH":
		return "SSH hosts & config export"
	case "KeyPair":
		return "SSH key pairs"
	case "MCP":
		return "MCP server profiles & export"
	case "Ledger":
		return "Structured entity ledger"
	case "Tree":
		return "Encrypted path tree files"
	case "Config":
		return "Managed config files"
	case "History":
		return "Version history & restore"
	case "Audit":
		return "Local operation audit log"
	case "Backup":
		return "Encrypted backup blocks"
	case "Settings":
		return "Theme, icons & sync status"
	default:
		return ""
	}
}

func newTabSwitcher(tabs []Tab, active int, hasHome, onWelcome bool) *tabSwitcher {
	items := make([]tabSwitcherItem, 0, len(tabs)+1)
	if hasHome {
		items = append(items, tabSwitcherItem{
			index:  -1,
			title:  "Home",
			desc:   tabDescription("Home"),
			hotkey: "0",
			active: onWelcome,
		})
	}
	for i, t := range tabs {
		title := t.Title()
		hotkey := ""
		if i < 9 {
			hotkey = strconv.Itoa(i + 1)
		}
		if title == "Settings" {
			hotkey = ","
		}
		items = append(items, tabSwitcherItem{
			index:  i,
			title:  title,
			desc:   tabDescription(title),
			hotkey: hotkey,
			active: !onWelcome && i == active,
		})
	}
	s := &tabSwitcher{items: items}
	s.refilter()
	return s
}

func (s *tabSwitcher) Title() string { return "Tabs" }

func (s *tabSwitcher) Bindings() []KeyAction {
	return []KeyAction{
		actUp, actDown,
		{[]string{"tab/shift+tab"}, "next/prev", grpNav, false},
		{[]string{"enter"}, "jump", grpSearch, false},
		{[]string{"esc"}, "close", grpSearch, false},
	}
}

func (s *tabSwitcher) InputMode() bool  { return true }
func (s *tabSwitcher) Init() tea.Cmd    { return nil }
func (s *tabSwitcher) Reload() tea.Cmd  { return nil }
func (s *tabSwitcher) SetSize(w, h int) { s.width, s.height = w, h }

func (s *tabSwitcher) overhead() int {
	if s.frameOverhead > 0 {
		return s.frameOverhead
	}
	return frameRows
}

func (s *tabSwitcher) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		return s, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return s, func() tea.Msg { return tabSwitchCloseMsg{} }
		case "enter":
			if len(s.filtered) > 0 {
				it := s.filtered[clamp(s.cursor, 0, len(s.filtered)-1)]
				return s, func() tea.Msg { return tabSwitchMsg{index: it.index} }
			}
		case "up", "shift+tab":
			if len(s.filtered) > 0 {
				s.cursor = clamp(s.cursor-1, 0, len(s.filtered)-1)
			}
		case "down", "tab":
			if len(s.filtered) > 0 {
				s.cursor = clamp(s.cursor+1, 0, len(s.filtered)-1)
			}
		case "backspace":
			if rs := []rune(s.input); len(rs) > 0 {
				s.input = string(rs[:len(rs)-1])
				s.refilter()
			}
		default:
			if isPrintable(msg) {
				s.input += msg.String()
				s.refilter()
			}
		}
	}
	return s, nil
}

func (s *tabSwitcher) refilter() {
	q := strings.ToLower(strings.TrimSpace(s.input))
	if q == "" {
		s.filtered = append(s.filtered[:0], s.items...)
	} else {
		out := make([]tabSwitcherItem, 0, len(s.items))
		for _, it := range s.items {
			haystack := strings.ToLower(it.title + " " + it.desc)
			if strings.Contains(haystack, q) {
				out = append(out, it)
			}
		}
		s.filtered = out
	}
	if len(s.filtered) == 0 {
		s.cursor = 0
	} else {
		s.cursor = clamp(s.cursor, 0, len(s.filtered)-1)
	}
}

func (s *tabSwitcher) View() string {
	oh := s.overhead()
	boxW := minInt(maxInt(s.width-overlayCols-2-helpMarginX, 24), 66)
	maxRows := maxInt(s.height-oh-helpMarginY-overlayRows, 3)

	title := "Switch Tab"
	inputBudget := maxInt(boxW-lipgloss.Width(title)-4, 6)
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Background(colorSurface).Render(title) +
		lipgloss.NewStyle().Background(colorSurface).Render("  ") +
		statusBarStyle.Render(truncateWidth(s.input+"_", inputBudget))

	if len(s.filtered) == 0 {
		empty := emptyStateStyle.Render(truncateWidth("no matching tabs", maxInt(boxW-4, 8)))
		box := searchOverlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, empty))
		return clipLines(box, maxInt(s.height-oh-helpMarginY, 1))
	}

	listBudget := maxInt(maxRows-1, 1)
	start, end := visibleRange(len(s.filtered), s.cursor, listBudget)

	body := make([]string, 0, 1+(end-start))
	body = append(body, header)
	for i := start; i < end; i++ {
		body = append(body, s.renderItemRow(s.filtered[i], i == s.cursor, boxW))
	}
	box := searchOverlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left, body...))
	return clipLines(box, maxInt(s.height-oh-helpMarginY, 1))
}

func (s *tabSwitcher) renderItemRow(it tabSwitcherItem, selected bool, boxW int) string {
	icon := tabIcon(it.title)
	curMarker := " "
	if it.active {
		curMarker = "*"
	}
	hotkey := ""
	if it.hotkey != "" {
		hotkey = "[" + it.hotkey + "]"
	}

	// Reserve columns: cursor(2) + icon(1) + space(1) + title(10) + marker(2) + hotkey
	hotkeyW := lipgloss.Width(hotkey)
	descBudget := boxW - 2 - 1 - 1 - 10 - 2 - hotkeyW - 1
	desc := ""
	if descBudget >= 6 && it.desc != "" {
		desc = truncateWidth(it.desc, descBudget)
	}

	if selected {
		left := fmt.Sprintf("%s %s %-10s %s", icon, curMarker, truncateWidth(it.title, 10), desc)
		if hotkey != "" {
			pad := maxInt(boxW-2-lipgloss.Width(left)-hotkeyW, 1)
			left += strings.Repeat(" ", pad) + hotkey
		}
		return cursorLineW(left, true, boxW)
	}

	iconPart := lipgloss.NewStyle().Foreground(tabIconColor(it.title)).Background(colorSurface).Render(icon)
	markerPart := lipgloss.NewStyle().Foreground(colorOk).Background(colorSurface).Render(curMarker)
	titlePart := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Render(fmt.Sprintf("%-10s", truncateWidth(it.title, 10)))
	descPart := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface).Render(desc)
	rightPart := ""
	if hotkey != "" {
		rightPart = lipgloss.NewStyle().Foreground(colorAccent).Background(colorSurface).Render(hotkey)
	}

	left := cursorPrefix(false) + iconPart + " " + markerPart + " " + titlePart
	if desc != "" {
		left += " " + descPart
	}
	usedW := lipgloss.Width(left) + hotkeyW
	if hotkey != "" {
		pad := maxInt(boxW-usedW, 1)
		left += strings.Repeat(" ", pad) + rightPart
	} else if usedW < boxW {
		left += strings.Repeat(" ", boxW-usedW)
	}
	return left
}

var _ Tab = (*tabSwitcher)(nil)
