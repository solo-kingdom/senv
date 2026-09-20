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
	"github.com/wii/senv/internal/perflog"
	sessionpkg "github.com/wii/senv/internal/session"
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

	filterBox Filter
	// sel 仅承载条目多选集（条目名即稳定标识；跨分组在 All 视图可用）。
	sel List

	// form 非 nil 时表示打开了一个结构化表单（重命名/元信息编辑）。
	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	input  textinput.Model
	mode   configMode
	detail *configDetail // set when viewing details

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

func (t *configTab) Bindings() []KeyAction {
	if t.form != nil {
		return formBindings()
	}
	switch t.mode {
	case configModeFilter:
		return filterBindings(false)
	case configModeExportPath:
		return []KeyAction{
			{[]string{"enter"}, "export", grpForm, false},
			{[]string{"esc"}, "cancel", grpForm, false},
		}
	case configModeDeleteConfirm:
		return confirmBindings()
	case configModePlan:
		return []KeyAction{
			{[]string{"y/enter"}, "confirm", grpConfirm, false},
			{[]string{"esc/n"}, "cancel", grpConfirm, false},
		}
	case configModeChangedConfirm:
		return []KeyAction{
			{[]string{"y"}, "delete", grpConfirm, false},
			{[]string{"n"}, "keep", grpConfirm, false},
		}
	case configModeDetail:
		return []KeyAction{{[]string{"esc/any"}, "close", grpConfirm, false}}
	}
	nav := navBindings(true)
	if t.focusLeft {
		return append(nav,
			KeyAction{[]string{"I/U"}, "install/uninstall group", grpGroup, false},
			// 侧栏仍可触发的条目动词：只进 `?`
			KeyAction{[]string{"enter"}, "view", grpItem, true},
			KeyAction{[]string{"e"}, "edit", grpItem, true},
			KeyAction{[]string{"r"}, "rename", grpItem, true},
			KeyAction{[]string{"n"}, "new", grpItem, true},
			KeyAction{[]string{"x"}, "export", grpItem, true},
			KeyAction{[]string{"d"}, "delete", grpItem, true},
			KeyAction{[]string{"m"}, "metadata", grpItem, true},
			KeyAction{[]string{"i"}, "install", grpItem, true},
			KeyAction{[]string{"u"}, "uninstall", grpItem, true},
			actFilter, actRefresh,
		)
	}
	// 条目栏：底栏优先 CRUD + install/export；m/space/a 留给 `?`
	return append(nav,
		actDetail, actEdit, actNew, actRename,
		KeyAction{[]string{"m"}, "metadata", grpItem, true},
		KeyAction{[]string{"i/I"}, "install", grpItem, false},
		KeyAction{[]string{"u/U"}, "uninstall", grpItem, false},
		actExport, actDelete,
		KeyAction{[]string{"space"}, "toggle select", grpItem, true},
		KeyAction{[]string{"a"}, "select all visible", grpItem, true},
		actFilter, actRefresh,
	)
}

// cursorForFocus 返回当前焦点栏的游标位置（翻页用）。
func (t *configTab) cursorForFocus() int {
	if t.focusLeft {
		return t.groupIndex
	}
	return t.itemIndex
}

