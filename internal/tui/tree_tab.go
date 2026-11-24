package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/tree"
)

// treeTab 浏览加密路径树（deploy / wireguard / seoul-egress）：
// 左栏为可折叠的分组/目录树，右栏为当前选中目录下的文件列表。
// `enter` 打开只读内容预览，`e` 用 $EDITOR 编辑单文件并写回。
type treeTab struct {
	mgr           Managers
	width, height int
	loaded        bool

	groups    []string
	metas     []tree.Meta     // 跨组全部文件元数据
	expanded  map[string]bool // key = "<group>" or "<group>:<dirPrefix>"
	navIndex  int
	focusLeft bool
	itemIndex int

	filterBox Filter
	detail    *detailOverlay
}

type treeLoadedMsg struct {
	groups []string
	metas  []tree.Meta
	err    error
}

type treeReloadMsg struct{ toast string }

type treeDetailMsg struct {
	group string
	path  string
	entry *tree.Entry
	err   error
}

type treeEditDoneMsg struct {
	group string
	path  string
	err   error
}

type treeNavNode struct {
	group  string
	prefix string // "" = group root
	label  string
	depth  int
	count  int
	hasSub bool
}

func newTreeTab(mgr Managers) *treeTab {
	return &treeTab{
		mgr:       mgr,
		focusLeft: true,
		expanded:  map[string]bool{},
	}
}

func (t *treeTab) Title() string { return "Tree" }

func (t *treeTab) Bindings() []KeyAction {
	if t.detail != nil {
		return detailBindings()
	}
	if t.filterBox.Active() {
		return filterBindings(true)
	}
	if t.focusLeft {
		return append(navBindings(true),
			KeyAction{[]string{"space"}, "expand/collapse", grpItem, false},
			actFilter, actRefresh,
		)
	}
	return append(navBindings(true),
		actDetail,
		KeyAction{[]string{"e"}, "edit file", grpItem, false},
		actEsc, actFilter, actRefresh,
	)
}

func (t *treeTab) InputMode() bool {
	return t.detail != nil || t.filterBox.Active()
}

func (t *treeTab) SetSize(width, height int) {
	t.width, t.height = width, height
	if t.detail != nil {
		t.detail.SetSize(width, height)
	}
}

func (t *treeTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *treeTab) Reload() tea.Cmd { return t.load() }

func (t *treeTab) load() tea.Cmd {
	mgr := t.mgr.Tree
	return func() tea.Msg {
		st := perflog.Start("tui.load-tree")
		if mgr == nil {
			st.End(false)
			return treeLoadedMsg{err: fmt.Errorf("tree manager unavailable")}
		}
		groups, _, err := mgr.ListGroups()
		if err != nil {
			st.End(false)
			return treeLoadedMsg{err: err}
		}
		var all []tree.Meta
		for _, g := range groups {
			metas, err := mgr.List(g, "")
			if err != nil {
				st.End(false)
				return treeLoadedMsg{err: err}
			}
			all = append(all, metas...)
		}
		st.With("files", len(all)).End(true)
		return treeLoadedMsg{groups: groups, metas: all}
	}
}

func expandKey(group, prefix string) string {
	if prefix == "" {
		return group
	}
	return group + ":" + prefix
}

func (t *treeTab) navNodes() []treeNavNode {
	// Build per-group directory counts and immediate subdirs.
	byGroup := map[string][]tree.Meta{}
	for _, mt := range t.metas {
		byGroup[mt.Group] = append(byGroup[mt.Group], mt)
	}
	var nodes []treeNavNode
	for _, g := range t.groups {
		gMetas := byGroup[g]
		// Collect directory prefixes and counts under this group.
		dirCounts := map[string]int{}
		children := map[string]map[string]bool{}
		for _, mt := range gMetas {
			parts := strings.Split(mt.Path, "/")
			for i := 1; i < len(parts); i++ {
				parent := strings.Join(parts[:i-1], "/")
				cur := strings.Join(parts[:i], "/")
				dirCounts[cur]++
				if children[parent] == nil {
					children[parent] = map[string]bool{}
				}
				children[parent][cur] = true
			}
		}
		nodes = append(nodes, treeNavNode{
			group:  g,
			prefix: "",
			label:  g,
			depth:  0,
			count:  len(gMetas),
			hasSub: len(children[""]) > 0,
		})
		var addChildren func(parent string, depth int)
		addChildren = func(parent string, depth int) {
			if !t.expanded[expandKey(g, parent)] {
				return
			}
			subs := make([]string, 0, len(children[parent]))
			for s := range children[parent] {
				subs = append(subs, s)
			}
			sort.Strings(subs)
			for _, sub := range subs {
				base := sub
				if idx := strings.LastIndex(sub, "/"); idx >= 0 {
					base = sub[idx+1:]
				}
				nodes = append(nodes, treeNavNode{
					group:  g,
					prefix: sub,
					label:  base + "/",
					depth:  depth,
					count:  dirCounts[sub],
					hasSub: len(children[sub]) > 0,
				})
				addChildren(sub, depth+1)
			}
		}
		addChildren("", 1)
	}
	return nodes
}

