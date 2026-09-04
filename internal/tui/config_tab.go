package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/storage"
)

// configTab renders Config data as two panes: left = group sidebar (with an
// "All" pseudo-group on top), right = config entries of the selected group.
// Shows name / description / target path / updated time; content only via
// vim edit or detail.
type configTab struct {
	mgr           Managers
	width, height int

	groups       []configGroupRow       // index 0 is always the "All" pseudo-group
	itemsByGroup map[string][]configRow // cached items per real group
	groupIndex   int
	focusLeft    bool
	itemIndex    int
	loaded       bool

	// pendingFocus* position the cursor on a freshly created entry once the
	// reload triggered by configCreatedMsg lands.
	pendingFocusName  string
	pendingFocusGroup string

	filter    string
	filtering bool

	input         textinput.Model
	mode          configMode
	flash         string
	pendingName   string // staging create: name, source, target, group, description
	pendingSource string
	pendingTarget string
	pendingGroup  string
	detail        *configDetail // set when viewing details

	plan *planState // pending install/uninstall plan awaiting confirmation
}

// planState holds a computed install/uninstall plan while the user confirms
// it, plus per-item decisions for changed uninstall targets.
type planState struct {
	kind           string // "install" | "uninstall"
	scope          config.Scope
	installPlan    *config.InstallPlan
	uninstallPlan  *config.UninstallPlan
	changedIdx     int             // iteration pointer over changed uninstall items
	changedAllowed map[string]bool // name -> user decision
}

type configRow struct {
	name        string
	group       string
	description string
	targetPath  string
	updatedAt   string
}

// allConfigsLabel is the pseudo-group pinned to the top of the sidebar. It is
// identified by index (groups[0]), never by name, so a real group literally
// named "All" stays unambiguous.
const allConfigsLabel = "All"

type configGroupRow struct {
	name string // real group name, or allConfigsLabel at index 0
}

type configDetail struct {
	name        string
	group       string
	description string
	targetPath  string
	createdAt   string
	updatedAt   string
}

type configMode int

const (
	configModeNormal configMode = iota
	configModeDetail
	configModeDeleteConfirm
	configModeExportPath
	configModeCreateName
	configModeCreateSource
	configModeCreateTarget
	configModeCreateGroup
	configModeCreateDesc
	configModeFilter
	configModePlan
	configModeChangedConfirm
)

func newConfigTab(mgr Managers) *configTab {
	ti := textinput.New()
	ti.CharLimit = 0
	return &configTab{mgr: mgr, input: ti}
}

func (t *configTab) Title() string { return "Config" }

func (t *configTab) Help() string {
	return "↑↓/jk move · ←→/hl panes · enter details · e vim edit · n new · i/I install · u/U uninstall · x export · d del · / filter"
}

func (t *configTab) InputMode() bool {
	switch t.mode {
	case configModeFilter, configModeExportPath, configModeCreateName,
		configModeCreateSource, configModeCreateTarget,
		configModeCreateGroup, configModeCreateDesc:
		return true
	}
	return false
}

// --- data loading ---

type configLoadedMsg struct {
	groups       []configGroupRow
	itemsByGroup map[string][]configRow
	warnings     []config.QuarantineWarning
	err          error
}

type configReloadMsg struct{}

// configCreatedMsg reports a successful Create so the tab can reload and then
// focus the new entry in its group.
type configCreatedMsg struct {
	name  string
	group string // as normalized by the manager (never empty)
}

// configDetailLoadedMsg carries the result of Get for the detail panel.
type configDetailLoadedMsg struct {
	name string
	det  *configDetail
	err  error
}

