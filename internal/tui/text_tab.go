package tui

import (
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/text"
)

// textTab renders Text data: left column = groups, right column = text block
// metadata (key/size/updated). Content is never shown in the list; it is only
// visible via vim editing or detail view.
type textTab struct {
	mgr           Managers
	width, height int

	groups       []textGroupRow
	itemsByGroup map[string][]textItemRow
	groupIndex   int
	loaded       bool

	itemIndex int
	focusLeft bool

	deref     bool
	filter    string
	filtering bool

	input textinput.Model
	mode  textMode
	flash string
}

type textGroupRow struct {
	name     string
	keyCount int
}

type textItemRow struct {
	key       string
	size      int
	updatedAt string
}

type textMode int

const (
	textModeNormal textMode = iota
	textModeDeleteConfirm
	textModeExportPath
	textModeNewKey
	textModeAddGroup
	textModeFilter
)

func newTextTab(mgr Managers) *textTab {
	ti := textinput.New()
	ti.CharLimit = 0
	return &textTab{mgr: mgr, focusLeft: true, input: ti}
}

func (t *textTab) Title() string { return "Text" }

func (t *textTab) Help() string {
	return "↑↓/jk move · ←→/hl panes · e vim edit · n new · d del · y yank · o export · + group · D deref · / filter"
}

func (t *textTab) InputMode() bool {
	switch t.mode {
	case textModeFilter, textModeExportPath, textModeNewKey, textModeAddGroup:
		return true
	}
	return false
}

// --- data loading ---

type textLoadedMsg struct {
	groups       []textGroupRow
	itemsByGroup map[string][]textItemRow
	err          error
}

type textReloadMsg struct{}

func (t *textTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *textTab) load() tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		if mgr == nil {
			return textLoadedMsg{err: fmt.Errorf("text manager unavailable")}
		}
		gs, err := mgr.ListGroups()
		if err != nil {
			return textLoadedMsg{err: err}
		}
		groups := make([]textGroupRow, 0, len(gs))
		itemsByGroup := make(map[string][]textItemRow, len(gs))
		for _, g := range gs {
			// Hide groups that have no keys, except "default" which is always
			// shown as a stable landing point.
			if g.KeyCount == 0 && g.Name != "default" {
				continue
			}
			groups = append(groups, textGroupRow{name: g.Name, keyCount: g.KeyCount})
			infos, err := mgr.List(g.Name)
			if err != nil {
				itemsByGroup[g.Name] = nil
				continue
			}
			itemsByGroup[g.Name] = buildTextItems(infos)
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if groups[i].name == "default" {
				return true
			}
			if groups[j].name == "default" {
				return false
			}
			return groups[i].name < groups[j].name
		})
		return textLoadedMsg{groups: groups, itemsByGroup: itemsByGroup}
	}
}

func buildTextItems(infos []text.TextInfo) []textItemRow {
	out := make([]textItemRow, 0, len(infos))
	for _, ti := range infos {
		out = append(out, textItemRow{
			key:       ti.Key,
			size:      ti.Size,
			updatedAt: ti.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func (t *textTab) currentGroup() string {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return ""
	}
	return t.groups[t.groupIndex].name
}

func (t *textTab) filteredItems() []textItemRow {
	all := t.itemsByGroup[t.currentGroup()]
	if t.filter == "" {
		return all
	}
	out := make([]textItemRow, 0, len(all))
	for _, it := range all {
		if matchKey(it.key, t.filter) {
			out = append(out, it)
		}
	}
	return out
}

func (t *textTab) currentItem() (textItemRow, bool) {
	items := t.filteredItems()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return textItemRow{}, false
	}
	return items[t.itemIndex], true
}

// --- update ---

func (t *textTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case textLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.groups = msg.groups
		t.itemsByGroup = msg.itemsByGroup
		t.loaded = true
		t.clampCursors()
		return t, nil

	case textReloadMsg:
		return t, t.load()

	case tea.KeyMsg:
		t.flash = ""

		if t.mode != textModeNormal {
			return t.handleModalKey(msg)
		}

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
			t.jumpCursor(len(t.listForFocus()) - 1)
		case "e":
			return t.editCurrent()
		case "n":
			return t.enterNewKeyMode()
		case "d":
			return t.enterDeleteConfirm()
		case "y":
			return t, t.doCopy()
		case "o":
			return t.enterExportMode()
		case "+":
			return t.enterAddGroupMode()
		case "D":
			// The text list shows metadata only (no content), so dereference has
			// no visual effect on the list; it would apply to a detail/export view.
			t.deref = !t.deref
			t.flash = "dereference: " + onOff(t.deref) + " (metadata only in list)"
		case "/":
			return t.enterFilterMode()
		}
	}
	return t, nil
}

// --- navigation ---

func (t *textTab) listForFocus() []string {
	if t.focusLeft {
		out := make([]string, len(t.groups))
		for i, g := range t.groups {
			out[i] = g.name
		}
		return out
	}
	items := t.filteredItems()
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.key
	}
	return out
}

