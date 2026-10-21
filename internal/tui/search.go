package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/wii/senv/internal/search"
)

// Search result types (kept here so styles.go and search.go share them).
const (
	typeEnv    = "Env"
	typeText   = "Text"
	typeBackup = "Backup"
	typeConfig = "Cfg"
	typeSSH    = "SSH"
	typeLLM    = "LLM"
	typeMCP    = "MCP"
)

// searchTab is the global cross-type search overlay. It gathers all keys/names
// across env (all groups), text (all groups) and config, and matches ONLY keys
// and names — values are never searched and never shown.
type searchTab struct {
	mgr           Managers
	width, height int

	input    string
	gathered []searchResult // full inventory (key/name only)
	results  []searchResult // filtered by input
	index    int
}

// searchResult is a single hit. Preview is always non-sensitive metadata:
// env -> "***", text -> "<size>b", config -> target path. Secret content is
// never surfaced in the result list.
type searchResult struct {
	resultType string // typeEnv | typeText | typeConfig
	group      string // empty for config
	key        string // key or config name
	preview    string
	// extra holds additional *identifier* text that may be matched (e.g. an SSH
	// hostname). It must never carry values, key material or credentials.
	extra string
}

// matchable returns the identifier/description haystack. When extra is set it
// is the full match text from internal/search (already includes key); otherwise
// only the key is matched.
func (r searchResult) matchable() string {
	if r.extra != "" {
		return r.extra
	}
	return r.key
}

func newSearchTab(mgr Managers) *searchTab {
	return &searchTab{mgr: mgr}
}

func (s *searchTab) Title() string { return "Search" }

func (s *searchTab) Bindings() []KeyAction {
	return []KeyAction{
		actUp, actDown,
		{[]string{"enter"}, "jump", grpSearch, false},
		{[]string{"esc"}, "close", grpSearch, false},
	}
}

// InputMode is always true for the search overlay: it captures all keys.
func (s *searchTab) InputMode() bool { return true }

func (s *searchTab) Init() tea.Cmd { return s.gather() }

// Reload re-runs the local cross-type scan so results reflect data applied by
// a background sync while the overlay is open.
func (s *searchTab) Reload() tea.Cmd { return s.gather() }

func (s *searchTab) SetSize(w, h int) { s.width, s.height = w, h }

// --- messages ---

type searchGatheredMsg struct{ all []searchResult }

// searchJumpMsg asks the top-level model to switch tab and select the entry.
type searchJumpMsg struct {
	resultType string
	group      string
	key        string
}

// searchCloseMsg asks the top-level model to close the overlay.
type searchCloseMsg struct{}

// --- gathering ---

// gather collects metadata across all data types via internal/search so CLI,
// MCP and TUI share the same field table. Values are never stored on hits.
func (s *searchTab) gather() tea.Cmd {
	mgr := s.mgr
	return func() tea.Msg {
		items := search.Gather(search.Sources{
			Env:    mgr.Env,
			Text:   mgr.Text,
			Backup: mgr.Backup,
			Config: mgr.Config,
			SSH:    mgr.SSH,
			LLM:    mgr.LLM,
			MCP:    mgr.MCP,
		})
		all := make([]searchResult, 0, len(items))
		for _, it := range items {
			preview := it.Preview
			if it.Kind == search.KindConfig {
				preview = truncPath(it.Preview)
			}
			all = append(all, searchResult{
				resultType: kindToResultType(it.Kind),
				group:      it.Group,
				key:        it.Key,
				preview:    preview,
				extra:      it.MatchText(),
			})
		}
		sort.Slice(all, func(i, j int) bool {
			if all[i].resultType != all[j].resultType {
				return all[i].resultType < all[j].resultType
			}
			if all[i].group != all[j].group {
				return all[i].group < all[j].group
			}
			return all[i].key < all[j].key
		})
		return searchGatheredMsg{all: all}
	}
}

func kindToResultType(k search.Kind) string {
	switch k {
	case search.KindEnv:
		return typeEnv
	case search.KindText:
		return typeText
	case search.KindBackup:
		return typeBackup
	case search.KindConfig:
		return typeConfig
	case search.KindSSH:
		return typeSSH
	case search.KindLLM:
		return typeLLM
	case search.KindMCP:
		return typeMCP
	default:
		return string(k)
	}
}

// --- update ---