func (t *configTab) InputMode() bool {
	if t.form != nil {
		return true
	}
	switch t.mode {
	case configModeFilter, configModeExportPath:
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

// Reload drops cached data and reloads; the top level calls it after a
// background sync applies remote changes.
func (t *configTab) Reload() tea.Cmd {
	// stale-while-revalidate：后台重载期间旧数据保持可见，完成后静默替换。
	return t.load()
}

// focusJump positions the cursor at (group, name) for search-result
// navigation. It also dismisses detail/filter modes. An empty or unknown
// group falls back to the "All" view so stale/foreign data cannot strand the
// cursor.
func (t *configTab) focusJump(group, name string) {
	t.filterBox.Clear()
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
		st := perflog.Start("tui.load-config")
		if mgr == nil {
			st.End(false)
			return configLoadedMsg{err: fmt.Errorf("config manager unavailable")}
		}
		cfgs, warnings, err := mgr.ListWithWarnings("")
		if err != nil {
			st.End(false)
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
		st.With("items", len(cfgs)).End(true)
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
	if t.filterBox.Term() == "" {
		return true
	}
	return t.filterBox.Matches(it.name) || t.filterBox.Matches(it.group) || t.filterBox.Matches(it.description)
}

func (t *configTab) filteredItems() []configRow {
	base := t.baseItems()
	if t.filterBox.Term() == "" {
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
	// Form results are handled before routing further messages into the open form.
	switch msg := msg.(type) {
	case formSubmitMsg:
		submit := t.formSubmit
		t.form = nil
		t.formSubmit = nil
		if submit == nil {
			return t, nil
		}
		return t, submit(msg.values)
	case formCancelMsg:
		t.form = nil
		t.formSubmit = nil
		return t, warnToast("cancelled")
	}
	if t.form != nil {
		next, cmd := t.form.Update(msg)
		t.form = next
		return t, cmd
	}

	switch msg := msg.(type) {
	case renameDoneMsg:
		t.pendingFocusName = msg.key
		t.pendingFocusGroup = msg.group
		return t, tea.Batch(okToast(msg.text), t.load())

	case configLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.groups = msg.groups
		t.itemsByGroup = msg.itemsByGroup
		t.loaded = true
		t.clampCursors()
		t.reconcileSelection()
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
			parts = append(parts, fmt.Sprintf("skipped config %s", w.OldName))
		}
		text := strings.Join(parts, ", ") + ": name is not portable, run senv config repair to fix"
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
		case "pgup":
			t.jumpCursor(t.cursorForFocus() - pageStep(t.height))
		case "pgdown":
			t.jumpCursor(t.cursorForFocus() + pageStep(t.height))
		case "enter":
			return t.showDetail()
		case "e":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("multiple entries selected: narrow to a single selection to edit")
			}
			return t.editCurrent()
		case "r":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("multiple entries selected: narrow to a single selection to rename")
			}
			return t.enterRenameMode()
		case "m":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("multiple entries selected: narrow to a single selection to edit metadata")
			}
			return t.enterMetaMode()
		case "n":
			return t.enterCreateName()
		case "x":
			return t.doExportCurrent()
		case " ", "space":
			if !t.focusLeft {
				if it, ok := t.currentItem(); ok {
					t.sel.Toggle(it.name)
				}
			}
		case "a":
			if !t.focusLeft {
				keys := make([]string, 0, len(t.filteredItems()))
				for _, it := range t.filteredItems() {
					keys = append(keys, it.name)
				}
				t.sel.SelectVisible(keys)
			}
		case "i":
			if !t.focusLeft && t.sel.SelectionCount() > 1 {
				return t.enterSelectionPlan("install")
			}
			return t.enterPlan("install", false)
		case "I":
			if t.focusLeft {
				return t.enterSidebarPlan("install")
			}
			return t.enterPlan("install", true)
		case "u":
			if !t.focusLeft && t.sel.SelectionCount() > 1 {
				return t.enterSelectionPlan("uninstall")
			}
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
		t.groupIndex = clamp(t.groupIndex+delta, 0, len(t.groups)-1)
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(t.itemIndex+delta, 0, len(t.filteredItems())-1)
}

func (t *configTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, len(t.groups)-1)
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(idx, 0, len(t.filteredItems())-1)
}

func (t *configTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, len(t.groups)-1)
	t.itemIndex = clamp(t.itemIndex, 0, len(t.filteredItems())-1)
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
			t.filterBox.Clear() // esc 清词并退出
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
			if t.filterBox.Backspace() {
				t.itemIndex = 0
			}
			return t, nil
		}
		if isPrintable(msg) {
			t.filterBox.Append(msg.String())
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
	}
	t.mode = configModeNormal
	return t, nil
}

// --- entry points ---

func (t *configTab) showDetail() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry selected")
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
		return t, warnToast("no entry to edit")
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

// realGroup 返回当前选中的真实分组名；All 伪组返回 ""。
func (t *configTab) realGroup() string {
	if g := t.currentGroup(); g != allConfigsLabel {
		return g
	}
	return ""
}