func (t *configTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

// focusJump positions the cursor at (group, name) for search-result
// navigation. It also dismisses detail/filter modes. An empty or unknown
// group falls back to the "All" view so stale/foreign data cannot strand the
// cursor.
func (t *configTab) focusJump(group, name string) {
	t.filter = ""
	t.mode = configModeNormal
	t.detail = nil
	t.focusLeft = false
	t.itemIndex = 0
	t.positionAt(group, name)
}

// positionAt selects the given group in the sidebar and the named entry in
// the item list. Falls back to the "All" view when the group is unknown or
// the entry is not listed under it.
func (t *configTab) positionAt(group, name string) {
	t.groupIndex = 0
	if group != "" {
		for i := 1; i < len(t.groups); i++ {
			if t.groups[i].name == group {
				t.groupIndex = i
				break
			}
		}
	}
	items := t.filteredItems()
	for i, it := range items {
		if it.name == name {
			t.itemIndex = i
			return
		}
	}
	if t.groupIndex != 0 {
		t.groupIndex = 0
		items = t.filteredItems()
		for i, it := range items {
			if it.name == name {
				t.itemIndex = i
				return
			}
		}
	}
}

func (t *configTab) load() tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		if mgr == nil {
			return configLoadedMsg{err: fmt.Errorf("config manager unavailable")}
		}
		cfgs, warnings, err := mgr.ListWithWarnings("")
		if err != nil {
			return configLoadedMsg{err: err}
		}
		itemsByGroup := make(map[string][]configRow)
		for _, c := range cfgs {
			itemsByGroup[c.Group] = append(itemsByGroup[c.Group], configRow{
				name: c.Name, group: c.Group, description: c.Description,
				targetPath: c.TargetPath, updatedAt: c.UpdatedAt,
			})
		}
		names := make([]string, 0, len(itemsByGroup))
		for g := range itemsByGroup {
			names = append(names, g)
		}
		sort.Strings(names)
		// Sidebar: "All" pseudo-group pinned at index 0, then real groups sorted.
		groups := make([]configGroupRow, 0, len(names)+1)
		groups = append(groups, configGroupRow{name: allConfigsLabel})
		for _, g := range names {
			items := itemsByGroup[g]
			sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
			itemsByGroup[g] = items
			groups = append(groups, configGroupRow{name: g})
		}
		return configLoadedMsg{groups: groups, itemsByGroup: itemsByGroup, warnings: warnings}
	}
}

// currentGroup returns the selected real group, or "" when the "All"
// pseudo-group (index 0) is selected.
func (t *configTab) currentGroup() string {
	if t.groupIndex <= 0 || t.groupIndex >= len(t.groups) {
		return ""
	}
	return t.groups[t.groupIndex].name
}

// baseItems returns the unfiltered entries of the selected view: the chosen
// group's entries, or every group's entries concatenated in sidebar order
// (which preserves the previous group-then-name ordering for "All").
func (t *configTab) baseItems() []configRow {
	if g := t.currentGroup(); g != "" {
		return t.itemsByGroup[g]
	}
	var out []configRow
	for i := 1; i < len(t.groups); i++ {
		out = append(out, t.itemsByGroup[t.groups[i].name]...)
	}
	return out
}

func (t *configTab) matchesFilter(it configRow) bool {
	if t.filter == "" {
		return true
	}
	return matchKey(it.name, t.filter) || matchKey(it.group, t.filter) || matchKey(it.description, t.filter)
}

func (t *configTab) filteredItems() []configRow {
	base := t.baseItems()
	if t.filter == "" {
		return base
	}
	out := make([]configRow, 0, len(base))
	for _, it := range base {
		if t.matchesFilter(it) {
			out = append(out, it)
		}
	}
	return out
}

// sidebarCount returns the filter-aware entry count for sidebar row i
// (0 = "All" totals every real group).
func (t *configTab) sidebarCount(i int) int {
	if i == 0 {
		n := 0
		for j := 1; j < len(t.groups); j++ {
			n += t.sidebarCount(j)
		}
		return n
	}
	if i < 0 || i >= len(t.groups) {
		return 0
	}
	n := 0
	for _, it := range t.itemsByGroup[t.groups[i].name] {
		if t.matchesFilter(it) {
			n++
		}
	}
	return n
}

func (t *configTab) currentItem() (configRow, bool) {
	items := t.filteredItems()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return configRow{}, false
	}
	return items[t.itemIndex], true
}

