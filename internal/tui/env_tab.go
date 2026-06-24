package tui

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// envTab renders the Env data: left column = groups (with active ● marker),
// right column = key=value of the selected group (values masked by default).
type envTab struct {
	mgr           Managers
	width, height int

	groups       []envGroupRow
	itemsByGroup map[string][]envItemRow // cached items per group
	groupIndex   int
	loaded       bool

	itemIndex int
	focusLeft bool

	maskRevealed bool                   // per-cursor unmask toggle
	deref        bool                   // show dereferenced values (task 6.1)
	derefResults map[string]derefResult // resolved values for the current group
	derefGroup   string                 // group derefResults corresponds to
	derefErrors  int                    // count of failed resolutions in current view
	filter       string
	filtering    bool

	input         textinput.Model
	mode          envMode
	pendingNewKey string // staging the key during the two-step "new" flow
	flash         string // transient success hint
}

type envGroupRow struct {
	name      string
	isActive  bool
	isDefault bool
	varCount  int
}

type envItemRow struct {
	key   string
	value string // raw stored value
}

type envMode int

const (
	envModeNormal envMode = iota
	envModeEditValue
	envModeNewKey
	envModeNewValue
	envModeDeleteConfirm
	envModeAddGroup
	envModeFilter
)

func newEnvTab(mgr Managers) *envTab {
	ti := textinput.New()
	ti.CharLimit = 0
	return &envTab{mgr: mgr, focusLeft: true, input: ti}
}

func (t *envTab) Title() string { return "Env" }

func (t *envTab) Help() string {
	return "↑↓/jk move · ←→/hl panes · v unmask · e edit · n new · d del · a/x activate · + group · D deref · y yank · / filter"
}

func (t *envTab) InputMode() bool {
	switch t.mode {
	case envModeFilter, envModeEditValue, envModeNewKey, envModeNewValue,
		envModeAddGroup, envModeDeleteConfirm:
		return true
	}
	return false
}

// --- data loading ---

// envLoadedMsg carries the freshly loaded env data into the tab.
type envLoadedMsg struct {
	groups       []envGroupRow
	itemsByGroup map[string][]envItemRow
	err          error
}

// envReloadMsg signals that a mutation succeeded and data should be reloaded.
type envReloadMsg struct{}

// envDerefMsg carries freshly resolved values for the current group (task 6.1).
type envDerefMsg struct {
	group   string
	results map[string]derefResult
}

func (t *envTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *envTab) load() tea.Cmd {
	mgr := t.mgr.Env
	return func() tea.Msg {
		if mgr == nil {
			return envLoadedMsg{err: fmt.Errorf("env manager unavailable")}
		}
		gis, err := mgr.ListGroups()
		if err != nil {
			return envLoadedMsg{err: err}
		}
		allVars, err := mgr.List("") // map[group]map[key]value
		if err != nil {
			return envLoadedMsg{err: err}
		}
		groups := make([]envGroupRow, 0, len(gis))
		itemsByGroup := make(map[string][]envItemRow, len(gis))
		for _, gi := range gis {
			// Hide groups that have no keys, except the default group which is
			// always shown as a stable landing point.
			if gi.VarCount == 0 && !gi.IsDefault {
				continue
			}
			groups = append(groups, envGroupRow{
				name:      gi.Name,
				isActive:  gi.IsActive,
				isDefault: gi.IsDefault,
				varCount:  gi.VarCount,
			})
			itemsByGroup[gi.Name] = buildEnvItems(allVars[gi.Name])
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if groups[i].isDefault != groups[j].isDefault {
				return groups[i].isDefault
			}
			return groups[i].name < groups[j].name
		})
		return envLoadedMsg{groups: groups, itemsByGroup: itemsByGroup}
	}
}

// buildEnvItems converts a group's key->value map into a key-sorted item list.
func buildEnvItems(vars map[string]string) []envItemRow {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]envItemRow, 0, len(keys))
	for _, k := range keys {
		items = append(items, envItemRow{key: k, value: vars[k]})
	}
	return items
}

// resolveDeref computes dereferenced values for the current group's items
// (all of them, so a filter change does not invalidate the cache).
func (t *envTab) resolveDeref() tea.Cmd {
	mgr := t.mgr
	group := t.currentGroup()
	items := t.itemsByGroup[group]
	preview := t.itemsByGroup // capture to avoid stale closure issues
	_ = preview
	return func() tea.Msg {
		results, err := resolveValues(mgr, group, items)
		if err != nil {
			return errMsg{err: err}
		}
		return envDerefMsg{group: group, results: results}
	}
}

func (t *envTab) currentGroup() string {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return ""
	}
	return t.groups[t.groupIndex].name
}