func (t *treeTab) currentNode() (treeNavNode, bool) {
	nodes := t.navNodes()
	if t.navIndex < 0 || t.navIndex >= len(nodes) {
		return treeNavNode{}, false
	}
	return nodes[t.navIndex], true
}

func (t *treeTab) baseMetas() []tree.Meta {
	nd, ok := t.currentNode()
	if !ok {
		return nil
	}
	var out []tree.Meta
	for _, mt := range t.metas {
		if mt.Group != nd.group {
			continue
		}
		if nd.prefix != "" && !strings.HasPrefix(mt.Path, nd.prefix+"/") {
			continue
		}
		out = append(out, mt)
	}
	return out
}

func (t *treeTab) filteredMetas() []tree.Meta {
	base := t.baseMetas()
	if t.filterBox.Term() == "" {
		return base
	}
	out := make([]tree.Meta, 0, len(base))
	for _, mt := range base {
		if t.filterBox.Matches(mt.Path) || t.filterBox.Matches(mt.Description) {
			out = append(out, mt)
		}
	}
	return out
}

func (t *treeTab) currentMeta() (tree.Meta, bool) {
	items := t.filteredMetas()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return tree.Meta{}, false
	}
	return items[t.itemIndex], true
}

func (t *treeTab) focusListLen() int {
	if t.focusLeft {
		return len(t.navNodes())
	}
	return len(t.filteredMetas())
}

func (t *treeTab) cursorForFocus() int {
	if t.focusLeft {
		return t.navIndex
	}
	return t.itemIndex
}

func (t *treeTab) moveCursor(delta int) {
	if t.focusLeft {
		n := len(t.navNodes())
		t.navIndex = clamp(t.navIndex+delta, 0, maxInt(n-1, 0))
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(t.itemIndex+delta, 0, maxInt(t.focusListLen()-1, 0))
}

func (t *treeTab) jumpCursor(idx int) {
	if t.focusLeft {
		n := len(t.navNodes())
		t.navIndex = clamp(idx, 0, maxInt(n-1, 0))
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(idx, 0, maxInt(t.focusListLen()-1, 0))
}

func (t *treeTab) clampCursors() {
	n := len(t.navNodes())
	t.navIndex = clamp(t.navIndex, 0, maxInt(n-1, 0))
	t.itemIndex = clamp(t.itemIndex, 0, maxInt(len(t.filteredMetas())-1, 0))
}

func (t *treeTab) toggleExpand() {
	if !t.focusLeft {
		return
	}
	nd, ok := t.currentNode()
	if !ok || !nd.hasSub {
		return
	}
	k := expandKey(nd.group, nd.prefix)
	t.expanded[k] = !t.expanded[k]
	t.clampCursors()
}

func (t *treeTab) focusJump(group, key string) {
	t.filterBox.Clear()
	t.detail = nil
	t.focusLeft = false
	t.itemIndex = 0
	for i, nd := range t.navNodes() {
		if nd.group == group && nd.prefix == "" {
			t.navIndex = i
			break
		}
	}
	for i, mt := range t.filteredMetas() {
		if mt.Path == key {
			t.itemIndex = i
			return
		}
	}
}

func (t *treeTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case treeLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.groups = msg.groups
		t.metas = msg.metas
		t.loaded = true
		t.clampCursors()
		return t, nil

	case treeReloadMsg:
		cmd := t.load()
		if msg.toast != "" {
			return t, tea.Batch(okToast(msg.toast), cmd)
		}
		return t, cmd

	case treeDetailMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.detail = newDetailOverlay(msg.group+":"+msg.path, treeDetailLines(msg.entry))
		t.detail.SetSize(t.width, t.height)
		return t, nil

	case treeEditDoneMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		return t, func() tea.Msg { return treeReloadMsg{toast: "saved " + msg.group + ":" + msg.path} }

	case detailCloseMsg:
		t.detail = nil
		return t, nil

	case tea.KeyMsg:
		if t.detail != nil {
			var cmd tea.Cmd
			t.detail, cmd = t.detail.Update(msg)
			return t, cmd
		}
		if t.filterBox.Active() {
			return t.updateFilter(msg)
		}
		return t.updateKey(msg)
	}
	return t, nil
}

func (t *treeTab) updateFilter(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "esc":
		t.filterBox.Clear()
		t.clampCursors()
	case "enter":
		t.filterBox.Confirm()
		t.clampCursors()
	case "backspace":
		if t.filterBox.Backspace() {
			t.clampCursors()
		}
	default:
		if r := msg.Runes; len(r) == 1 {
			t.filterBox.Append(string(r))
			t.clampCursors()
		}
	}
	return t, nil
}

