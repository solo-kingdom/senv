package tui

import (
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/config"
)

// configTab renders Config data as a single-column list grouped by group.
// Shows group/name / description / target path / updated time; content only
// via vim edit or detail.
type configTab struct {
	mgr           Managers
	width, height int

	items     []configRow
	itemIndex int
	loaded    bool

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
	return "↑↓/jk move · enter details · e vim edit · n new · i/I install · u/U uninstall · x export · d del · / filter"
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
	items []configRow
	err   error
}

type configReloadMsg struct{}

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

// focusJump positions the cursor at the given config name for search-result
// navigation. It also dismisses detail/filter modes.
func (t *configTab) focusJump(name string) {
	t.filter = ""
	t.mode = configModeNormal
	t.detail = nil
	for i, it := range t.items {
		if it.name == name {
			t.itemIndex = i
			break
		}
	}
}

func (t *configTab) load() tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		if mgr == nil {
			return configLoadedMsg{err: fmt.Errorf("config manager unavailable")}
		}
		cfgs, err := mgr.List("")
		if err != nil {
			return configLoadedMsg{err: err}
		}
		rows := make([]configRow, 0, len(cfgs))
		for _, c := range cfgs {
			rows = append(rows, configRow{name: c.Name, group: c.Group, description: c.Description, targetPath: c.TargetPath, updatedAt: c.UpdatedAt})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].group != rows[j].group {
				return rows[i].group < rows[j].group
			}
			return rows[i].name < rows[j].name
		})
		return configLoadedMsg{items: rows}
	}
}

func (t *configTab) filteredItems() []configRow {
	if t.filter == "" {
		return t.items
	}
	out := make([]configRow, 0, len(t.items))
	for _, it := range t.items {
		if matchKey(it.name, t.filter) || matchKey(it.group, t.filter) || matchKey(it.description, t.filter) {
			out = append(out, it)
		}
	}
	return out
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
		t.items = msg.items
		t.loaded = true
		t.itemIndex = clamp(t.itemIndex, 0, maxLen(t.filteredItems())-1)
		return t, nil

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
			t.itemIndex = clamp(t.itemIndex-1, 0, maxLen(t.filteredItems())-1)
		case "down", "j":
			t.itemIndex = clamp(t.itemIndex+1, 0, maxLen(t.filteredItems())-1)
		case "g":
			t.itemIndex = 0
		case "G":
			t.itemIndex = clamp(len(t.filteredItems())-1, 0, maxLen(t.filteredItems())-1)
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
			return t.enterPlan("install", true)
		case "u":
			return t.enterPlan("uninstall", false)
		case "U":
			return t.enterPlan("uninstall", true)
		case "d":
			return t.enterDeleteConfirm()
		case "/":
			return t.enterFilterMode()
		}
	}
	return t, nil
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
	mgr := t.mgr.Config
	if mgr == nil {
		return t, func() tea.Msg { return errMsg{err: fmt.Errorf("config manager unavailable")} }
	}
	scope := config.Scope{Name: it.name}
	if groupScope {
		scope = config.Scope{Group: it.group}
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
		return configReloadMsg{}
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
	base := t.renderList()
	if t.mode == configModeNormal {
		if t.flash != "" {
			return lipgloss.JoinVertical(lipgloss.Left, base,
				statusBarStyle.Foreground(lipgloss.Color(colorSuccess)).Render(t.flash))
		}
		return base
	}
	return lipgloss.JoinVertical(lipgloss.Left, base, t.renderModal())
}

func (t *configTab) renderList() string {
	if !t.loaded {
		return emptyStateStyle.Render("loading configs...")
	}
	items := t.filteredItems()
	header := fmt.Sprintf("Configs (%d)", len(items))
	if t.filter != "" {
		header += "  /" + t.filter
	}
	title := paneTitleStyle.Render(header)
	if len(items) == 0 {
		hint := "no configuration files"
		if t.filter != "" {
			hint = "no names match /" + t.filter
		}
		return lipgloss.JoinVertical(lipgloss.Left, title, emptyStateStyle.Render(hint))
	}
	var lines []string
	for i, it := range items {
		fullName := it.group + "/" + it.name
		line := fmt.Sprintf("%-22s %-16s %-24s %s", truncRunes(fullName, 22), truncRunes(it.description, 16), truncPath(it.targetPath), it.updatedAt)
		if i == t.itemIndex {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Width(t.width-2) because lipgloss draws the pane border outside Width;
	// total rendered width = t.width, fitting the content area exactly.
	return activePaneStyle.Width(t.width - 2).Height(t.height).Render(
		lipgloss.JoinVertical(lipgloss.Left, title, body))
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

// truncPath shortens a long target path for list display.
func truncPath(p string) string {
	r := []rune(p)
	if len(r) <= 24 {
		return p
	}
	return "…" + string(r[len(r)-23:])
}

// ensure config package is referenced (Manager used via Managers; ConfigInfo
// referenced indirectly through load). This guard keeps the import meaningful.
var _ = config.ConfigInfo{}

// Compile-time guard: *configTab satisfies Tab.
var _ Tab = (*configTab)(nil)