func (t *envTab) filteredItems() []envItemRow {
	all := t.itemsByGroup[t.currentGroup()]
	if t.filter == "" {
		return all
	}
	out := make([]envItemRow, 0, len(all))
	for _, it := range all {
		if matchKey(it.key, t.filter) {
			out = append(out, it)
		}
	}
	return out
}

// currentItem returns the currently selected item, if any.
func (t *envTab) currentItem() (envItemRow, bool) {
	items := t.filteredItems()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return envItemRow{}, false
	}
	return items[t.itemIndex], true
}

// --- update ---

func (t *envTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case envLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.groups = msg.groups
		t.itemsByGroup = msg.itemsByGroup
		t.loaded = true
		t.clampCursors()
		return t, nil

	case envReloadMsg:
		// A mutation succeeded; refresh from storage.
		t.derefResults = nil
		t.derefGroup = ""
		cmd := t.load()
		if t.deref {
			return t, tea.Batch(cmd, t.resolveDeref())
		}
		return t, cmd

	case envDerefMsg:
		if msg.group == t.currentGroup() {
			t.derefResults = msg.results
			t.derefGroup = msg.group
			t.derefErrors = 0
			for _, r := range msg.results {
				if r.failed {
					t.derefErrors++
				}
			}
			if t.derefErrors > 0 {
				t.flash = fmt.Sprintf("%d reference(s) unresolved (raw values shown)", t.derefErrors)
			}
		}
		return t, nil

	case tea.KeyMsg:
		// Clear any transient flash on the next key in normal mode.
		flash := t.flash
		t.flash = ""

		if t.mode != envModeNormal {
			return t.handleModalKey(msg)
		}

		switch msg.String() {
		case "up", "k":
			t.moveCursor(-1)
		case "down", "j":
			t.moveCursor(1)
		case "left", "h":
			t.focusLeft = true
			t.maskRevealed = false
		case "right", "l":
			t.focusLeft = false
			t.maskRevealed = false
		case "v":
			if !t.focusLeft {
				t.maskRevealed = !t.maskRevealed
			}
		case "g":
			t.jumpCursor(0)
		case "G":
			t.jumpCursor(len(t.listForFocus()) - 1)
		case "e":
			return t.enterEditMode()
		case "n":
			return t.enterNewKeyMode()
		case "d":
			return t.enterDeleteConfirm()
		case "a":
			return t, t.doActivate()
		case "x":
			return t, t.doDeactivate()
		case "+":
			return t.enterAddGroupMode()
		case "y":
			return t, t.doCopy()
		case "D":
			t.deref = !t.deref
			t.flash = "dereference: " + onOff(t.deref)
			if t.deref {
				t.derefResults = nil
				t.derefGroup = ""
				return t, t.resolveDeref()
			}
			t.derefResults = nil
			t.derefGroup = ""
		case "/":
			return t.enterFilterMode()
		}

		// If dereference is on and the displayed group changed, refresh the
		// resolved values so the right column reflects the current group.
		if t.deref && t.currentGroup() != "" && t.currentGroup() != t.derefGroup {
			return t, t.resolveDeref()
		}

		// If we did not consume the flash above, keep it for this render.
		if flash != "" && t.flash == "" {
			t.flash = ""
		}
	}
	return t, nil
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// --- navigation helpers ---

func (t *envTab) listForFocus() []string {
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

func (t *envTab) moveCursor(delta int) {
	if t.focusLeft {
		t.groupIndex = clamp(t.groupIndex+delta, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(t.itemIndex+delta, 0, maxLen(items)-1)
	}
	// Moving the cursor re-masks the previously revealed value.
	t.maskRevealed = false
}

func (t *envTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(idx, 0, maxLen(items)-1)
	}
	t.maskRevealed = false
}

func (t *envTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, maxLen(t.groups)-1)
	t.itemIndex = clamp(t.itemIndex, 0, maxLen(t.filteredItems())-1)
}

// focusJump positions the cursor at (group, key) for search-result navigation.
// It clears any filter and mask so the target is visible at its real position.
func (t *envTab) focusJump(group, key string) {
	t.filter = ""
	t.mode = envModeNormal
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
	t.maskRevealed = false
}

// --- modal handling ---