// enterCreateName 创建配置走结构化表单（grill D6-⑥）：一次收集
// name/源路径/target/分组/描述，内联校验，esc 取消零副作用。
func (t *configTab) enterCreateName() (Tab, tea.Cmd) {
	mgr := t.mgr.Config
	siblings := make(map[string]bool)
	if mgr != nil {
		if cfgs, err := mgr.List(""); err == nil {
			for _, c := range cfgs {
				siblings[c.Name] = true
			}
		}
	}
	group := t.realGroup()
	f := newForm("create config",
		formField{key: "name", label: "name", kind: formText, placeholder: "app-config",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("name cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid name")
				}
				if siblings[v] {
					return fmt.Errorf("config %s already exists", v)
				}
				return nil
			}},
		formField{key: "source", label: "source file path", kind: formPath, placeholder: "~/app.conf",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("source file path cannot be empty")
				}
				return nil
			}},
		formField{key: "target", label: "target path", kind: formPath, placeholder: "~/.config/app/app.conf",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("target path cannot be empty")
				}
				return nil
			}},
		formField{key: "group", label: "group", kind: formText, value: group, placeholder: "group (empty = default)",
			validate: func(v string) error {
				// 与 meta 编辑的分组校验一致：空值回落 default，非空须合法。
				v = strings.TrimSpace(v)
				if v == "" {
					return nil
				}
				return storage.ValidateName(v)
			}},
		formField{key: "description", label: "description", kind: formText, placeholder: "optional",
			validate: func(v string) error {
				_, err := storage.ValidateDescription(v, true)
				return err
			}},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doCreate(strings.TrimSpace(values["name"]), strings.TrimSpace(values["source"]),
			strings.TrimSpace(values["target"]), strings.TrimSpace(values["group"]),
			strings.TrimSpace(values["description"]))
	})
	return t, nil
}

// openForm installs a structured form and the action to run on submit.
func (t *configTab) openForm(f *form, onSubmit func(values map[string]string) tea.Cmd) {
	f.SetSize(t.width, t.height)
	t.form = f
	t.formSubmit = onSubmit
}

// enterRenameMode opens the entry-rename form (target path, group and contents
// are untouched by a rename).
func (t *configTab) enterRenameMode() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry to rename")
	}
	names := make(map[string]bool, len(t.baseItems()))
	for _, row := range t.baseItems() {
		names[row.name] = true
	}
	old := it.name
	f := newForm("rename config entry",
		formField{key: "name", label: "new name", kind: formText, value: old, placeholder: "new-name",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("name cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid name")
				}
				if v != old && names[v] {
					return fmt.Errorf("config %s already exists", v)
				}
				return nil
			}})
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doRename(old, strings.TrimSpace(values["name"]))
	})
	return t, nil
}

// enterMetaMode opens the metadata form (group + description) backed by
// config.Manager.SetMeta.
func (t *configTab) enterMetaMode() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry to edit")
	}
	groups := make([]string, 0, len(t.groups))
	for i := 1; i < len(t.groups); i++ {
		groups = append(groups, t.groups[i].name)
	}
	old := it
	f := newForm("edit config metadata",
		formField{key: "group", label: "group", kind: formText, value: old.group, placeholder: storage.ConfigDefaultGroup,
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return nil // empty falls back to the default group
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid group name")
				}
				return nil
			}},
		formField{key: "description", label: "description", kind: formText, value: old.description, placeholder: "optional",
			validate: func(v string) error {
				_, err := storage.ValidateDescription(v, true)
				return err
			}},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doSetMeta(old.name, strings.TrimSpace(values["group"]), strings.TrimSpace(values["description"]))
	})
	return t, nil
}

func (t *configTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		return t, warnToast("no entry to delete")
	}
	t.mode = configModeDeleteConfirm
	return t, nil
}

func (t *configTab) enterFilterMode() (Tab, tea.Cmd) {
	t.mode = configModeFilter
	t.filterBox.EnterFresh() // config 语义：`/` 清词重新开始
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
// When the All view is selected, groupScope uses Scope{All: true}.
func (t *configTab) enterPlan(kind string, groupScope bool) (Tab, tea.Cmd) {
	if groupScope {
		if t.currentGroup() == "" {
			return t.planForScope(kind, config.Scope{All: true})
		}
		it, ok := t.currentItem()
		if !ok {
			return t, warnToast("no entry selected")
		}
		return t.planForScope(kind, config.Scope{Group: it.group})
	}
	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry selected")
	}
	return t.planForScope(kind, config.Scope{Name: it.name})
}