// --- update ---

func (t *configTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case configLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.groups = msg.groups
		t.itemsByGroup = msg.itemsByGroup
		t.loaded = true
		t.clampCursors()
		if t.pendingFocusName != "" {
			name, group := t.pendingFocusName, t.pendingFocusGroup
			t.pendingFocusName, t.pendingFocusGroup = "", ""
			t.positionAt(group, name)
		}
		if len(msg.warnings) == 0 {
			return t, func() tea.Msg { return clearWarnMsg{} }
		}
		parts := make([]string, 0, len(msg.warnings))
		for _, w := range msg.warnings {
			parts = append(parts, fmt.Sprintf("跳过配置 %s", w.OldName))
		}
		text := strings.Join(parts, "、") + "：名称不可移植，运行 senv config repair 修复"
		return t, func() tea.Msg { return warnMsg{text: text} }

	case configCreatedMsg:
		// Reload, then land the cursor on the new entry in its own group.
		t.pendingFocusName = msg.name
		t.pendingFocusGroup = msg.group
		return t, t.load()

	case configReloadMsg:
		return t, t.load()

	case configDetailLoadedMsg:
		if msg.err != nil {
			err := msg.err
			t.mode = configModeNormal
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.detail = msg.det
		t.mode = configModeDetail
		return t, nil

	case configPlanLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.plan = &planState{
			kind:          msg.kind,
			scope:         msg.scope,
			installPlan:   msg.installPlan,
			uninstallPlan: msg.uninstallPlan,
		}
		t.mode = configModePlan
		return t, nil

	case tea.KeyMsg:
		t.flash = ""

		if t.mode == configModePlan || t.mode == configModeChangedConfirm {
			return t.handlePlanKey(msg)
		}

		if t.mode != configModeNormal && t.mode != configModeDetail {
			return t.handleModalKey(msg)
		}

		// Detail view: any key returns to the list (except esc which is explicit).
		if t.mode == configModeDetail {
			t.mode = configModeNormal
			t.detail = nil
			return t, nil
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
			t.jumpCursor(t.focusListLen() - 1)
		case "enter":
			return t.showDetail()
		case "e":
			return t.editCurrent()
		case "n":
			return t.enterCreateName()
		case "x":
			return t.doExportCurrent()
		case "i":
			return t.enterPlan("install", false)
		case "I":
			if t.focusLeft {
				return t.enterSidebarPlan("install")
			}
			return t.enterPlan("install", true)
		case "u":
			return t.enterPlan("uninstall", false)
		case "U":
			if t.focusLeft {
				return t.enterSidebarPlan("uninstall")
			}
			return t.enterPlan("uninstall", true)
		case "d":
			return t.enterDeleteConfirm()
		case "/":
			return t.enterFilterMode()
		}
	}
	return t, nil
}

// --- navigation ---

// focusListLen is the cursor list length of the currently focused pane.
func (t *configTab) focusListLen() int {
	if t.focusLeft {
		return len(t.groups)
	}
	return len(t.filteredItems())
}