func (t *envTab) handleModalKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	// Delete confirmation uses dedicated keys (no text input).
	if t.mode == envModeDeleteConfirm {
		switch msg.String() {
		case "enter", "y":
			it, ok := t.currentItem()
			t.mode = envModeNormal
			if !ok {
				return t, nil
			}
			return t, t.doDelete(t.currentGroup(), it.key)
		default: // esc, n, anything else
			t.mode = envModeNormal
			return t, nil
		}
	}

	switch msg.String() {
	case "esc":
		if t.mode == envModeFilter {
			t.filter = "" // esc clears the filter and restores the full list
		}
		t.mode = envModeNormal
		t.input.Blur()
		return t, nil
	case "enter":
		return t.submitModal()
	}

	// Filter mode (task 6.2): typing updates the filter live.
	if t.mode == envModeFilter {
		switch msg.String() {
		case "backspace":
			if len(t.filter) > 0 {
				t.filter = t.filter[:len(t.filter)-1]
			}
			t.itemIndex = 0
			return t, nil
		}
		// Append printable runes.
		if isPrintable(msg) {
			t.filter += msg.String()
			t.itemIndex = 0
		}
		return t, nil
	}

	// Default: feed keystrokes to the text input.
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return t, cmd
}

// isPrintable reports whether a KeyMsg is a single printable rune.
func isPrintable(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1
}

func (t *envTab) submitModal() (Tab, tea.Cmd) {
	switch t.mode {
	case envModeEditValue:
		it, ok := t.currentItem()
		if !ok {
			t.mode = envModeNormal
			return t, nil
		}
		value := t.input.Value()
		group := t.currentGroup()
		key := it.key
		t.mode = envModeNormal
		t.input.Blur()
		return t, t.doSet(group, key, value)

	case envModeNewKey:
		key := t.input.Value()
		if key == "" {
			t.flash = "key cannot be empty"
			return t, nil
		}
		t.pendingNewKey = key
		t.input.SetValue("")
		t.mode = envModeNewValue
		t.input.Placeholder = "value"
		t.input.Focus()
		return t, textinput.Blink

	case envModeNewValue:
		value := t.input.Value()
		group := t.currentGroup()
		key := t.pendingNewKey
		t.mode = envModeNormal
		t.pendingNewKey = ""
		t.input.Blur()
		return t, t.doSet(group, key, value)

	case envModeAddGroup:
		name := t.input.Value()
		t.mode = envModeNormal
		t.input.Blur()
		if name == "" {
			t.flash = "group name cannot be empty"
			return t, nil
		}
		return t, t.doAddGroup(name)
	}
	t.mode = envModeNormal
	return t, nil
}

// --- modal entry points ---

func (t *envTab) enterEditMode() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to edit"
		return t, nil
	}
	t.mode = envModeEditValue
	t.input.Placeholder = "value"
	t.input.SetValue(it.value)
	t.input.Focus()
	t.input.CursorEnd()
	return t, textinput.Blink
}

func (t *envTab) enterNewKeyMode() (Tab, tea.Cmd) {
	if t.currentGroup() == "" {
		t.flash = "select a group first"
		return t, nil
	}
	t.mode = envModeNewKey
	t.pendingNewKey = ""
	t.input.SetValue("")
	t.input.Placeholder = "key"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *envTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		t.flash = "no item to delete"
		return t, nil
	}
	t.mode = envModeDeleteConfirm
	return t, nil
}

func (t *envTab) enterAddGroupMode() (Tab, tea.Cmd) {
	t.mode = envModeAddGroup
	t.input.SetValue("")
	t.input.Placeholder = "group name"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *envTab) enterFilterMode() (Tab, tea.Cmd) {
	t.mode = envModeFilter
	t.filter = ""
	return t, nil
}

// --- manager operations (executed in a command goroutine) ---

func (t *envTab) doSet(group, key, value string) tea.Cmd {
	mgr := t.mgr.Env
	return func() tea.Msg {
		if err := mgr.Set(group, key, value); err != nil {
			return errMsg{err: err}
		}
		return envReloadMsg{}
	}
}

func (t *envTab) doDelete(group, key string) tea.Cmd {
	mgr := t.mgr.Env
	return func() tea.Msg {
		if err := mgr.Delete(group, key); err != nil {
			return errMsg{err: err}
		}
		return envReloadMsg{}
	}
}

func (t *envTab) doActivate() tea.Cmd {
	name := t.currentGroup()
	mgr := t.mgr.Env
	return func() tea.Msg {
		if err := mgr.ActivateGroup(name); err != nil {
			return errMsg{err: err}
		}
		return envReloadMsg{}
	}
}

func (t *envTab) doDeactivate() tea.Cmd {
	name := t.currentGroup()
	if g, ok := t.currentGroupRow(); ok && g.isDefault {
		t.flash = "cannot deactivate default group"
		return nil
	}
	mgr := t.mgr.Env
	return func() tea.Msg {
		if err := mgr.DeactivateGroup(name); err != nil {
			return errMsg{err: err}
		}
		return envReloadMsg{}
	}
}