func (t *treeTab) updateKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		t.moveCursor(-1)
	case "down", "j":
		t.moveCursor(1)
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "g":
		t.jumpCursor(0)
	case "G":
		t.jumpCursor(t.focusListLen() - 1)
	case "pgup":
		t.jumpCursor(t.cursorForFocus() - pageStep(t.height))
	case "pgdown":
		t.jumpCursor(t.cursorForFocus() + pageStep(t.height))
	case " ":
		if t.focusLeft {
			t.toggleExpand()
		}
	case "enter":
		if !t.focusLeft {
			return t.openDetail()
		}
	case "e":
		if !t.focusLeft {
			return t.editSelected()
		}
	case "/":
		t.filterBox.EnterFresh()
	case "esc":
		if t.filterBox.Term() != "" {
			t.filterBox.Clear()
			t.clampCursors()
		} else if !t.focusLeft {
			t.focusLeft = true
		}
	case "ctrl+r":
		return t, t.load()
	}
	return t, nil
}

func (t *treeTab) openDetail() (Tab, tea.Cmd) {
	mt, ok := t.currentMeta()
	if !ok {
		return t, nil
	}
	mgr := t.mgr.Tree
	return t, func() tea.Msg {
		e, err := mgr.Get(mt.Group, mt.Path)
		return treeDetailMsg{group: mt.Group, path: mt.Path, entry: e, err: err}
	}
}

func (t *treeTab) editSelected() (Tab, tea.Cmd) {
	mt, ok := t.currentMeta()
	if !ok {
		return t, nil
	}
	entry, err := t.mgr.Tree.Get(mt.Group, mt.Path)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	if entry.Encoding == "base64" {
		return t, func() tea.Msg { return errMsg{err: fmt.Errorf("binary file %s cannot be edited in $EDITOR", mt.Path)} }
	}
	raw, err := entry.DecodeBytes()
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	dir, err := os.MkdirTemp("", "senv-tui-tree-")
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	tmp := filepath.Join(dir, filepath.Base(mt.Path))
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], tmp)...)
	return t, tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.RemoveAll(dir)
		if err != nil {
			return treeEditDoneMsg{err: err}
		}
		after, rerr := os.ReadFile(tmp)
		if rerr != nil {
			return treeEditDoneMsg{err: rerr}
		}
		if string(after) == string(raw) {
			return treeReloadMsg{}
		}
		if _, serr := t.mgr.Tree.Set(mt.Group, mt.Path, after, entry.Mode, entry.Description, true); serr != nil {
			return treeEditDoneMsg{err: serr}
		}
		recordAudit(t.mgr, session.AuditOpTree, "tree:"+mt.Group+":"+mt.Path, true, "tui-edit")
		return treeEditDoneMsg{group: mt.Group, path: mt.Path}
	})
}