func (t *configTab) moveCursor(delta int) {
	if t.focusLeft {
		t.groupIndex = clamp(t.groupIndex+delta, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(t.itemIndex+delta, 0, maxLen(t.filteredItems())-1)
}

func (t *configTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(idx, 0, maxLen(t.filteredItems())-1)
}

func (t *configTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, maxLen(t.groups)-1)
	t.itemIndex = clamp(t.itemIndex, 0, maxLen(t.filteredItems())-1)
}

// --- modal handling ---

func (t *configTab) handleModalKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	if t.mode == configModeDeleteConfirm {
		switch msg.String() {
		case "enter", "y":
			it, ok := t.currentItem()
			t.mode = configModeNormal
			if !ok {
				return t, nil
			}
			return t, t.doDelete(it.name)
		default:
			t.mode = configModeNormal
			return t, nil
		}
	}

	switch msg.String() {
	case "esc":
		if t.mode == configModeFilter {
			t.filter = ""
		}
		t.mode = configModeNormal
		t.input.Blur()
		return t, nil
	case "enter":
		return t.submitModal()
	}

	if t.mode == configModeFilter {
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

func (t *configTab) submitModal() (Tab, tea.Cmd) {
	switch t.mode {
	case configModeExportPath:
		path := t.input.Value()
		it, ok := t.currentItem()
		t.mode = configModeNormal
		t.input.Blur()
		if !ok {
			return t, nil
		}
		return t, t.doExport(it.name, path)
	case configModeCreateName:
		name := t.input.Value()
		if name == "" {
			t.flash = "name cannot be empty"
			return t, nil
		}
		t.pendingName = name
		t.input.SetValue("")
		t.input.Placeholder = "source file path"
		t.mode = configModeCreateSource
		t.input.Focus()
		return t, textinput.Blink
	case configModeCreateSource:
		src := t.input.Value()
		if src == "" {
			t.flash = "source path cannot be empty"
			return t, nil
		}
		t.pendingSource = src
		t.input.SetValue("")
		t.input.Placeholder = "target file path"
		t.mode = configModeCreateTarget
		t.input.Focus()
		return t, textinput.Blink
	case configModeCreateTarget:
		target := t.input.Value()
		if target == "" {
			t.flash = "target path cannot be empty"
			return t, nil
		}
		t.pendingTarget = target
		t.input.SetValue("")
		t.input.Placeholder = "group (optional, default: default)"
		t.mode = configModeCreateGroup
		t.input.Focus()
		return t, textinput.Blink
	case configModeCreateGroup:
		t.pendingGroup = t.input.Value()
		t.input.SetValue("")
		t.input.Placeholder = "description (optional)"
		t.mode = configModeCreateDesc
		t.input.Focus()
		return t, textinput.Blink
	case configModeCreateDesc:
		desc := t.input.Value()
		name := t.pendingName
		src := t.pendingSource
		target := t.pendingTarget
		group := t.pendingGroup
		t.mode = configModeNormal
		t.pendingName = ""
		t.pendingSource = ""
		t.pendingTarget = ""
		t.pendingGroup = ""
		t.input.Blur()
		return t, t.doCreate(name, src, target, group, desc)
	}
	t.mode = configModeNormal
	return t, nil
}

// --- entry points ---

func (t *configTab) showDetail() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item selected"
		return t, nil
	}
	mgr := t.mgr.Config
	name := it.name
	return t, func() tea.Msg {
		ci, err := mgr.Get(name)
		if err != nil {
			return configDetailLoadedMsg{name: name, err: err}
		}
		return configDetailLoadedMsg{name: name, det: &configDetail{
			name: ci.Name, group: ci.Group, description: ci.Description, targetPath: ci.TargetPath,
			createdAt: ci.CreatedAt, updatedAt: ci.UpdatedAt,
		}}
	}
}

func (t *configTab) editCurrent() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to edit"
		return t, nil
	}
	mgr := t.mgr.Config
	if mgr == nil {
		return t, func() tea.Msg { return errMsg{err: fmt.Errorf("config manager unavailable")} }
	}
	session, err := mgr.PrepareEdit(it.name)
	if err != nil {
		err := err
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	t.flash = "opening editor…"
	return t, tea.ExecProcess(session.EditorCommand(), func(runErr error) tea.Msg {
		return t.finishAfterEdit(session, runErr)
	})
}

// finishAfterEdit is the post-editor callback for config: on editor failure it
// cleans up the temp file and reports an error without persisting; otherwise it
// commits the edit. Extracted so task 11.3 is unit-testable.
func (t *configTab) finishAfterEdit(session *config.ConfigEditSession, runErr error) tea.Msg {
	if runErr != nil {
		os.Remove(session.TmpPath)
		return errMsg{err: fmt.Errorf("editor failed: %w", runErr)}
	}
	if _, ferr := t.mgr.Config.FinishEdit(session); ferr != nil {
		return errMsg{err: ferr}
	}
	return configReloadMsg{}
}