func (t *envTab) doAddGroup(name string) tea.Cmd {
	mgr := t.mgr.Env
	return func() tea.Msg {
		if err := mgr.AddGroup(name); err != nil {
			return errMsg{err: err}
		}
		return envReloadMsg{}
	}
}

func (t *envTab) doCopy() tea.Cmd {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to copy"
		return nil
	}
	value := it.value
	t.flash = "copied " + it.key
	return func() tea.Msg {
		if err := copyToClipboard(value); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

func (t *envTab) currentGroupRow() (envGroupRow, bool) {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return envGroupRow{}, false
	}
	return t.groups[t.groupIndex], true
}

// --- view ---

func (t *envTab) SetSize(w, h int) { t.width, t.height = w, h }

func (t *envTab) View() string {
	base := t.viewBase()
	if t.mode == envModeNormal {
		if t.flash != "" {
			return lipgloss.JoinVertical(lipgloss.Left, base,
				statusBarStyle.Foreground(lipgloss.Color(colorSuccess)).Render(t.flash))
		}
		return base
	}
	return lipgloss.JoinVertical(lipgloss.Left, base, t.renderModal())
}

func (t *envTab) viewBase() string {
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

func (t *envTab) renderGroups(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading groups...")
	}
	if len(t.groups) == 0 {
		return emptyStateStyle.Render("no groups — press + to create one")
	}
	title := paneTitleStyle.Render(fmt.Sprintf("Groups (%d)", len(t.groups)))
	var lines []string
	for i, g := range t.groups {
		marker := " "
		if g.isActive {
			marker = "●"
		}
		name := g.name
		if g.isDefault {
			name += " (default)"
		}
		line := fmt.Sprintf("%s %s  [%d]", marker, name, g.varCount)
		if i == t.groupIndex && t.focusLeft {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.JoinVertical(lipgloss.Left, title, body)
}

func (t *envTab) renderItems(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading variables...")
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
		hint := "no variables in this group"
		if t.filter != "" {
			hint = "no keys match /" + t.filter
		}
		return lipgloss.JoinVertical(lipgloss.Left, title, emptyStateStyle.Render(hint))
	}
	var lines []string
	for i, it := range items {
		shown := it.value
		failed := false
		if t.deref {
			if dr, ok := t.derefResults[it.key]; ok {
				shown = dr.resolved
				failed = dr.failed
			}
		}
		revealed := i == t.itemIndex && t.maskRevealed
		if !revealed {
			shown = maskValue(shown)
		}
		keyLabel := it.key
		if failed {
			keyLabel = "⚠ " + it.key
		}
		var line string
		if i == t.itemIndex {
			line = selectedLineStyle.Render("▸ "+keyLabel+"=") + renderValue(shown, revealed)
		} else {
			line = keyLabel + "=" + renderValue(shown, false)
		}
		lines = append(lines, line)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.JoinVertical(lipgloss.Left, title, body)
}

// renderModal renders the active modal input/confirm panel.
func (t *envTab) renderModal() string {
	switch t.mode {
	case envModeEditValue:
		return modalBox("Edit "+t.currentItemKeyLabel(), t.input.View(), "enter save · esc cancel")
	case envModeNewKey:
		return modalBox("New variable — key", t.input.View(), "enter next · esc cancel")
	case envModeNewValue:
		return modalBox("New value for "+t.pendingNewKey, t.input.View(), "enter save · esc cancel")
	case envModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox("Delete "+it.key+"?", "", "enter/y confirm · esc/n cancel")
	case envModeAddGroup:
		return modalBox("New group name", t.input.View(), "enter create · esc cancel")
	case envModeFilter:
		return modalBox("Filter keys (case-insensitive)", "/"+t.filter+"_", "esc to clear")
	}
	return ""
}

func (t *envTab) currentItemKeyLabel() string {
	if it, ok := t.currentItem(); ok {
		return it.key
	}
	return "?"
}

// modalBox renders a bordered modal line with a title, body and hint.
func modalBox(title, body, hint string) string {
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent)).Render("» " + title)
	parts := []string{head}
	if body != "" {
		parts = append(parts, body)
	}
	if hint != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render(hint))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorAccent)).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return box
}

// --- masking ---

func maskValue(v string) string {
	if v == "" {
		return "***"
	}
	r := []rune(v)
	const prefix = 3
	if len(r) <= prefix {
		return string(r[:1]) + "***"
	}
	return string(r[:prefix]) + "***"
}

func renderValue(v string, revealed bool) string {
	if revealed {
		return v
	}
	return maskedValueStyle.Render(v)
}

// --- helpers ---

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxLen[T any](s []T) int { return len(s) }

// Compile-time guard: *envTab satisfies Tab.
var _ Tab = (*envTab)(nil)