func (t *treeTab) View() string {
	if t.detail != nil {
		return t.detail.View()
	}
	width, height := t.width, t.height
	leftW := width / 4
	if leftW > 30 {
		leftW = 30
	}
	if leftW < 18 {
		leftW = 18
	}
	rightW := width - leftW - 5
	if rightW < 4 {
		rightW = 4
	}
	if !t.loaded {
		left := emptyStateStyle.Render("loading trees…")
		right := emptyStateStyle.Render("loading trees…")
		if t.focusLeft {
			left = applyPane(left, leftW, height, true)
			right = applyPane(right, rightW, height, false)
		} else {
			left = applyPane(left, leftW, height, false)
			right = applyPane(right, rightW, height, true)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}
	left := t.renderTreeSidebar(leftW, height)
	right := t.renderFiles(rightW, height)
	if t.focusLeft {
		left = applyPane(left, leftW, height, true)
		right = applyPane(right, rightW, height, false)
	} else {
		left = applyPane(left, leftW, height, false)
		right = applyPane(right, rightW, height, true)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (t *treeTab) renderTreeSidebar(width, height int) string {
	nodes := t.navNodes()
	if len(nodes) == 0 {
		return emptyStateStyle.Render("no tree groups")
	}
	rows := make([]SidebarRow, 0, len(nodes))
	for i, nd := range nodes {
		marker := " "
		if nd.hasSub {
			if t.expanded[expandKey(nd.group, nd.prefix)] {
				marker = "▾"
			} else {
				marker = "▸"
			}
		}
		name := strings.Repeat("  ", nd.depth) + nd.label
		rows = append(rows, SidebarRow{
			Marker:   marker,
			Name:     name,
			Count:    nd.count,
			Selected: i == t.navIndex && t.focusLeft,
		})
	}
	return renderSidebar(rows, t.navIndex, height, width)
}

func (t *treeTab) renderFiles(width, height int) string {
	items := t.filteredMetas()
	header := "Tree"
	if nd, ok := t.currentNode(); ok {
		if nd.prefix == "" {
			header = fmt.Sprintf("%s (%d)", nd.group, len(items))
		} else {
			header = fmt.Sprintf("%s:%s (%d)", nd.group, nd.prefix, len(items))
		}
	}
	if t.filterBox.Term() != "" {
		header += "  /" + t.filterBox.Term()
	}
	if len(items) == 0 {
		hint := "no files"
		if t.filterBox.Term() != "" {
			hint = "no files match /" + t.filterBox.Term()
		}
		return lipgloss.JoinVertical(lipgloss.Left, paneTitleStyle.Render(header), emptyStateStyle.Render(hint))
	}
	inner := width - 2
	modeW, sizeW := 6, 8
	pathW := inner - 2 - modeW - sizeW - 2
	if pathW < 12 {
		pathW = 12
	}
	lines := make([]string, 0, len(items))
	for i, mt := range items {
		row := fmt.Sprintf("%s %s %s",
			padRight(fmt.Sprintf("%04o", mt.Mode), modeW),
			padRight(fmt.Sprintf("%dB", mt.Size), sizeW),
			truncateWidth(mt.Path, pathW))
		if i == t.itemIndex {
			lines = append(lines, selectedLineStyle.Render(cursorPrefix(true)+truncateWidth(row, inner-2)))
		} else {
			lines = append(lines, unselectedLineStyle.Render(cursorPrefix(false)+truncateWidth(row, inner-2)))
		}
	}
	return windowedPane(header, lines, t.itemIndex, height, width)
}

func treeDetailLines(e *tree.Entry) []string {
	if e == nil {
		return nil
	}
	lines := []string{
		fmt.Sprintf("group: %s", e.Group),
		fmt.Sprintf("path: %s", e.Path),
		fmt.Sprintf("mode: %04o", e.Mode),
		fmt.Sprintf("sha256: %s", e.SHA256),
		fmt.Sprintf("updated: %s", e.UpdatedAt.Local().Format("2006-01-02 15:04")),
	}
	if e.Description != "" {
		lines = append(lines, "description: "+e.Description)
	}
	lines = append(lines, "")
	if e.Encoding == "base64" {
		lines = append(lines, fmt.Sprintf("<binary base64 (%d chars)>", len(e.Content)))
		return lines
	}
	raw, err := e.DecodeBytes()
	if err != nil {
		return append(lines, "decode error: "+err.Error())
	}
	for _, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		lines = append(lines, l)
	}
	return lines
}

var _ Tab = (*treeTab)(nil)