func (t *configTab) enterCreateName() (Tab, tea.Cmd) {
	t.mode = configModeCreateName
	t.pendingName = ""
	t.pendingSource = ""
	t.pendingTarget = ""
	t.pendingGroup = ""
	t.input.SetValue("")
	t.input.Placeholder = "config name"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *configTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		t.flash = "no item to delete"
		return t, nil
	}
	t.mode = configModeDeleteConfirm
	return t, nil
}

func (t *configTab) enterFilterMode() (Tab, tea.Cmd) {
	t.mode = configModeFilter
	t.filter = ""
	return t, nil
}

// --- install / uninstall plan flow ---

// configPlanLoadedMsg carries a computed install/uninstall plan.
type configPlanLoadedMsg struct {
	kind          string
	scope         config.Scope
	installPlan   *config.InstallPlan
	uninstallPlan *config.UninstallPlan
	err           error
}

// enterPlan computes an install/uninstall plan for the current item (or its
// whole group when groupScope is true) and switches to the plan preview.
func (t *configTab) enterPlan(kind string, groupScope bool) (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item selected"
		return t, nil
	}
	scope := config.Scope{Name: it.name}
	if groupScope {
		scope = config.Scope{Group: it.group}
	}
	return t.planForScope(kind, scope)
}

// enterSidebarPlan handles group-scope install/uninstall triggered while the
// group sidebar has focus: the selected real group is the scope. The "All"
// pseudo-group has no group scope and only shows a hint.
func (t *configTab) enterSidebarPlan(kind string) (Tab, tea.Cmd) {
	g := t.currentGroup()
	if g == "" {
		t.flash = "select a concrete group for group-wide " + kind + " (All has no group scope)"
		return t, nil
	}
	return t.planForScope(kind, config.Scope{Group: g})
}

// planForScope computes an install/uninstall plan for the given scope and
// switches to the plan preview once loaded.
func (t *configTab) planForScope(kind string, scope config.Scope) (Tab, tea.Cmd) {
	mgr := t.mgr.Config
	if mgr == nil {
		return t, func() tea.Msg { return errMsg{err: fmt.Errorf("config manager unavailable")} }
	}
	return t, func() tea.Msg {
		msg := configPlanLoadedMsg{kind: kind, scope: scope}
		if kind == "install" {
			plan, err := mgr.PlanInstall(scope)
			msg.installPlan, msg.err = plan, err
		} else {
			plan, err := mgr.PlanUninstall(scope)
			msg.uninstallPlan, msg.err = plan, err
		}
		return msg
	}
}

// nextChangedItem advances changedIdx to the next changed uninstall item and
// reports whether one was found.
func (t *configTab) nextChangedItem() bool {
	items := t.plan.uninstallPlan.Items
	for t.plan.changedIdx < len(items) {
		if items[t.plan.changedIdx].Action == config.ActionChanged {
			return true
		}
		t.plan.changedIdx++
	}
	return false
}

func (t *configTab) handlePlanKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	if t.mode == configModeChangedConfirm {
		item := t.plan.uninstallPlan.Items[t.plan.changedIdx]
		t.plan.changedAllowed[item.Name] = msg.String() == "y"
		t.plan.changedIdx++
		if t.nextChangedItem() {
			return t, nil
		}
		// All changed items answered: execute.
		t.mode = configModeNormal
		plan := t.plan
		t.plan = nil
		return t, t.executePlan(plan)
	}

	// configModePlan
	switch msg.String() {
	case "y", "enter":
		if t.plan.kind == "uninstall" && t.plan.uninstallPlan.HasChanged() {
			t.plan.changedAllowed = map[string]bool{}
			t.plan.changedIdx = 0
			t.nextChangedItem()
			t.mode = configModeChangedConfirm
			return t, nil
		}
		t.mode = configModeNormal
		plan := t.plan
		t.plan = nil
		return t, t.executePlan(plan)
	default: // esc / n / anything else cancels
		t.mode = configModeNormal
		t.plan = nil
		t.flash = "cancelled"
		return t, nil
	}
}