func (t *textTab) moveCursor(delta int) {
	if t.focusLeft {
		t.groupIndex = clamp(t.groupIndex+delta, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(t.itemIndex+delta, 0, maxLen(items)-1)
	}
}

func (t *textTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(idx, 0, maxLen(items)-1)
	}
}

func (t *textTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, maxLen(t.groups)-1)
	t.itemIndex = clamp(t.itemIndex, 0, maxLen(t.filteredItems())-1)
}

// focusJump positions the cursor at (group, key) for search-result navigation.
func (t *textTab) focusJump(group, key string) {
	t.filter = ""
	t.mode = textModeNormal
	for i, g := range t.groups {
		if g.name == group {
			t.groupIndex = i
			break
		}
	}
	items := t.itemsByGroup[t.currentGroup()]
	for i, it := range items {
		if it.key == key {
			t.itemIndex = i
			break
		}
	}
	t.focusLeft = false
}

// --- modal handling ---

func (t *textTab) handleModalKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	if t.mode == textModeDeleteConfirm {
		switch msg.String() {
		case "enter", "y":
			it, ok := t.currentItem()
			t.mode = textModeNormal
			if !ok {
				return t, nil
			}
			return t, t.doDelete(t.currentGroup(), it.key)
		default:
			t.mode = textModeNormal
			return t, nil
		}
	}

	switch msg.String() {
	case "esc":
		if t.mode == textModeFilter {
			t.filter = ""
		}
		t.mode = textModeNormal
		t.input.Blur()
		return t, nil
	case "enter":
		return t.submitModal()
	}

	if t.mode == textModeFilter {
		switch msg.String() {
		case "backspace":
			if len(t.filter) > 0 {
				t.filter = t.filter[:len(t.filter)-1]
			}
			t.itemIndex = 0
			return t, nil
		}
		if isPrintable(msg) {
			t.filter += msg.String()
			t.itemIndex = 0
		}
		return t, nil
	}

	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return t, cmd
}

func (t *textTab) submitModal() (Tab, tea.Cmd) {
	switch t.mode {
	case textModeExportPath:
		path := t.input.Value()
		it, ok := t.currentItem()
		t.mode = textModeNormal
		t.input.Blur()
		if !ok || path == "" {
			t.flash = "export cancelled"
			return t, nil
		}
		return t, t.doExport(t.currentGroup(), it.key, path)
	case textModeNewKey:
		key := t.input.Value()
		t.mode = textModeNormal
		t.input.Blur()
		if key == "" {
			t.flash = "key cannot be empty"
			return t, nil
		}
		return t.editKey(t.currentGroup(), key)
	case textModeAddGroup:
		name := t.input.Value()
		t.mode = textModeNormal
		t.input.Blur()
		if name == "" {
			t.flash = "group name cannot be empty"
			return t, nil
		}
		return t, t.doAddGroup(name)
	}
	t.mode = textModeNormal
	return t, nil
}

// --- modal entry points ---

func (t *textTab) enterNewKeyMode() (Tab, tea.Cmd) {
	if t.currentGroup() == "" {
		t.flash = "select a group first"
		return t, nil
	}
	t.mode = textModeNewKey
	t.input.SetValue("")
	t.input.Placeholder = "key (will open vim)"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *textTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		t.flash = "no item to delete"
		return t, nil
	}
	t.mode = textModeDeleteConfirm
	return t, nil
}

func (t *textTab) enterExportMode() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		t.flash = "no item to export"
		return t, nil
	}
	t.mode = textModeExportPath
	t.input.SetValue("")
	t.input.Placeholder = "output file path"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *textTab) enterAddGroupMode() (Tab, tea.Cmd) {
	t.mode = textModeAddGroup
	t.input.SetValue("")
	t.input.Placeholder = "group name"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *textTab) enterFilterMode() (Tab, tea.Cmd) {
	t.mode = textModeFilter
	t.filter = ""
	return t, nil
}

// --- vim editing (task 8.2): PrepareEditor -> tea.ExecProcess -> FinishEditor ---

// editCurrent opens the selected text block in vim via tea.ExecProcess.
func (t *textTab) editCurrent() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to edit"
		return t, nil
	}
	return t.editKey(t.currentGroup(), it.key)
}