func (s *searchTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// overlay 打开期间终端尺寸变化：跟随重排，避免按旧尺寸渲染溢出。
		s.width, s.height = msg.Width, msg.Height
		return s, nil

	case searchGatheredMsg:
		s.gathered = msg.all
		s.refilter()
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return s, func() tea.Msg { return searchCloseMsg{} }
		case "enter":
			if len(s.results) > 0 {
				r := s.results[clamp(s.index, 0, len(s.results)-1)]
				rt, g, k := r.resultType, r.group, r.key
				return s, func() tea.Msg { return searchJumpMsg{rt, g, k} }
			}
		case "up", "k":
			s.index = clamp(s.index-1, 0, len(s.results)-1)
		case "down", "j":
			s.index = clamp(s.index+1, 0, len(s.results)-1)
		case "backspace":
			if len(s.input) > 0 {
				s.input = s.input[:len(s.input)-1]
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

// refilter recomputes results from the gathered inventory. Empty input shows
// the full inventory (human browse); non-empty input uses shared Matches
// (token AND, identifiers/descriptions only).
func (s *searchTab) refilter() {
	if strings.TrimSpace(s.input) == "" {
		s.results = s.gathered
	} else {
		out := make([]searchResult, 0, len(s.gathered))
		for _, r := range s.gathered {
			if search.Matches(r.matchable(), s.input) {
				out = append(out, r)
			}
		}
		s.results = out
	}
	s.index = clamp(s.index, 0, len(s.results)-1)
}

// --- view ---

func (s *searchTab) View() string {
	// s.width/s.height come from SetSize with the FULL terminal size; budget
	// constants (frame/overlay chrome) are package-level in keymap.go. Results
	// render inside what is left so a large inventory can never push the box
	// past the outer frame.
	innerH := s.height - frameRows - overlayRows
	if innerH < 1 {
		innerH = 1
	}
	// 外框边框还占 2 列（overlay 边框/内边距 6 列之外），漏算会在 frame 内折行。
	innerW := s.width - overlayCols - 2
	if innerW < 10 {
		innerW = 10
	}

	title := "Search"
	var lines []string
	if len(s.results) == 0 {
		// emptyStateStyle 自带 Padding(1,2)：文本先截断到 innerW-4 再套样式。
		lines = append(lines, emptyStateStyle.Render(
			truncateWidth("no matches"+emptyHint(s.input), innerW-4)))
	} else {
		// 1-line header + windowed results, cursor kept visible (same
		// primitives as windowedPane).
		page := listPageSize(innerH)
		start, end := visibleRange(len(s.results), s.index, page)
		if start > 0 || end < len(s.results) {
			title = fmt.Sprintf("%s  %d–%d / %d", title, start+1, end, len(s.results))
		}
		for i, r := range s.results[start:end] {
			// 先按显示宽截断纯文本，再套 badge/选中样式：truncateWidth 不会
			// 计算 ANSI 转义的显示宽。
			rest := truncateWidth(
				fmt.Sprintf("%s  %s", secondaryLabel(r.group, r.key), r.preview),
				innerW-2-len(r.resultType)-2)
			line := typeBadge(r.resultType) + "  " + rest
			lines = append(lines, cursorLine(line, start+i == s.index))
		}
	}
	// 头部（标题 + 输入回显）截断到 innerW，长输入不得撑破 overlay。输入
	// 段套 statusBarStyle（Padding(0,1) 多占 2 列），一并计入预算。
	title = truncateWidth(title, innerW-4)
	inputBudget := innerW - lipgloss.Width(title) - 2 - 2
	header := lipgloss.NewStyle().Bold(true).Render(title) +
		"  " + statusBarStyle.Render(truncateWidth(s.input+"_", maxInt(inputBudget, 4)))
	box := searchOverlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		append([]string{header}, lines...)...))
	// Last line of defence: the overlay must stay inside the frame budget.
	return clipLines(box, s.height-frameRows)
}

func emptyHint(input string) string {
	if input == "" {
		return ""
	}
	return "(input only appears in values?)"
}

// typeBadge renders a colored type label.
func typeBadge(t string) string {
	if style, ok := typeBadgeStyles[t]; ok {
		return style.Render(t)
	}
	return t
}

// secondaryLabel renders "group/key" or just "name".
func secondaryLabel(group, key string) string {
	if group == "" {
		return key
	}
	return group + "/" + key
}

// matchKey reports whether needle is contained in key (case-insensitive).
// This is the ONLY matching primitive used by search and tab filters, to
// guarantee values are never matched.
func matchKey(key, needle string) bool {
	return strings.Contains(strings.ToLower(key), strings.ToLower(needle))
}

// Compile-time guard.
var _ Tab = (*searchTab)(nil)