// executePlan runs a confirmed plan and reloads the list afterwards.
func (t *configTab) executePlan(ps *planState) tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		var err error
		if ps.kind == "install" {
			err = mgr.ExecuteInstall(ps.installPlan)
		} else {
			err = mgr.ExecuteUninstall(ps.uninstallPlan, func(item config.UninstallItem) bool {
				return ps.changedAllowed[item.Name]
			})
		}
		if err != nil {
			return errMsg{err: err}
		}
		return configReloadMsg{}
	}
}

// --- operations ---

func (t *configTab) doDelete(name string) tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		if err := mgr.Delete(name); err != nil {
			return errMsg{err: err}
		}
		return configReloadMsg{}
	}
}

// doExportCurrent exports to the config's default target path.
func (t *configTab) doExportCurrent() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to export"
		return t, nil
	}
	return t, t.doExport(it.name, "") // empty -> config's TargetPath
}

func (t *configTab) doExport(name, path string) tea.Cmd {
	mgr := t.mgr.Config
	t.flash = "exported " + name
	return func() tea.Msg {
		if err := mgr.Export(name, path); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

func (t *configTab) doCreate(name, source, target, group, description string) tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		if err := mgr.Create(name, source, target, group, description); err != nil {
			return errMsg{err: err}
		}
		// The manager normalizes an empty group to "default"; mirror that here
		// so the post-reload focus lands in the entry's real group.
		if group == "" {
			group = storage.ConfigDefaultGroup
		}
		return configCreatedMsg{name: name, group: group}
	}
}

// --- view ---

func (t *configTab) SetSize(w, h int) { t.width, t.height = w, h }

func (t *configTab) View() string {
	if (t.mode == configModePlan || t.mode == configModeChangedConfirm) && t.plan != nil {
		return t.renderPlan()
	}
	if t.mode == configModeDetail && t.detail != nil {
		return t.renderDetail()
	}
	base := t.viewBase()
	if t.mode == configModeNormal {
		if t.flash != "" {
			return lipgloss.JoinVertical(lipgloss.Left, base,
				statusBarStyle.Foreground(lipgloss.Color(colorSuccess)).Render(t.flash))
		}
		return base
	}
	return lipgloss.JoinVertical(lipgloss.Left, base, t.renderModal())
}