// enterSelectionPlan 以多选集为范围生成合并计划（跨分组逐条列出）。
func (t *configTab) enterSelectionPlan(kind string) (Tab, tea.Cmd) {
	mgr := t.mgr.Config
	if mgr == nil {
		return t, warnToast("config manager unavailable")
	}
	names := t.selectedNames()
	if len(names) == 0 {
		return t, warnToast("selection is empty")
	}
	return t, func() tea.Msg {
		msg := configPlanLoadedMsg{kind: kind}
		if kind == "install" {
			merged := &config.InstallPlan{}
			for _, name := range names {
				plan, err := mgr.PlanInstall(config.Scope{Name: name})
				if err != nil {
					msg.err = err
					return msg
				}
				merged.Items = append(merged.Items, plan.Items...)
			}
			msg.installPlan = merged
		} else {
			merged := &config.UninstallPlan{}
			for _, name := range names {
				plan, err := mgr.PlanUninstall(config.Scope{Name: name})
				if err != nil {
					msg.err = err
					return msg
				}
				merged.Items = append(merged.Items, plan.Items...)
			}
			msg.uninstallPlan = merged
		}
		return msg
	}
}

// allItems 聚合全部真实分组的条目（不过滤），与当前视图无关。批量目标
// 解析与选择集 reconcile 以此为准：选择集跨过滤与分组持久。
func (t *configTab) allItems() []configRow {
	var out []configRow
	for i := 1; i < len(t.groups); i++ {
		out = append(out, t.itemsByGroup[t.groups[i].name]...)
	}
	return out
}

// selectedNames 返回多选集命中的全部条目名——选择集跨过滤持久，被过滤隐藏
// 的已选项保持在批量计划范围内（tui-viewer 多选语义），故遍历全量而非可见集。
func (t *configTab) selectedNames() []string {
	var out []string
	for _, it := range t.allItems() {
		if t.sel.IsSelected(it.name) {
			out = append(out, it.name)
		}
	}
	return out
}

// reconcileSelection 丢弃多选集中已不存在的条目名（删除/改名/外部 reload 后
// 防止幽灵勾选）。
func (t *configTab) reconcileSelection() {
	live := make(map[string]bool)
	for _, it := range t.allItems() {
		live[it.name] = true
	}
	for _, name := range t.sel.Selected() {
		if !live[name] {
			t.sel.Toggle(name)
		}
	}
}

// enterSidebarPlan handles group-scope install/uninstall triggered while the
// group sidebar has focus. The All pseudo-group installs/uninstalls every
// config (Scope{All: true}); a real group uses that group's scope.
func (t *configTab) enterSidebarPlan(kind string) (Tab, tea.Cmd) {
	g := t.currentGroup()
	if g == "" {
		return t.planForScope(kind, config.Scope{All: true})
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
	case "esc", "n":
		t.mode = configModeNormal
		t.plan = nil
		return t, warnToast("cancelled")
	}
	// 其余按键忽略：计划确认页不把未知键解释为取消或放行（grill D7）
	return t, nil
}