// editKey prepares an editor session for (group,key) and suspends the TUI.
func (t *textTab) editKey(group, key string) (Tab, tea.Cmd) {
	mgr := t.mgr.Text
	if mgr == nil {
		return t, func() tea.Msg { return errMsg{err: fmt.Errorf("text manager unavailable")} }
	}
	session, err := mgr.PrepareEditor(group, key)
	if err != nil {
		err := err
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	t.flash = "opening editor…"
	return t, tea.ExecProcess(session.EditorCommand(), func(runErr error) tea.Msg {
		return t.finishAfterEdit(session, runErr)
	})
}

// finishAfterEdit is the post-editor callback: on editor failure it cleans up
// the temp file and reports an error without persisting; otherwise it commits
// the (possibly unchanged) edit. Extracted so task 11.3 (editor failure) is
// unit-testable without a real TTY/editor.
func (t *textTab) finishAfterEdit(session *text.EditorSession, runErr error) tea.Msg {
	if runErr != nil {
		// Editor failed/absent: clean up the temp file, do not persist.
		os.Remove(session.TmpPath)
		return errMsg{err: fmt.Errorf("editor failed: %w", runErr)}
	}
	if _, ferr := t.mgr.Text.FinishEditor(session); ferr != nil {
		return errMsg{err: ferr}
	}
	return textReloadMsg{}
}

// --- manager operations ---

func (t *textTab) doDelete(group, key string) tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		if err := mgr.Delete(group, key); err != nil {
			return errMsg{err: err}
		}
		return textReloadMsg{}
	}
}

func (t *textTab) doCopy() tea.Cmd {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to copy"
		return nil
	}
	mgr := t.mgr.Text
	group := t.currentGroup()
	key := it.key
	t.flash = "copied " + key
	return func() tea.Msg {
		if err := mgr.GetToClipboard(group, key); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

func (t *textTab) doExport(group, key, path string) tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		if err := mgr.GetToFile(group, key, path); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

func (t *textTab) doAddGroup(name string) tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		if err := mgr.AddGroup(name); err != nil {
			return errMsg{err: err}
		}
		return textReloadMsg{}
	}
}

// --- view ---

func (t *textTab) SetSize(w, h int) { t.width, t.height = w, h }

func (t *textTab) View() string {
	base := t.viewBase()
	if t.mode == textModeNormal {
		if t.flash != "" {
			return lipgloss.JoinVertical(lipgloss.Left, base,
				statusBarStyle.Foreground(lipgloss.Color(colorSuccess)).Render(t.flash))
		}
		return base
	}
	return lipgloss.JoinVertical(lipgloss.Left, base, t.renderModal())
}

func (t *textTab) viewBase() string {
	leftW := t.width / 4
	if leftW > 26 {
		leftW = 26
	}
	if leftW < 16 {
		leftW = 16
	}
	// Reserve 5 cols for inter-pane chrome: 1 gap + 2 left-pane border +
	// 2 right-pane border (lipgloss draws borders outside Width).
	rightW := t.width - leftW - 5
	if rightW < 4 {
		rightW = 4
	}

	left := t.renderGroups(leftW, t.height)
	right := t.renderItems(rightW, t.height)

	if t.focusLeft {
		left = activePaneStyle.Width(leftW).Height(t.height).Render(left)
		right = paneStyle.Width(rightW).Height(t.height).Render(right)
	} else {
		left = paneStyle.Width(leftW).Height(t.height).Render(left)
		right = activePaneStyle.Width(rightW).Height(t.height).Render(right)
	}
	gap := lipgloss.NewStyle().Width(1).Render(" ")
	return lipgloss.JoinHorizontal(lipgloss.Top, left, gap, right)
}

func (t *textTab) renderGroups(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading groups...")
	}
	if len(t.groups) == 0 {
		return emptyStateStyle.Render("no groups — press + to create one")
	}
	title := paneTitleStyle.Render(fmt.Sprintf("Groups (%d)", len(t.groups)))
	var lines []string
	for i, g := range t.groups {
		line := fmt.Sprintf("%s  [%d]", g.name, g.keyCount)
		if i == t.groupIndex && t.focusLeft {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.JoinVertical(lipgloss.Left, title, body)
}

func (t *textTab) renderItems(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading blocks...")
	}
	group := t.currentGroup()
	if group == "" {
		return emptyStateStyle.Render("select a group")
	}
	items := t.filteredItems()
	header := group
	if t.filter != "" {
		header += "  /" + t.filter
	}
	title := paneTitleStyle.Render(header)
	if len(items) == 0 {
		hint := "no text blocks in this group"
		if t.filter != "" {
			hint = "no keys match /" + t.filter
		}
		return lipgloss.JoinVertical(lipgloss.Left, title, emptyStateStyle.Render(hint))
	}
	var lines []string
	for i, it := range items {
		line := fmt.Sprintf("%-20s %8d b  %s", it.key, it.size, it.updatedAt)
		if i == t.itemIndex {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.JoinVertical(lipgloss.Left, title, body)
}

func (t *textTab) renderModal() string {
	switch t.mode {
	case textModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox("Delete "+it.key+"?", "", "enter/y confirm · esc/n cancel")
	case textModeExportPath:
		return modalBox("Export to file", t.input.View(), "enter export · esc cancel")
	case textModeNewKey:
		return modalBox("New text block — key", t.input.View(), "enter to open vim · esc cancel")
	case textModeAddGroup:
		return modalBox("New group name", t.input.View(), "enter create · esc cancel")
	case textModeFilter:
		return modalBox("Filter keys (case-insensitive)", "/"+t.filter+"_", "esc to clear")
	}
	return ""
}

// Compile-time guard: *textTab satisfies Tab.
var _ Tab = (*textTab)(nil)
