package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Search result types (kept here so styles.go and search.go share them).
const (
	typeEnv    = "Env"
	typeText   = "Text"
	typeConfig = "Cfg"
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
}

func newSearchTab(mgr Managers) *searchTab {
	return &searchTab{mgr: mgr}
}

func (s *searchTab) Title() string { return "Search" }

func (s *searchTab) Help() string {
	return "type to search keys/names · ↑↓ select · enter jump · esc close"
}

// InputMode is always true for the search overlay: it captures all keys.
func (s *searchTab) InputMode() bool { return true }

func (s *searchTab) Init() tea.Cmd { return s.gather() }

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

// gather collects every key/name across all data types. Only keys/names are
// read; values are intentionally never inspected or stored.
func (s *searchTab) gather() tea.Cmd {
	mgr := s.mgr
	return func() tea.Msg {
		var all []searchResult
		// Env: iterate groups, collect keys.
		if mgr.Env != nil {
			if gis, err := mgr.Env.ListGroups(); err == nil {
				for _, g := range gis {
					vars, err := mgr.Env.List(g.Name)
					if err != nil {
						continue
					}
					for k := range vars[g.Name] {
						all = append(all, searchResult{
							resultType: typeEnv, group: g.Name, key: k, preview: "***",
						})
					}
				}
			}
		}
		// Text: iterate groups, collect keys (preview = size, not content).
		if mgr.Text != nil {
			if gs, err := mgr.Text.ListGroups(); err == nil {
				for _, g := range gs {
					infos, err := mgr.Text.List(g.Name)
					if err != nil {
						continue
					}
					for _, ti := range infos {
						all = append(all, searchResult{
							resultType: typeText, group: g.Name, key: ti.Key,
							preview: fmt.Sprintf("%db", ti.Size),
						})
					}
				}
			}
		}
		// Config: flat list of names (preview = target path, not content).
		if mgr.Config != nil {
			if cfgs, err := mgr.Config.List(); err == nil {
				for _, c := range cfgs {
					all = append(all, searchResult{
						resultType: typeConfig, key: c.Name, preview: truncPath(c.TargetPath),
					})
				}
			}
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

// --- update ---

func (s *searchTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
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
			s.index = clamp(s.index-1, 0, maxLen(s.results)-1)
		case "down", "j":
			s.index = clamp(s.index+1, 0, maxLen(s.results)-1)
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

// refilter recomputes results from the gathered inventory using matchKey, which
// only matches against the key/name field — values are structurally excluded.
func (s *searchTab) refilter() {
	if s.input == "" {
		s.results = s.gathered
	} else {
		out := make([]searchResult, 0, len(s.gathered))
		for _, r := range s.gathered {
			if matchKey(r.key, s.input) {
				out = append(out, r)
			}
		}
		s.results = out
	}
	s.index = clamp(s.index, 0, maxLen(s.results)-1)
}

// --- view ---

func (s *searchTab) View() string {
	header := lipgloss.NewStyle().Bold(true).Render("Search") +
		"  " + statusBarStyle.Render(s.input+"_")

	parts := []string{header}
	if len(s.results) == 0 {
		parts = append(parts, emptyStateStyle.Render(
			"no matches"+emptyHint(s.input)))
	} else {
		for i, r := range s.results {
			badge := typeBadge(r.resultType)
			line := fmt.Sprintf("%s  %s  %s", badge, secondaryLabel(r.group, r.key), r.preview)
			if i == s.index {
				line = selectedLineStyle.Render("▸ ") + line
			} else {
				line = "  " + line
			}
			parts = append(parts, line)
		}
	}
	box := searchOverlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return box
}

func emptyHint(input string) string {
	if input == "" {
		return ""
	}
	return " (input only appears in values?)"
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