// scopeLabel 汇总安装/卸载范围用于审计 target（不含文件内容）。
func (ps *planState) scopeLabel() string {
	var names []string
	if ps.kind == "install" {
		if ps.installPlan != nil {
			for _, it := range ps.installPlan.Items {
				names = append(names, it.Name)
			}
		}
	} else if ps.uninstallPlan != nil {
		for _, it := range ps.uninstallPlan.Items {
			names = append(names, it.Name)
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	if ps.scope.All {
		return "*"
	}
	if ps.scope.Name != "" {
		return ps.scope.Name
	}
	return "[" + fmt.Sprint(len(names)) + " items]"
}

// executePlan runs a confirmed plan and reloads the list afterwards.
func (t *configTab) executePlan(ps *planState) tea.Cmd {
	mgr := t.mgr.Config
	mgrs := t.mgr
	return func() tea.Msg {
		var err error
		eventType := sessionpkg.AuditOpInstall
		detail := "install"
		if ps.kind == "install" {
			err = mgr.ExecuteInstall(ps.installPlan)
		} else {
			eventType = sessionpkg.AuditOpUninstall
			detail = "uninstall"
			err = mgr.ExecuteUninstall(ps.uninstallPlan, func(item config.UninstallItem) bool {
				return ps.changedAllowed[item.Name]
			})
		}
		if err != nil {
			recordAudit(mgrs, eventType, "config:"+ps.scopeLabel(), false, detail+" failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, eventType, "config:"+ps.scopeLabel(), true, detail)
		return configReloadMsg{}
	}
}

// --- operations ---

func (t *configTab) doDelete(name string) tea.Cmd {
	mgr := t.mgr.Config
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.Delete(name); err != nil {
			recordAudit(mgrs, sessionpkg.AuditOpConfig, "config:"+name, false, "delete failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, sessionpkg.AuditOpConfig, "config:"+name, true, "delete")
		return configReloadMsg{}
	}
}

// doRename renames a config entry through the storage-layer atomic rename.
func (t *configTab) doRename(oldName, newName string) tea.Cmd {
	if oldName == newName {
		return warnToast("name unchanged")
	}
	mgr := t.mgr.Config
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.Rename(oldName, newName); err != nil {
			recordAudit(mgrs, sessionpkg.AuditOpConfig, "config:"+oldName, false, "rename failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, sessionpkg.AuditOpConfig, "config:"+newName, true, "rename "+oldName)
		return renameDoneMsg{group: t.currentGroup(), key: newName, text: "renamed to " + newName}
	}
}

// doSetMeta updates a config's group and description.
func (t *configTab) doSetMeta(name, group, description string) tea.Cmd {
	mgr := t.mgr.Config
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.SetMeta(name, group, description); err != nil {
			recordAudit(mgrs, sessionpkg.AuditOpConfig, configTarget(group, name), false, "set meta failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, sessionpkg.AuditOpConfig, configTarget(group, name), true, "set meta")
		return renameDoneMsg{group: group, key: name, text: "metadata updated"}
	}
}

// doExportCurrent exports to the config's default target path.
func (t *configTab) doExportCurrent() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry to export")
	}
	return t, t.doExport(it.name, "") // empty -> config's TargetPath
}

func (t *configTab) doExport(name, path string) tea.Cmd {
	mgr := t.mgr.Config
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.Export(name, path); err != nil {
			recordAudit(mgrs, sessionpkg.AuditOpConfig, "config:"+name, false, "export failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, sessionpkg.AuditOpConfig, "config:"+name, true, "export")
		return toastMsg{text: "exported " + name, level: toastSuccess}
	}
}

func (t *configTab) doCreate(name, source, target, group, description string) tea.Cmd {
	mgr := t.mgr.Config
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.Create(name, source, target, group, description); err != nil {
			recordAudit(mgrs, sessionpkg.AuditOpConfig, configTarget(group, name), false, "create failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, sessionpkg.AuditOpConfig, configTarget(group, name), true, "create")
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
		return clipLines(t.renderPlan(), paneBudget(t.height))
	}
	if t.mode == configModeDetail && t.detail != nil {
		return clipLines(t.renderDetail(), paneBudget(t.height))
	}
	overlay := ""
	if t.form != nil {
		overlay = t.form.View()
	} else if t.mode != configModeNormal {
		overlay = t.renderModal()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

func (t *configTab) viewBaseAt(h int) string {
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

	left := t.renderGroups(leftW, h)
	right := t.renderItems(rightW, h)

	if t.focusLeft {
		left = activePaneStyle.Width(leftW).Height(h).Render(left)
		right = paneStyle.Width(rightW).Height(h).Render(right)
	} else {
		left = paneStyle.Width(leftW).Height(h).Render(left)
		right = activePaneStyle.Width(rightW).Height(h).Render(right)
	}
	gap := lipgloss.NewStyle().Width(1).Render(" ")
	return lipgloss.JoinHorizontal(lipgloss.Top, left, gap, right)
}

func (t *configTab) renderGroups(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading groups…")
	}
	rows := make([]SidebarRow, 0, len(t.groups))
	for i, g := range t.groups {
		rows = append(rows, SidebarRow{
			Marker:   " ",
			Name:     g.name,
			Count:    t.sidebarCount(i),
			Selected: i == t.groupIndex && t.focusLeft,
		})
	}
	// Count excludes the "All" pseudo-group.
	return renderSidebar(rows, t.groupIndex, height, width)
}

func (t *configTab) renderItems(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading configs…")
	}
	items := t.filteredItems()
	label := t.currentGroup()
	if label == "" {
		label = allConfigsLabel
	}
	header := fmt.Sprintf("%s (%d)", label, len(items))
	if t.filterBox.Term() != "" {
		header += "  /" + t.filterBox.Term()
	}
	visibleKeys := make([]string, 0, len(items))
	for _, it := range items {
		visibleKeys = append(visibleKeys, it.name)
	}
	header += t.sel.SelectionHint(t.sel.SelectionCount() - t.sel.SelectedIn(visibleKeys))
	if len(items) == 0 {
		hint := "no config files yet"
		if t.filterBox.Term() != "" {
			hint = "no names match /" + t.filterBox.Term()
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
		if t.sel.IsSelected(it.name) {
			displayName = "[x] " + displayName
		}
		if t.currentGroup() == "" {
			displayName = it.group + "/" + it.name
		}
		selected := i == t.itemIndex
		line := formatConfigItemLine(displayName, it.description, it.targetPath, it.updatedAt, pathW, inner, selected)
		if selected {
			line = selectedLineStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return windowedPane(header, lines, t.itemIndex, height, width)
}

// formatConfigItemLine builds one config list row. Every row starts with a
// 2-column cursor prefix so name/desc/path/updated stay aligned across files.
func formatConfigItemLine(name, desc, path, updated string, pathW, inner int, selected bool) string {
	line := fmt.Sprintf("%s %s %s %s",
		padRunes(name, 22), padRunes(desc, 14),
		padRunes(truncPathN(path, pathW), pathW), updated)
	return cursorPrefix(selected) + truncateRunes(line, inner-2)
}

// renderPlan renders the install/uninstall plan preview and, during changed
// confirmation, the per-item prompt.
func (t *configTab) renderPlan() string {
	inner := t.width - 4
	if inner < 40 {
		inner = 40
	}
	var lines []string
	title := "install plan"
	if t.plan.kind == "uninstall" {
		title = "uninstall plan"
	}
	if t.plan.installPlan != nil {
		for _, item := range t.plan.installPlan.Items {
			name := item.Name
			if t.plan.scope.All && item.Group != "" {
				name = item.Group + "/" + item.Name
			}
			lines = append(lines, formatPlanLine(item.Action, name, item.TargetPath, item.Reason, inner))
		}
	} else {
		for _, item := range t.plan.uninstallPlan.Items {
			marker := item.Action
			if item.Action == config.ActionChanged {
				marker = "CHANGED"
			}
			name := item.Name
			if t.plan.scope.All && item.Group != "" {
				name = item.Group + "/" + item.Name
			}
			lines = append(lines, formatPlanLine(marker, name, item.TargetPath, item.Reason, inner))
		}
	}
	// Keep title + hint + borders visible: window the item list if needed.
	chrome := 5
	if t.mode == configModeChangedConfirm {
		chrome += 2
	}
	page := paneBudget(t.height) - chrome
	if page < 1 {
		page = 1
	}
	if len(lines) > page {
		lines = lines[:page]
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)

	if t.mode == configModeChangedConfirm {
		item := t.plan.uninstallPlan.Items[t.plan.changedIdx]
		prompt := fmt.Sprintf("target file was modified locally, delete %s? y delete · n keep", item.TargetPath)
		return modalBox(t.width, t.height, title, body+"\n\n"+prompt, "")
	}
	return modalBox(t.width, t.height, title, body, "y confirm · esc cancel")
}

// formatPlanLine aligns action / name / path / reason so multi-file plans
// keep a stable column grid. Action is padded to backup_overwrite (16).
func formatPlanLine(action, name, path, reason string, inner int) string {
	const actionW, nameW = 16, 14
	fixed := 2 + 1 + actionW + 2 + 1 + nameW + 4 // "  [" + action + "] " + name + " -> "
	rest := inner - fixed
	if rest < 12 {
		rest = 12
	}
	pathW := rest * 2 / 3
	if pathW > 36 {
		pathW = 36
	}
	reasonW := rest - pathW - 3 // " (" + ")"
	if reasonW < 4 {
		reasonW = 4
	}
	line := "  [" + padRunes(action, actionW) + "] " +
		padRunes(name, nameW) + " -> " +
		padRunes(truncPathN(path, pathW), pathW) +
		" (" + truncRunes(reason, reasonW) + ")"
	return truncateRunes(line, inner)
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
		"name:     %s\ngroup:    %s\ndesc:     %s\ntarget:   %s\ncreated:  %s\nupdated:  %s",
		d.name, d.group, d.description, d.targetPath, d.createdAt, d.updatedAt)
	box := modalBox(t.width, t.height, "config detail", body, "any key to close")
	return box
}

func (t *configTab) renderModal() string {
	switch t.mode {
	case configModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox(t.width, t.height, "delete "+it.name+"?", "", "enter/y confirm · esc/n cancel")
	case configModeExportPath:
		return modalBox(t.width, t.height, "export to file", t.input.View(), "enter export · esc cancel")
	case configModeFilter:
		return modalBox(t.width, t.height, "filter names (case insensitive)", "/"+t.filterBox.Term()+"_", "esc clear")
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