func (t *configTab) viewBase() string {
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

func (t *configTab) renderGroups(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading groups...")
	}
	inner := width - 2
	var lines []string
	for i, g := range t.groups {
		line := truncateRunes(fmt.Sprintf("%s  [%d]", g.name, t.sidebarCount(i)), inner-2)
		if i == t.groupIndex && t.focusLeft {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	// Count excludes the "All" pseudo-group.
	return windowedPane(fmt.Sprintf("Groups (%d)", max(0, len(t.groups)-1)), lines, t.groupIndex, height, width)
}

func (t *configTab) renderItems(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading configs...")
	}
	items := t.filteredItems()
	label := t.currentGroup()
	if label == "" {
		label = allConfigsLabel
	}
	header := fmt.Sprintf("%s (%d)", label, len(items))
	if t.filter != "" {
		header += "  /" + t.filter
	}
	if len(items) == 0 {
		hint := "no configuration files"
		if t.filter != "" {
			hint = "no names match /" + t.filter
		}
		return lipgloss.JoinVertical(lipgloss.Left, paneTitleStyle.Render(header), emptyStateStyle.Render(hint))
	}
	inner := width - 2
	// Column budget: name 22 + desc 14 + updated 16 + 3 separators; the path
	// column takes the rest. Reserve 2 cols for the cursor marker so a selected
	// line cannot wrap and inflate the pane.
	pathW := inner - 2 - 22 - 14 - 16 - 5
	if pathW < 8 {
		pathW = 8
	}
	var lines []string
	for i, it := range items {
		displayName := it.name
		if t.currentGroup() == "" {
			displayName = it.group + "/" + it.name
		}
		line := truncateRunes(fmt.Sprintf("%-22s %-14s %-*s %s",
			truncRunes(displayName, 22), truncRunes(it.description, 14),
			pathW, truncPathN(it.targetPath, pathW), it.updatedAt), inner-2)
		if i == t.itemIndex {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	return windowedPane(header, lines, t.itemIndex, height, width)
}

// renderPlan renders the install/uninstall plan preview and, during changed
// confirmation, the per-item prompt.
func (t *configTab) renderPlan() string {
	var lines []string
	title := "Install plan"
	if t.plan.kind == "uninstall" {
		title = "Uninstall plan"
	}
	if t.plan.installPlan != nil {
		for _, item := range t.plan.installPlan.Items {
			lines = append(lines, fmt.Sprintf("  [%s] %s -> %s (%s)", item.Action, item.Name, truncRunes(item.TargetPath, 40), item.Reason))
		}
	} else {
		for _, item := range t.plan.uninstallPlan.Items {
			marker := item.Action
			if item.Action == config.ActionChanged {
				marker = "CHANGED"
			}
			lines = append(lines, fmt.Sprintf("  [%s] %s -> %s (%s)", marker, item.Name, truncRunes(item.TargetPath, 40), item.Reason))
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)

	if t.mode == configModeChangedConfirm {
		item := t.plan.uninstallPlan.Items[t.plan.changedIdx]
		prompt := fmt.Sprintf("目标文件已被本地修改，确认删除 %s? y delete · n keep", item.TargetPath)
		return modalBox(title, body+"\n\n"+prompt, "")
	}
	return modalBox(title, body, "y confirm · esc cancel")
}

// truncRunes shortens a string to at most n runes with an ellipsis.
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (t *configTab) renderDetail() string {
	d := t.detail
	body := fmt.Sprintf(
		"Name:    %s\nGroup:   %s\nDesc:    %s\nTarget:  %s\nCreated: %s\nUpdated: %s",
		d.name, d.group, d.description, d.targetPath, d.createdAt, d.updatedAt)
	box := modalBox("Config details", body, "any key to close")
	return box
}

func (t *configTab) renderModal() string {
	switch t.mode {
	case configModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox("Delete "+it.name+"?", "", "enter/y confirm · esc/n cancel")
	case configModeExportPath:
		return modalBox("Export to file", t.input.View(), "enter export · esc cancel")
	case configModeCreateName:
		return modalBox("New config — name", t.input.View(), "enter next · esc cancel")
	case configModeCreateSource:
		return modalBox("Source file path for "+t.pendingName, t.input.View(), "enter next · esc cancel")
	case configModeCreateTarget:
		return modalBox("Target file path for "+t.pendingName, t.input.View(), "enter next · esc cancel")
	case configModeCreateGroup:
		return modalBox("Group for "+t.pendingName+" (optional)", t.input.View(), "enter next · esc cancel")
	case configModeCreateDesc:
		return modalBox("Description for "+t.pendingName+" (optional)", t.input.View(), "enter create · esc cancel")
	case configModeFilter:
		return modalBox("Filter names (case-insensitive)", "/"+t.filter+"_", "esc to clear")
	}
	return ""
}

// truncPath shortens a long target path for list display (24 runes).
func truncPath(p string) string { return truncPathN(p, 24) }

// truncPathN shortens a long target path to at most n runes, keeping the
// tail (the distinguishing part of a path) with a leading ellipsis.
func truncPathN(p string, n int) string {
	r := []rune(p)
	if len(r) <= n {
		return p
	}
	return "…" + string(r[len(r)-(n-1):])
}

// ensure config package is referenced (Manager used via Managers; ConfigInfo
// referenced indirectly through load). This guard keeps the import meaningful.
var _ = config.ConfigInfo{}

// Compile-time guard: *configTab satisfies Tab.
var _ Tab = (*configTab)(nil)
